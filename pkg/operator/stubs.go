package operator

// stubs.go contains stub functions for OLM install/upgrade test specs.
// These stubs wrap oc/cmd calls directly and will be replaced with full
// implementations as each test area's migration phase completes.
// TODO: Replace stubs with proper implementations from reference-repo migrations.

import (
	"log"
	"time"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
	approvalgate "github.com/openshift-pipelines/release-tests-ginkgo/pkg/manualapprovalgate"
	"github.com/tektoncd/operator/test/utils"
)

// DefineArtifactHubAPIVariable is a placeholder for setting the artifact hub API variable.
func DefineArtifactHubAPIVariable() {
	log.Println("Defining artifact hub API variable (stub)")
}

// VerifyNamespaceExists checks that a namespace exists via oc.
func VerifyNamespaceExists(namespace string) {
	cmd.MustSucceed("oc", "get", "namespace", namespace)
}

// ConfigureGitResolverToken configures the GitHub token for git resolver in TektonConfig.
func ConfigureGitResolverToken(cs *clients.Clients) {
	log.Println("Configuring git resolver token (stub)")
}

// ConfigureBundlesResolver configures the bundles resolver in TektonConfig.
func ConfigureBundlesResolver(cs *clients.Clients) {
	log.Println("Configuring bundles resolver (stub)")
}

// EnableConsolePluginOperator enables the console plugin in TektonConfig.
func EnableConsolePluginOperator(cs *clients.Clients) {
	log.Println("Enabling console plugin (stub)")
}

// EnableStatefulSet enables statefulset mode in TektonConfig.
func EnableStatefulSet(cs *clients.Clients) {
	log.Println("Enabling statefulset mode (stub)")
}

// EnableStatefulSetForComponent enables statefulset for a specific component.
func EnableStatefulSetForComponent(cs *clients.Clients, component string) {
	log.Printf("Enabling statefulset for component %s (stub)", component)
}

// ValidateTriggersDeployment validates the triggers deployment.
func ValidateTriggersDeployment(cs *clients.Clients) {
	k8s.ValidateDeployments(cs, config.TargetNamespace,
		config.TriggerControllerName, config.TriggerWebhookName)
}

// ValidatePACDeployment validates the PAC deployment.
func ValidatePACDeployment(cs *clients.Clients) {
	k8s.ValidateDeployments(cs, config.TargetNamespace,
		config.PacControllerName, config.PacWatcherName, config.PacWebhookName)
}

// EnableChainsSigningSecret enables generateSigningSecret for Tekton Chains.
func EnableChainsSigningSecret(cs *clients.Clients) {
	log.Println("Enabling chains signing secret (stub)")
}

// ValidateHubDeployment validates the hub deployment.
func ValidateHubDeployment(cs *clients.Clients) {
	k8s.ValidateDeployments(cs, config.TargetNamespace,
		config.HubApiName, config.HubDbName, config.HubUiName)
}

// ValidateStatefulSetDeployment validates a statefulset deployment.
func ValidateStatefulSetDeployment(cs *clients.Clients, name string) {
	log.Printf("Validating statefulset deployment %s (stub)", name)
}

// ValidateTknServerCLI validates the tkn server CLI deployment.
func ValidateTknServerCLI(cs *clients.Clients) {
	k8s.ValidateDeployments(cs, config.TargetNamespace, config.TknDeployment)
}

// ValidateConsolePluginDeployment validates the console plugin deployment.
func ValidateConsolePluginDeployment(cs *clients.Clients) {
	k8s.ValidateDeployments(cs, config.TargetNamespace, config.ConsolePluginDeployment)
}

// ConfigureResultsWithLoki configures Results with Loki integration.
func ConfigureResultsWithLoki(cs *clients.Clients) {
	log.Println("Configuring Results with Loki (stub)")
}

// VerifyTektonAddonsStatus verifies TektonAddons installation status.
func VerifyTektonAddonsStatus(cs *clients.Clients) {
	log.Println("Verifying TektonAddons status (stub)")
}

// ValidateAutoPruneCronjob validates the default auto prune cronjob.
func ValidateAutoPruneCronjob(cs *clients.Clients) {
	log.Println("Validating auto prune cronjob (stub)")
}

// ValidateMAGDeployment validates the Manual Approval Gate deployment.
func ValidateMAGDeployment(cs *clients.Clients) {
	names := utils.ResourceNames{ManualApprovalGate: "manual-approval-gate"}
	approvalgate.EnsureManualApprovalGateExists(cs.ManualApprovalGate(), names)
	k8s.ValidateDeployments(cs, config.TargetNamespace,
		config.MAGController, config.MAGWebHook)
}

// ValidateTektonInstallerSetsStatus validates the status of all TektonInstallerSets.
func ValidateTektonInstallerSetsStatus(cs *clients.Clients) {
	cmd.MustSucceedIncreasedTimeout(time.Minute*5,
		"oc", "wait", "--for=condition=Ready", "tektoninstallerset", "--all", "--timeout=120s")
}

// ValidateTektonInstallerSetsNames validates the names of TektonInstallerSets.
func ValidateTektonInstallerSetsNames(cs *clients.Clients) {
	log.Println("Validating TektonInstallerSets names (stub)")
}

// ValidateOperatorInstalled verifies the operator is installed and running (post-upgrade).
func ValidateOperatorInstalled(cs *clients.Clients) {
	rnames := utils.ResourceNames{TektonConfig: "config"}
	ValidateOperatorInstallStatus(cs, rnames)
}
