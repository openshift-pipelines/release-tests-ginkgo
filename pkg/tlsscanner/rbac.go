// Package tlsscanner provides helpers for deploying and running the
// openshift/tls-scanner Job against the openshift-pipelines namespace,
// extracting scan artifacts, and asserting PQC compliance per component.
package tlsscanner

import (
	"log"
	"strings"
	"time"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
)

const (
	// ScannerNamespace is the dedicated namespace for the tls-scanner Job.
	ScannerNamespace = "tls-scanner"

	// ScannerImage is the pre-built tls-scanner container image.
	ScannerImage = "quay.io/jkhelil/tls-scanner:latest"

	// ScannerJobName is the Kubernetes Job name created per scan run.
	ScannerJobName = "tls-scanner-pipelines"

	// scannerSA is the ServiceAccount used by the scanner Job.
	scannerSA = "default"

	// crossNSRole / crossNSBinding are the ClusterRole and ClusterRoleBinding
	// that grant the scanner cross-namespace read access to pods and services.
	crossNSRole    = "tls-scanner-cross-namespace"
	crossNSBinding = "tls-scanner-cross-namespace"

	// artifactWaitSecs is how long the scanner pod sleeps after completing the
	// scan to allow artifact extraction via oc cp.
	artifactWaitSecs = "300"

	// ScanCompleteMarker is printed to stdout by the scanner shell command once
	// the scan binary exits, signaling that results are on disk.
	ScanCompleteMarker = "Pausing for artifact collection"

	// resultsDir is the emptyDir volume mount where the scanner writes output.
	resultsDir = "/artifacts"
)

// SetupScannerNamespace creates the tls-scanner OpenShift project, waiting
// for any pre-existing Terminating namespace to be fully removed first.
func SetupScannerNamespace() {
	// If a Terminating namespace exists (from a previous run), force-remove its
	// finalizer and wait until it is fully gone before recreating.
	for retry := 0; retry < 30; retry++ {
		result := cmd.Run("oc", "get", "namespace", ScannerNamespace,
			"-o", "jsonpath={.status.phase}")
		if result.ExitCode != 0 {
			break // namespace is gone
		}
		phase := strings.TrimSpace(result.Stdout())
		if phase == "Active" {
			log.Printf("Scanner namespace %q already exists and is Active", ScannerNamespace)
			return
		}
		// Namespace is Terminating — force-remove its finalizer once, then wait.
		if retry == 0 {
			log.Printf("Scanner namespace %q is %s; force-removing finalizer", ScannerNamespace, phase)
			_ = cmd.Run("oc", "patch", "namespace", ScannerNamespace,
				"--type=merge", `-p={"spec":{"finalizers":[]}}`)
		}
		log.Printf("Waiting for namespace %q to terminate... (%d/30)", ScannerNamespace, retry+1)
		time.Sleep(3 * time.Second)
	}
	oc.CreateNewProject(ScannerNamespace)
}

// SetupScannerRBAC grants the scanner's default ServiceAccount the permissions
// required by deploy.sh:
//   - cluster-reader ClusterRoleBinding
//   - privileged SCC
//   - tls-scanner-cross-namespace ClusterRole + ClusterRoleBinding
func SetupScannerRBAC() {
	saRef := "system:serviceaccount:" + ScannerNamespace + ":" + scannerSA

	log.Printf("Granting cluster-reader to %s", saRef)
	oc.AddClusterRoleToUser("cluster-reader", saRef)

	log.Printf("Granting privileged SCC to %s", saRef)
	oc.AddSCCToUser("privileged", saRef)

	// Delete and recreate the cross-namespace ClusterRole so setup is
	// idempotent across repeated test runs.
	_ = cmd.Run("oc", "delete", "clusterrole", crossNSRole,
		"--ignore-not-found=true")
	log.Printf("Creating ClusterRole %q", crossNSRole)
	cmd.MustSucceed("oc", "create", "clusterrole", crossNSRole,
		"--verb=get,list,watch",
		"--resource=pods,services,endpoints,namespaces,nodes,secrets")

	_ = cmd.Run("oc", "delete", "clusterrolebinding", crossNSBinding,
		"--ignore-not-found=true")
	log.Printf("Creating ClusterRoleBinding %q", crossNSBinding)
	cmd.MustSucceed("oc", "create", "clusterrolebinding", crossNSBinding,
		"--clusterrole="+crossNSRole,
		"--serviceaccount="+ScannerNamespace+":"+scannerSA)
}

// TeardownScannerRBAC removes all resources created by SetupScannerRBAC and
// deletes the tls-scanner project. Errors are logged but not fatal so that
// DeferCleanup always runs to completion.
func TeardownScannerRBAC() {
	saRef := "system:serviceaccount:" + ScannerNamespace + ":" + scannerSA

	log.Printf("Removing cluster-reader from %s", saRef)
	oc.RemoveClusterRoleFromUser("cluster-reader", saRef)

	log.Printf("Removing privileged SCC from %s", saRef)
	oc.RemoveSCCFromUser("privileged", saRef)

	log.Printf("Deleting ClusterRoleBinding %q", crossNSBinding)
	_ = cmd.Run("oc", "delete", "clusterrolebinding", crossNSBinding,
		"--ignore-not-found=true")

	log.Printf("Deleting ClusterRole %q", crossNSRole)
	_ = cmd.Run("oc", "delete", "clusterrole", crossNSRole,
		"--ignore-not-found=true")

	log.Printf("Deleting scanner project %q", ScannerNamespace)
	oc.DeleteProject(ScannerNamespace)
}
