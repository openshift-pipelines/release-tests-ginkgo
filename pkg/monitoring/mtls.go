// Package monitoring provides helpers for querying Prometheus metrics in integration tests.
package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// MTLSComponentConfig describes a component's expected metrics Service and ServiceMonitor names
// for mTLS assertions.
type MTLSComponentConfig struct {
	// ComponentName is the human-readable name of the component (e.g. "TektonPipeline").
	ComponentName string
	// ServiceName is the Kubernetes Service name to assert on.
	ServiceName string
	// ServiceMonitorName is the ServiceMonitor name to assert on.
	ServiceMonitorName string
	// Namespace is the namespace where the Service/ServiceMonitor/Pods live.
	Namespace string
	// HTTPPortName is the expected plain-HTTP metrics port name (e.g. "http-metrics").
	HTTPPortName string
	// HTTPSPortName is the expected mTLS metrics port name (e.g. "https-metrics").
	HTTPSPortName string
}

// ServingCertAnnotation is the OpenShift annotation that triggers serving-cert Secret generation.
const ServingCertAnnotation = "service.beta.openshift.io/serving-cert-secret-name"

// AssertServiceMTLSEnabled verifies that the Service has the expected mTLS configuration:
//   - Port renamed to the HTTPS variant
//   - Serving-cert annotation present
//   - Referenced Secret exists and is populated
func AssertServiceMTLSEnabled(cs *clients.Clients, cfg MTLSComponentConfig) error {
	svc, err := cs.KubeClient.Kube.CoreV1().Services(cfg.Namespace).Get(
		context.Background(), cfg.ServiceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Service %s/%s: %w", cfg.Namespace, cfg.ServiceName, err)
	}

	// Check port name
	found := false
	for _, port := range svc.Spec.Ports {
		if port.Name == cfg.HTTPSPortName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("service %s/%s: expected port named %q, got ports: %s",
			cfg.Namespace, cfg.ServiceName, cfg.HTTPSPortName, formatPortNames(svc.Spec.Ports))
	}

	// Check serving-cert annotation
	secretName, ok := svc.Annotations[ServingCertAnnotation]
	if !ok || secretName == "" {
		return fmt.Errorf("service %s/%s: missing annotation %q",
			cfg.Namespace, cfg.ServiceName, ServingCertAnnotation)
	}

	// Check Secret exists and is populated
	secret, err := cs.KubeClient.Kube.CoreV1().Secrets(cfg.Namespace).Get(
		context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("serving-cert Secret %s/%s not found: %w", cfg.Namespace, secretName, err)
	}
	if len(secret.Data) == 0 {
		return fmt.Errorf("serving-cert Secret %s/%s exists but has no data", cfg.Namespace, secretName)
	}
	log.Printf("Service %s/%s: mTLS enabled — port=%s, secret=%s (%d data keys)",
		cfg.Namespace, cfg.ServiceName, cfg.HTTPSPortName, secretName, len(secret.Data))
	return nil
}

// AssertServiceMTLSDisabled verifies that the Service has the expected plain HTTP configuration:
//   - Port name is the HTTP variant
//   - No serving-cert annotation
func AssertServiceMTLSDisabled(cs *clients.Clients, cfg MTLSComponentConfig) error {
	svc, err := cs.KubeClient.Kube.CoreV1().Services(cfg.Namespace).Get(
		context.Background(), cfg.ServiceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Service %s/%s: %w", cfg.Namespace, cfg.ServiceName, err)
	}

	// Check port name is plain HTTP
	found := false
	for _, port := range svc.Spec.Ports {
		if port.Name == cfg.HTTPPortName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("service %s/%s: expected port named %q, got ports: %s",
			cfg.Namespace, cfg.ServiceName, cfg.HTTPPortName, formatPortNames(svc.Spec.Ports))
	}

	// Serving-cert annotation should be absent
	if secretName, ok := svc.Annotations[ServingCertAnnotation]; ok && secretName != "" {
		return fmt.Errorf("service %s/%s: unexpected annotation %q=%q (should be absent when mTLS disabled)",
			cfg.Namespace, cfg.ServiceName, ServingCertAnnotation, secretName)
	}

	log.Printf("Service %s/%s: mTLS disabled — port=%s, no serving-cert annotation",
		cfg.Namespace, cfg.ServiceName, cfg.HTTPPortName)
	return nil
}

// AssertServiceMonitorMTLSEnabled verifies that the ServiceMonitor has HTTPS scheme and tlsConfig.
// Uses oc CLI since ServiceMonitor CRDs are not in the vendored Go types.
func AssertServiceMonitorMTLSEnabled(cfg MTLSComponentConfig) error {
	result := cmd.Run("oc", "get", "servicemonitor", cfg.ServiceMonitorName,
		"-n", cfg.Namespace, "-o", "json")
	if result.ExitCode != 0 {
		return fmt.Errorf("failed to get ServiceMonitor %s/%s: %s",
			cfg.Namespace, cfg.ServiceMonitorName, result.Stderr())
	}

	var sm map[string]any
	if err := json.Unmarshal([]byte(result.Stdout()), &sm); err != nil {
		return fmt.Errorf("failed to parse ServiceMonitor JSON: %w", err)
	}

	endpoints, err := jsonPath(sm, "spec", "endpoints")
	if err != nil {
		return fmt.Errorf("servicemonitor %s/%s: %w", cfg.Namespace, cfg.ServiceMonitorName, err)
	}
	endpointList, ok := endpoints.([]any)
	if !ok || len(endpointList) == 0 {
		return fmt.Errorf("servicemonitor %s/%s: no endpoints found",
			cfg.Namespace, cfg.ServiceMonitorName)
	}

	ep, ok := endpointList[0].(map[string]any)
	if !ok {
		return fmt.Errorf("servicemonitor %s/%s: first endpoint is not an object",
			cfg.Namespace, cfg.ServiceMonitorName)
	}

	scheme, _ := ep["scheme"].(string)
	if scheme != "https" {
		return fmt.Errorf("servicemonitor %s/%s: expected scheme=https, got %q",
			cfg.Namespace, cfg.ServiceMonitorName, scheme)
	}

	tlsConfig, ok := ep["tlsConfig"].(map[string]any)
	if !ok {
		return fmt.Errorf("servicemonitor %s/%s: tlsConfig missing",
			cfg.Namespace, cfg.ServiceMonitorName)
	}
	expectedServerName := fmt.Sprintf("%s.%s.svc", cfg.ServiceName, cfg.Namespace)
	serverName, _ := tlsConfig["serverName"].(string)
	if serverName != expectedServerName {
		return fmt.Errorf("servicemonitor %s/%s: expected tlsConfig.serverName=%q, got %q",
			cfg.Namespace, cfg.ServiceMonitorName, expectedServerName, serverName)
	}

	log.Printf("ServiceMonitor %s/%s: mTLS enabled — scheme=https, serverName=%s",
		cfg.Namespace, cfg.ServiceMonitorName, serverName)
	return nil
}

// AssertServiceMonitorMTLSDisabled verifies that the ServiceMonitor uses plain HTTP.
func AssertServiceMonitorMTLSDisabled(cfg MTLSComponentConfig) error {
	result := cmd.Run("oc", "get", "servicemonitor", cfg.ServiceMonitorName,
		"-n", cfg.Namespace, "-o", "json")
	if result.ExitCode != 0 {
		return fmt.Errorf("failed to get ServiceMonitor %s/%s: %s",
			cfg.Namespace, cfg.ServiceMonitorName, result.Stderr())
	}

	var sm map[string]any
	if err := json.Unmarshal([]byte(result.Stdout()), &sm); err != nil {
		return fmt.Errorf("failed to parse ServiceMonitor JSON: %w", err)
	}

	endpoints, err := jsonPath(sm, "spec", "endpoints")
	if err != nil {
		return fmt.Errorf("servicemonitor %s/%s: %w", cfg.Namespace, cfg.ServiceMonitorName, err)
	}
	endpointList, ok := endpoints.([]any)
	if !ok || len(endpointList) == 0 {
		return fmt.Errorf("servicemonitor %s/%s: no endpoints found",
			cfg.Namespace, cfg.ServiceMonitorName)
	}

	ep, ok := endpointList[0].(map[string]any)
	if !ok {
		return fmt.Errorf("servicemonitor %s/%s: first endpoint is not an object",
			cfg.Namespace, cfg.ServiceMonitorName)
	}

	scheme, _ := ep["scheme"].(string)
	// When mTLS is disabled, scheme should be empty or "http"
	if scheme == "https" {
		return fmt.Errorf("servicemonitor %s/%s: expected scheme=http or empty, got %q",
			cfg.Namespace, cfg.ServiceMonitorName, scheme)
	}

	log.Printf("ServiceMonitor %s/%s: mTLS disabled — scheme=%q",
		cfg.Namespace, cfg.ServiceMonitorName, scheme)
	return nil
}

// AssertPodMTLSEnabled verifies that Pods for the given Service have the expected
// mTLS environment variables and volume mounts.
func AssertPodMTLSEnabled(cs *clients.Clients, cfg MTLSComponentConfig) error {
	pods, err := findPodsForService(cs, cfg.Namespace, cfg.ServiceName)
	if err != nil {
		return err
	}
	if len(pods) == 0 {
		return fmt.Errorf("no pods found for Service %s/%s", cfg.Namespace, cfg.ServiceName)
	}

	for _, pod := range pods {
		if len(pod.Spec.Containers) == 0 {
			continue
		}
		container := pod.Spec.Containers[0]
		envMap := envToMap(container.Env)

		// Check for METRICS_PROMETHEUS_TLS_* env vars
		foundTLSEnv := false
		for key := range envMap {
			if strings.HasPrefix(key, "METRICS_PROMETHEUS_TLS_") {
				foundTLSEnv = true
				break
			}
		}
		if !foundTLSEnv {
			return fmt.Errorf("pod %s/%s container %s: missing METRICS_PROMETHEUS_TLS_* env vars",
				cfg.Namespace, pod.Name, container.Name)
		}

		// Check for cert Secret volume mount and metrics-client-ca ConfigMap mount
		volNames := volumeNames(pod.Spec.Volumes)
		hasCertVol := false
		hasCAVol := false
		for _, vol := range pod.Spec.Volumes {
			if vol.Secret != nil && strings.Contains(vol.Secret.SecretName, "metrics") {
				hasCertVol = true
			}
			if vol.ConfigMap != nil && vol.ConfigMap.Name == "metrics-client-ca" {
				hasCAVol = true
			}
		}
		if !hasCertVol {
			return fmt.Errorf("pod %s/%s: no metrics cert Secret volume found (volumes: %v)",
				cfg.Namespace, pod.Name, volNames)
		}
		if !hasCAVol {
			return fmt.Errorf("pod %s/%s: no metrics-client-ca ConfigMap volume found (volumes: %v)",
				cfg.Namespace, pod.Name, volNames)
		}

		log.Printf("Pod %s/%s container %s: mTLS env/volumes present",
			cfg.Namespace, pod.Name, container.Name)
	}
	return nil
}

// AssertPodMTLSDisabled verifies that Pods for the given Service do NOT have
// METRICS_PROMETHEUS_TLS_* environment variables.
func AssertPodMTLSDisabled(cs *clients.Clients, cfg MTLSComponentConfig) error {
	pods, err := findPodsForService(cs, cfg.Namespace, cfg.ServiceName)
	if err != nil {
		return err
	}
	if len(pods) == 0 {
		return fmt.Errorf("no pods found for Service %s/%s", cfg.Namespace, cfg.ServiceName)
	}

	for _, pod := range pods {
		if len(pod.Spec.Containers) == 0 {
			continue
		}
		container := pod.Spec.Containers[0]
		for _, env := range container.Env {
			if strings.HasPrefix(env.Name, "METRICS_PROMETHEUS_TLS_") {
				return fmt.Errorf("pod %s/%s container %s: unexpected env var %s (should be absent when mTLS disabled)",
					cfg.Namespace, pod.Name, container.Name, env.Name)
			}
		}
		log.Printf("Pod %s/%s container %s: no mTLS env vars (correct for disabled)",
			cfg.Namespace, pod.Name, container.Name)
	}
	return nil
}

// AssertTLSHandshake verifies that the metrics endpoint accepts connections with the
// monitoring client certificate and rejects connections without it.
// It copies the metrics-client-certs Secret from openshift-monitoring into the target
// namespace and runs curl with/without the client certificate.
func AssertTLSHandshake(cs *clients.Clients, cfg MTLSComponentConfig) error {
	// Get the Service ClusterIP and port
	svc, err := cs.KubeClient.Kube.CoreV1().Services(cfg.Namespace).Get(
		context.Background(), cfg.ServiceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Service %s/%s: %w", cfg.Namespace, cfg.ServiceName, err)
	}

	var metricsPort int32
	for _, port := range svc.Spec.Ports {
		if port.Name == cfg.HTTPSPortName {
			metricsPort = port.Port
			break
		}
	}
	if metricsPort == 0 {
		return fmt.Errorf("service %s/%s: no port named %q found",
			cfg.Namespace, cfg.ServiceName, cfg.HTTPSPortName)
	}

	// Copy the monitoring client certs Secret into the target namespace
	copyResult := cmd.Run("oc", "get", "secret", "metrics-client-certs",
		"-n", "openshift-monitoring", "-o", "json")
	if copyResult.ExitCode != 0 {
		return fmt.Errorf("failed to get metrics-client-certs from openshift-monitoring: %s",
			copyResult.Stderr())
	}

	// Parse, clean metadata, and create in target namespace
	var secretObj map[string]any
	if err := json.Unmarshal([]byte(copyResult.Stdout()), &secretObj); err != nil {
		return fmt.Errorf("failed to parse metrics-client-certs JSON: %w", err)
	}
	if meta, ok := secretObj["metadata"].(map[string]any); ok {
		meta["namespace"] = cfg.Namespace
		for _, key := range []string{"creationTimestamp", "resourceVersion", "selfLink", "uid", "annotations", "ownerReferences"} {
			delete(meta, key)
		}
	}
	cleanedJSON, err := json.Marshal(secretObj)
	if err != nil {
		return fmt.Errorf("failed to marshal cleaned secret: %w", err)
	}

	// Write to a temp file and apply
	tmpFile, tmpErr := os.CreateTemp("", "metrics-client-certs-*.json")
	if tmpErr != nil {
		return fmt.Errorf("failed to create temp file for secret: %w", tmpErr)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	if _, writeErr := tmpFile.Write(cleanedJSON); writeErr != nil {
		return fmt.Errorf("failed to write secret to temp file: %w", writeErr)
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		return fmt.Errorf("failed to close temp file: %w", closeErr)
	}

	createResult := cmd.Run("oc", "apply", "-n", cfg.Namespace, "-f", tmpFile.Name())
	if createResult.ExitCode != 0 {
		log.Printf("Warning: apply metrics-client-certs to %s failed: %s",
			cfg.Namespace, createResult.Stderr())
	}

	url := fmt.Sprintf("https://%s.%s.svc:%d/metrics", cfg.ServiceName, cfg.Namespace, metricsPort)

	// Test WITH client cert — should succeed
	withCertResult := cmd.Run("oc", "run", "mtls-curl-test-with-cert",
		"-n", cfg.Namespace,
		"--image=registry.access.redhat.com/ubi9/ubi-minimal:latest",
		"--restart=Never", "--rm", "-i",
		"--overrides", fmt.Sprintf(`{
			"spec": {
				"containers": [{
					"name": "curl",
					"image": "registry.access.redhat.com/ubi9/ubi-minimal:latest",
					"command": ["curl", "-sf", "--cert", "/certs/tls.crt", "--key", "/certs/tls.key", "--cacert", "/certs/tls.crt", "-o", "/dev/null", "-w", "%%{http_code}", "%s"],
					"volumeMounts": [{"name": "certs", "mountPath": "/certs", "readOnly": true}]
				}],
				"volumes": [{
					"name": "certs",
					"secret": {"secretName": "metrics-client-certs"}
				}],
				"restartPolicy": "Never"
			}
		}`, url),
		"--", "sh", "-c", "true")
	if withCertResult.ExitCode != 0 {
		log.Printf("TLS handshake with client cert output: %s", withCertResult.Combined())
		return fmt.Errorf("tls handshake with client cert failed for %s: %s", url, withCertResult.Stderr())
	}

	// Test WITHOUT client cert — should fail
	withoutCertResult := cmd.Run("oc", "run", "mtls-curl-test-no-cert",
		"-n", cfg.Namespace,
		"--image=registry.access.redhat.com/ubi9/ubi-minimal:latest",
		"--restart=Never", "--rm", "-i",
		"--command", "--", "curl", "-sf", "--cacert", "/dev/null",
		"-o", "/dev/null", "-w", "%{http_code}", url)
	if withoutCertResult.ExitCode == 0 {
		return fmt.Errorf("tls handshake without client cert should have failed for %s but succeeded",
			url)
	}

	log.Printf("TLS handshake check passed for %s: with-cert=success, without-cert=rejected",
		cfg.ServiceName)
	return nil
}

// AssertPlainHTTPHandshake verifies that the metrics endpoint is accessible via plain HTTP
// without requiring a client certificate.
func AssertPlainHTTPHandshake(cs *clients.Clients, cfg MTLSComponentConfig) error {
	svc, err := cs.KubeClient.Kube.CoreV1().Services(cfg.Namespace).Get(
		context.Background(), cfg.ServiceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Service %s/%s: %w", cfg.Namespace, cfg.ServiceName, err)
	}

	var metricsPort int32
	for _, port := range svc.Spec.Ports {
		if port.Name == cfg.HTTPPortName {
			metricsPort = port.Port
			break
		}
	}
	if metricsPort == 0 {
		// Try any port if no exact match
		if len(svc.Spec.Ports) > 0 {
			metricsPort = svc.Spec.Ports[0].Port
		} else {
			return fmt.Errorf("service %s/%s: no ports found",
				cfg.Namespace, cfg.ServiceName)
		}
	}

	url := fmt.Sprintf("http://%s.%s.svc:%d/metrics", cfg.ServiceName, cfg.Namespace, metricsPort)
	result := cmd.Run("oc", "run", "http-check",
		"-n", cfg.Namespace,
		"--image=registry.access.redhat.com/ubi9/ubi-minimal:latest",
		"--restart=Never", "--rm", "-i",
		"--command", "--", "curl", "-sf", "-o", "/dev/null", "-w", "%{http_code}", url)
	if result.ExitCode != 0 {
		return fmt.Errorf("plain HTTP check failed for %s: %s", url, result.Stderr())
	}

	log.Printf("Plain HTTP check passed for %s", cfg.ServiceName)
	return nil
}

// WaitForTektonConfigReady waits until TektonConfig reports Ready status.
func WaitForTektonConfigReady(cs *clients.Clients) error {
	return wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true,
		func(context.Context) (bool, error) {
			result := cmd.Run("oc", "get", "tektonconfig", "config",
				"-o", "jsonpath={.status.conditions[?(@.type==\"Ready\")].status}")
			if result.ExitCode != 0 {
				log.Printf("Waiting for TektonConfig Ready: %s", result.Stderr())
				return false, nil
			}
			status := strings.TrimSpace(result.Stdout())
			if status == "True" {
				return true, nil
			}
			log.Printf("TektonConfig Ready status: %s (waiting for True)", status)
			return false, nil
		})
}

// SetEnableMetricsMTLS patches TektonConfig to set spec.platforms.openshift.enableMetricsMTLS.
func SetEnableMetricsMTLS(enabled bool) {
	patchData := fmt.Sprintf(
		`{"spec":{"platforms":{"openshift":{"enableMetricsMTLS":%t}}}}`, enabled)
	cmd.MustSucceed("oc", "patch", "tektonconfig", "config", "-p", patchData, "--type=merge")
	log.Printf("Patched TektonConfig enableMetricsMTLS=%t", enabled)
}

// GetEnableMetricsMTLS reads the current value of spec.platforms.openshift.enableMetricsMTLS.
// Returns nil if the field is not set.
func GetEnableMetricsMTLS() *bool {
	result := cmd.Run("oc", "get", "tektonconfig", "config",
		"-o", "jsonpath={.spec.platforms.openshift.enableMetricsMTLS}")
	if result.ExitCode != 0 || strings.TrimSpace(result.Stdout()) == "" {
		return nil
	}
	val := strings.TrimSpace(result.Stdout())
	if val == "true" {
		b := true
		return &b
	}
	b := false
	return &b
}

// AssertNoStuckInstallerSets verifies that no TektonInstallerSets are in a non-ready state.
func AssertNoStuckInstallerSets(_ *clients.Clients) error {
	result := cmd.Run("oc", "get", "tektoninstallersets", "-o",
		"jsonpath={range .items[*]}{.metadata.name}={.status.conditions[?(@.type==\"Ready\")].status}{\"\\n\"}{end}")
	if result.ExitCode != 0 {
		return fmt.Errorf("failed to list TektonInstallerSets: %s", result.Stderr())
	}

	stuckSets := []string{}
	for line := range strings.SplitSeq(strings.TrimSpace(result.Stdout()), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		name := parts[0]
		status := parts[1]
		if status != "True" {
			stuckSets = append(stuckSets, fmt.Sprintf("%s(%s)", name, status))
		}
	}

	if len(stuckSets) > 0 {
		return fmt.Errorf("stuck InstallerSets: %s", strings.Join(stuckSets, ", "))
	}
	log.Printf("All TektonInstallerSets are Ready")
	return nil
}

// ── internal helpers ─────────────────────────────────────────────────────────

func findPodsForService(cs *clients.Clients, namespace, serviceName string) ([]corev1.Pod, error) {
	svc, err := cs.KubeClient.Kube.CoreV1().Services(namespace).Get(
		context.Background(), serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get Service %s/%s: %w", namespace, serviceName, err)
	}

	if len(svc.Spec.Selector) == 0 {
		return nil, fmt.Errorf("service %s/%s has no selector", namespace, serviceName)
	}

	labelSelector := metav1.FormatLabelSelector(&metav1.LabelSelector{
		MatchLabels: svc.Spec.Selector,
	})

	pods, err := cs.KubeClient.Kube.CoreV1().Pods(namespace).List(
		context.Background(), metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods for Service %s/%s: %w", namespace, serviceName, err)
	}

	// Filter to running pods only
	running := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			running = append(running, pod)
		}
	}
	return running, nil
}

func envToMap(envVars []corev1.EnvVar) map[string]string {
	m := make(map[string]string, len(envVars))
	for _, env := range envVars {
		m[env.Name] = env.Value
	}
	return m
}

func volumeNames(volumes []corev1.Volume) []string {
	names := make([]string, 0, len(volumes))
	for _, v := range volumes {
		names = append(names, v.Name)
	}
	return names
}

func formatPortNames(ports []corev1.ServicePort) string {
	names := make([]string, 0, len(ports))
	for _, p := range ports {
		names = append(names, fmt.Sprintf("%s(%d)", p.Name, p.Port))
	}
	return strings.Join(names, ", ")
}

// jsonPath navigates a nested map[string]any by the given keys.
func jsonPath(obj map[string]any, keys ...string) (any, error) {
	var current any = obj
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("key %q: parent is not an object", key)
		}
		val, exists := m[key]
		if !exists {
			return nil, fmt.Errorf("key %q not found", key)
		}
		current = val
	}
	return current, nil
}
