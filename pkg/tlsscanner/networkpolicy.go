package tlsscanner

import (
	"fmt"
	"log"
	"strings"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

const (
	// consolePluginScannerNP is a temporary NetworkPolicy that allows the
	// tls-scanner Job (in ScannerNamespace) to reach pipelines-console-plugin
	// on TCP/8443. The operator-managed allow policy only admits
	// openshift-console; this dedicated policy avoids patching that one.
	consolePluginScannerNP = "pipelines-console-plugin-tls-scanner"

	// consolePluginAppLabel matches the console-plugin Deployment pod selector.
	consolePluginAppLabel = "pipelines-console-plugin"
)

// SetupConsolePluginScannerNetworkPolicy creates a temporary NetworkPolicy in
// config.TargetNamespace so pods in ScannerNamespace can ingress to
// pipelines-console-plugin on TCP/8443. Idempotent across repeated runs.
func SetupConsolePluginScannerNetworkPolicy() {
	ns := config.TargetNamespace
	log.Printf("Creating NetworkPolicy %q in namespace %q (ingress from %q)",
		consolePluginScannerNP, ns, ScannerNamespace)

	yaml := fmt.Sprintf(`apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: %s
  namespace: %s
spec:
  podSelector:
    matchLabels:
      app: %s
  policyTypes:
  - Ingress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: %s
    ports:
    - protocol: TCP
      port: 8443
`, consolePluginScannerNP, ns, consolePluginAppLabel, ScannerNamespace)

	cmd.MustSucceedWithStdin(strings.NewReader(yaml), "oc", "apply", "-f", "-")
}

// TeardownConsolePluginScannerNetworkPolicy deletes the temporary NetworkPolicy
// created by SetupConsolePluginScannerNetworkPolicy. Errors are ignored so
// DeferCleanup can always complete.
func TeardownConsolePluginScannerNetworkPolicy() {
	log.Printf("Deleting NetworkPolicy %q in namespace %q",
		consolePluginScannerNP, config.TargetNamespace)
	_ = cmd.Run("oc", "delete", "networkpolicy", consolePluginScannerNP,
		"-n", config.TargetNamespace, "--ignore-not-found=true")
}
