package tlsscanner

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ── JSON model ────────────────────────────────────────────────────────────────
// Field names mirror the tls-scanner results.json schema exactly.

// ScanResults is the top-level object in results.json.
type ScanResults struct {
	TotalIPs          int               `json:"total_ips"`
	ScannedIPs        int               `json:"scanned_ips"`
	IPResults         []IPResult        `json:"ip_results"`
	TLSSecurityConfig TLSSecurityConfig `json:"tls_security_config"`
}

// IPResult holds scan data for a single pod IP.
type IPResult struct {
	IP                 string        `json:"ip"`
	Status             string        `json:"status"`
	OpenPorts          []int         `json:"open_ports"`
	PortResults        []PortResult  `json:"port_results"`
	OpenShiftComponent ComponentInfo `json:"openshift_component"`
	Pod                PodInfo       `json:"pod"`
}

// PortResult holds the TLS assessment for one port on one pod.
type PortResult struct {
	Port                int                  `json:"port"`
	Status              string               `json:"status"`
	TLS13Supported      bool                 `json:"tls13_supported"`
	MLKemSupported      bool                 `json:"mlkem_supported"`
	APIServerCompliance *APIServerCompliance `json:"api_server_tls_config_compliance"`
	TLSReadiness        TLSReadiness         `json:"tls_readiness"`
}

// APIServerCompliance captures whether the port honors the cluster TLS profile.
type APIServerCompliance struct {
	ConfiguredProfile string `json:"configured_profile"`
	Version           bool   `json:"version"`
	Ciphers           bool   `json:"ciphers"`
}

// TLSReadiness is a summary object the scanner computes per port.
type TLSReadiness struct {
	TLS13Offered bool   `json:"tls13_offered"`
	TLS12Only    bool   `json:"tls12_only"`
	PQCCapable   bool   `json:"pqc_capable"`
	Notes        string `json:"notes"`
}

// ComponentInfo identifies the OpenShift component for a pod.
type ComponentInfo struct {
	Component string `json:"component"`
}

// PodInfo carries the pod name as reported by the scanner.
type PodInfo struct {
	Name string `json:"Name"`
}

// TLSSecurityConfig is the cluster-level TLS configuration snapshot.
type TLSSecurityConfig struct {
	APIServer struct {
		Type          string `json:"type"`
		MinTLSVersion string `json:"min_tls_version"`
	} `json:"api_server"`
}

// ── Parser ────────────────────────────────────────────────────────────────────

// ParseResultsJSON reads and unmarshals results.json written by the scanner.
func ParseResultsJSON(path string) *ScanResults {
	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred(), "failed to read scanner results from %s", path)
	var results ScanResults
	Expect(json.Unmarshal(data, &results)).To(Succeed(),
		"failed to parse scanner results JSON from %s", path)
	return &results
}

// ── Assertions ────────────────────────────────────────────────────────────────

// AssertClusterTLSSecurityConfig asserts that the cluster-level TLS snapshot
// in the scan results matches one of the accepted PQC-capable profiles
// ("Modern" on OCP 4.17+, "Intermediate" on older clusters).
// "Old" (TLS 1.0) is always rejected as it regresses security.
func AssertClusterTLSSecurityConfig(results *ScanResults, acceptableProfiles ...string) {
	actual := results.TLSSecurityConfig.APIServer.Type
	log.Printf("Cluster APIServer TLS security profile: %q", actual)
	Expect(actual).To(BeElementOf(acceptableProfiles),
		"cluster APIServer TLS profile %q is not in the accepted PQC-capable profiles %v", actual, acceptableProfiles)
}

// AssertComponentPQCCompliant finds the scan result for the given deployment
// name (matched by pod-name prefix), locates its TLS-serving port (status=OK),
// and asserts all five PQC compliance dimensions:
//
//  1. TLS 1.3 offered
//  2. ML-KEM (X25519MLKEM768) supported
//  3. API server version compliance (honors cluster min TLS version)
//  4. API server cipher compliance (honors cluster cipher suite)
//  5. Not TLS 1.2 only (no TLS 1.2 downgrade)
func AssertComponentPQCCompliant(results *ScanResults, deploymentName string) {
	ip, err := findIPResultByPodPrefix(results, deploymentName)
	Expect(err).NotTo(HaveOccurred(),
		"component %q not found in scan results — check scanner RBAC and namespace filter", deploymentName)

	port, err := findTLSServingPort(ip)
	Expect(err).NotTo(HaveOccurred(),
		"no TLS-serving port (status=OK) found for component %q (pod %q, open ports: %v)",
		deploymentName, ip.Pod.Name, ip.OpenPorts)

	prefix := fmt.Sprintf("component %q pod %q port %d", deploymentName, ip.Pod.Name, port.Port)

	Expect(port.TLS13Supported).To(BeTrue(),
		"%s: TLS 1.3 not offered — tls13_supported=false", prefix)

	Expect(port.MLKemSupported).To(BeTrue(),
		"%s: ML-KEM not supported — mlkem_supported=false\nnotes: %s",
		prefix, port.TLSReadiness.Notes)

	Expect(port.APIServerCompliance).NotTo(BeNil(),
		"%s: api_server_tls_config_compliance block missing from scan result", prefix)

	Expect(port.APIServerCompliance.Version).To(BeTrue(),
		"%s: does not honor cluster min TLS version (api_server_tls_config_compliance.version=false)",
		prefix)

	Expect(port.APIServerCompliance.Ciphers).To(BeTrue(),
		"%s: does not honor cluster cipher suite (api_server_tls_config_compliance.ciphers=false)",
		prefix)

	Expect(port.TLSReadiness.TLS12Only).To(BeFalse(),
		"%s: serves TLS 1.2 only — not compliant with Modern profile", prefix)
}

// AssertComponentEdgeTerminated asserts that a component uses OpenShift edge TLS
// termination (TLS handled at the router, not at the pod). The pod must expose
// only plain HTTP (NO_TLS status) — PQC compliance at the pod level is not applicable.
func AssertComponentEdgeTerminated(results *ScanResults, deploymentName string) {
	ip, err := findIPResultByPodPrefix(results, deploymentName)
	Expect(err).NotTo(HaveOccurred(),
		"component %q not found in scan results", deploymentName)

	for _, pr := range ip.PortResults {
		Expect(pr.Status).To(Equal("NO_TLS"),
			"component %q pod %q port %d: expected NO_TLS (edge termination) but got %s",
			deploymentName, ip.Pod.Name, pr.Port, pr.Status)
	}
	log.Printf("Component %q (pod %q) correctly uses edge TLS termination — no pod-level TLS",
		deploymentName, ip.Pod.Name)
}

// AssertComponentPQCCompliantOrNoPort asserts PQC compliance when the scanner
// found a TLS port (status=OK). If the pod declares no container ports and the
// scanner reports NO_PORTS, the test is skipped with an informational message.
// Use this for components whose pods don't declare container ports in their spec
// (like tekton-results-api) but that are known to serve TLS.
func AssertComponentPQCCompliantOrNoPort(results *ScanResults, deploymentName string) {
	ip, err := findIPResultByPodPrefix(results, deploymentName)
	Expect(err).NotTo(HaveOccurred(),
		"component %q not found in scan results", deploymentName)

	if len(ip.PortResults) == 1 && ip.PortResults[0].Status == "NO_PORTS" {
		log.Printf("Component %q (pod %q): scanner reports NO_PORTS — pod declares no container ports; TLS verification skipped",
			deploymentName, ip.Pod.Name)
		Skip(fmt.Sprintf("component %q: scanner found NO_PORTS (pod declares no container ports); manually verify TLS on service port", deploymentName))
		return
	}

	port, err := findTLSServingPort(ip)
	Expect(err).NotTo(HaveOccurred(),
		"no TLS-serving port (status=OK) found for component %q (pod %q, open ports: %v)",
		deploymentName, ip.Pod.Name, ip.OpenPorts)

	prefix := fmt.Sprintf("component %q pod %q port %d", deploymentName, ip.Pod.Name, port.Port)

	Expect(port.TLS13Supported).To(BeTrue(),
		"%s: TLS 1.3 not offered — tls13_supported=false", prefix)
	Expect(port.MLKemSupported).To(BeTrue(),
		"%s: ML-KEM not supported — mlkem_supported=false\nnotes: %s",
		prefix, port.TLSReadiness.Notes)
	Expect(port.APIServerCompliance).NotTo(BeNil(),
		"%s: api_server_tls_config_compliance block missing", prefix)
	Expect(port.APIServerCompliance.Version).To(BeTrue(),
		"%s: does not honor cluster min TLS version", prefix)
	Expect(port.APIServerCompliance.Ciphers).To(BeTrue(),
		"%s: does not honor cluster cipher suite", prefix)
	Expect(port.TLSReadiness.TLS12Only).To(BeFalse(),
		"%s: serves TLS 1.2 only — not compliant with Modern profile", prefix)
}

// ── lookup helpers ────────────────────────────────────────────────────────────

// findIPResultByPodPrefix returns the IPResult whose pod name starts with the
// given deployment name prefix (e.g. "tekton-pipelines-webhook").
func findIPResultByPodPrefix(results *ScanResults, deploymentName string) (*IPResult, error) {
	for i := range results.IPResults {
		if strings.HasPrefix(results.IPResults[i].Pod.Name, deploymentName) {
			return &results.IPResults[i], nil
		}
	}
	var found []string
	for _, r := range results.IPResults {
		found = append(found, r.Pod.Name)
	}
	return nil, fmt.Errorf("no pod with prefix %q found; scanned pods: %v",
		deploymentName, found)
}

// findTLSServingPort returns the first port whose status is "OK", meaning the
// scanner successfully negotiated TLS and has full compliance data.
func findTLSServingPort(ip *IPResult) (*PortResult, error) {
	for i := range ip.PortResults {
		if ip.PortResults[i].Status == "OK" {
			return &ip.PortResults[i], nil
		}
	}
	var statuses []string
	for _, pr := range ip.PortResults {
		statuses = append(statuses, fmt.Sprintf("port=%d status=%s", pr.Port, pr.Status))
	}
	return nil, fmt.Errorf("no OK port found; port statuses: %v", statuses)
}
