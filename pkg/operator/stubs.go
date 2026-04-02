package operator

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
	approvalgate "github.com/openshift-pipelines/release-tests-ginkgo/pkg/manualapprovalgate"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/openshift"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/statefulset"
	"github.com/tektoncd/operator/test/utils"
)

// DefineArtifactHubAPIVariable patches TektonConfig to set the artifact-hub-api
// URL for the hub resolver, pointing to https://artifacthub.io/.
func DefineArtifactHubAPIVariable() {
	patchData := `{"spec":{"pipeline":{"hub-resolver-config":{"artifact-hub-api":"https://artifacthub.io/"}}}}`
	oc.UpdateTektonConfig(patchData)
}

// VerifyNamespaceExists checks that a namespace exists via oc.
func VerifyNamespaceExists(namespace string) {
	cmd.MustSucceed("oc", "get", "namespace", namespace)
}

// ConfigureGitResolverToken configures the GitHub token for git resolver in TektonConfig.
// If GITHUB_TOKEN is set and the secret does not already exist, it creates the secret
// and patches TektonConfig to reference it.
func ConfigureGitResolverToken(cs *clients.Clients) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Printf("Token for authorization to the GitHub repository was not exported as a system variable")
		return
	}
	if !oc.SecretExists("github-auth-secret", "openshift-pipelines") {
		oc.CreateSecretForGitResolver(token)
	} else {
		log.Printf("Secret \"github-auth-secret\" already exists")
	}
	patchData := `{"spec":{"pipeline":{"git-resolver-config":{"api-token-secret-key":"github-auth-key","api-token-secret-name":"github-auth-secret","api-token-secret-namespace":"openshift-pipelines","default-revision":"main","fetch-timeout":"1m","scm-type":"github"}}}}`
	oc.UpdateTektonConfig(patchData)
}

// ConfigureBundlesResolver patches TektonConfig to configure the bundles resolver
// with default-kind and default-service-account.
func ConfigureBundlesResolver(cs *clients.Clients) {
	patchData := `{"spec":{"pipeline":{"bundles-resolver-config":{"default-kind":"task","defaut-service-account":"pipelines"}}}}`
	oc.UpdateTektonConfig(patchData)
}

// EnableConsolePluginOperator enables the pipelines console plugin.
// It checks the OpenShift version (must be >= 4.15) and then enables the plugin.
func EnableConsolePluginOperator(cs *clients.Clients) {
	openshiftVersion := openshift.GetOpenShiftVersion(cs)
	Expect(openshiftVersion).NotTo(BeEmpty(),
		"Unknown version of OpenShift (cluster version %q)", openshiftVersion)

	parts := strings.Split(openshiftVersion, ".")
	Expect(len(parts)).To(BeNumerically(">=", 2),
		"Invalid OpenShift version format: %s", openshiftVersion)

	minorVersion, err := strconv.Atoi(parts[1])
	Expect(err).NotTo(HaveOccurred(),
		"Failed to parse OpenShift minor version from %s", openshiftVersion)

	if minorVersion < 15 {
		log.Printf("Console plugin is not supported on OpenShift version lower than 4.15 (cluster version %v).", openshiftVersion)
		return
	}

	oc.EnableConsolePlugin()
}

// EnableStatefulSet patches TektonConfig to enable StatefulSet mode for pipelines
// with HA, statefulset ordinals, 2 replicas and 2 buckets.
func EnableStatefulSet(cs *clients.Clients) {
	patchData := `{"spec":{"pipeline":{"performance":{"disable-ha":false,"statefulset-ordinals":true,"replicas":2,"buckets":2}}}}`
	oc.UpdateTektonConfig(patchData)
}

// EnableStatefulSetForComponent enables statefulset for a specific component
// (chains or results) in TektonConfig.
func EnableStatefulSetForComponent(cs *clients.Clients, component string) {
	var patchData string
	switch component {
	case "chains":
		patchData = `{"spec":{"chain":{"performance":{"disable-ha":false,"statefulset-ordinals":true,"replicas":2,"buckets":2}}}}`
	case "results":
		patchData = `{"spec":{"result":{"performance":{"disable-ha":false,"statefulset-ordinals":true,"replicas":2,"buckets":2}}}}`
	default:
		Fail(fmt.Sprintf("unsupported component: %s. Cannot generate patch data for statefulset", component))
	}
	oc.UpdateTektonConfig(patchData)
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

// EnableChainsSigningSecret enables generateSigningSecret for Tekton Chains
// in TektonConfig. If the signing-secrets secret does not exist or is empty,
// it creates/patches as needed.
func EnableChainsSigningSecret(cs *clients.Clients) {
	patchData := `{"spec":{"chain":{"generateSigningSecret":true}}}`
	if oc.SecretExists("signing-secrets", "openshift-pipelines") {
		log.Printf("Secrets \"signing-secrets\" already exists")
		if oc.GetSecretsData("signing-secrets", "openshift-pipelines") == "\"\"" {
			log.Printf("The \"signing-secrets\" does not contain any data")
			oc.UpdateTektonConfig(patchData)
		}
	} else {
		cmd.MustSucceed("oc", "create", "secret", "generic", "signing-secrets", "--namespace", "openshift-pipelines")
		oc.UpdateTektonConfig(patchData)
	}
}

// ValidateHubDeployment validates the hub deployment.
func ValidateHubDeployment(cs *clients.Clients) {
	k8s.ValidateDeployments(cs, config.TargetNamespace,
		config.HubApiName, config.HubDbName, config.HubUiName)
}

// ValidateStatefulSetDeployment validates a statefulset deployment by name.
func ValidateStatefulSetDeployment(cs *clients.Clients, name string) {
	log.Printf("Validating statefulset %v deployment\n", name)
	statefulset.ValidateStatefulSetDeployment(cs, name)
}

// ValidateTknServerCLI validates the tkn server CLI deployment.
func ValidateTknServerCLI(cs *clients.Clients) {
	if openshift.IsCapabilityEnabled(cs, "Console") {
		k8s.ValidateDeployments(cs, config.TargetNamespace, config.TknDeployment)
	} else {
		log.Printf("OpenShift Console is not enabled, skipping validation of tkn serve CLI deployment")
	}
}

// ValidateConsolePluginDeployment validates the console plugin deployment.
func ValidateConsolePluginDeployment(cs *clients.Clients) {
	if openshift.IsCapabilityEnabled(cs, "Console") {
		k8s.ValidateDeployments(cs, config.TargetNamespace, config.ConsolePluginDeployment)
	} else {
		log.Printf("OpenShift Console is not enabled, skipping validation of console plugin deployment")
	}
}

// ConfigureResultsWithLoki patches TektonConfig to configure Results with Loki
// integration for log storage.
func ConfigureResultsWithLoki(cs *clients.Clients) {
	patchData := `{"spec":{"result":{"auth_disable":true,"disabled":false,"log_level":"debug","loki_stack_name":"logging-loki","loki_stack_namespace":"openshift-logging"}}}`
	oc.UpdateTektonConfig(patchData)
}

// VerifyTektonAddonsStatus verifies that the TektonAddons CR reports
// InstallSucceeded status by listing installer sets with addon type.
func VerifyTektonAddonsStatus(cs *clients.Clients) {
	// Wait for the addon-related TektonInstallerSets to be ready.
	// This is the Ginkgo equivalent of EnsureTektonAddonsStatusInstalled
	// which would require the TektonAddon CR client. Using the oc wait
	// approach ensures addon installersets are in Ready state.
	cmd.MustSucceedIncreasedTimeout(time.Minute*5,
		"oc", "wait", "--for=condition=Ready", "tektoninstallerset",
		"-l", "operator.tekton.dev/type=addon", "--timeout=120s")
	log.Println("TektonAddons install status verified")
}

// ValidateAutoPruneCronjob validates that the default auto prune cronjob exists
// in the target namespace with the expected schedule and name prefix.
func ValidateAutoPruneCronjob(cs *clients.Clients) {
	cronJobs, err := cs.KubeClient.Kube.BatchV1().CronJobs(config.TargetNamespace).List(
		cs.Ctx, k8s.ListOptionsDefault())
	Expect(err).NotTo(HaveOccurred(),
		"failed to list cronjobs in namespace %s", config.TargetNamespace)
	Expect(cronJobs.Items).NotTo(BeEmpty(),
		"no cronjobs present in namespace %s", config.TargetNamespace)

	found := false
	for _, cj := range cronJobs.Items {
		if cj.Spec.Schedule == config.PrunerSchedule &&
			strings.Contains(cj.Name, config.PrunerNamePrefix) {
			found = true
			log.Printf("Cronjob with schedule %v and with name prefix %v is present",
				config.PrunerSchedule, config.PrunerNamePrefix)
			break
		}
	}
	Expect(found).To(BeTrue(),
		"no cronjob with schedule %v and prefix %v found in namespace %s",
		config.PrunerSchedule, config.PrunerNamePrefix, config.TargetNamespace)
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
	tis, err := cs.Operator.TektonInstallerSets().List(cs.Ctx, k8s.ListOptionsDefault())
	Expect(err).NotTo(HaveOccurred(), "error getting tektoninstallersets")

	failedInstallersets := make([]string, 0)
	for _, is := range tis.Items {
		log.Printf("Verifying if the installerset %s is in ready state", is.Name)
		if !is.Status.IsReady() {
			failedInstallersets = append(failedInstallersets, is.Name)
		}
	}

	Expect(failedInstallersets).To(BeEmpty(),
		"the installersets %s is/are not in ready status",
		strings.Join(failedInstallersets, ","))
	log.Print("All the installersets are in ready state")
}

// ValidateTektonInstallerSetsNames validates that all expected TektonInstallerSet
// name prefixes are present.
func ValidateTektonInstallerSetsNames(cs *clients.Clients) {
	tis, err := cs.Operator.TektonInstallerSets().List(cs.Ctx, k8s.ListOptionsDefault())
	Expect(err).NotTo(HaveOccurred(), "error getting tektoninstallersets")

	missingInstallersets := make([]string, 0)
	for _, isp := range config.TektonInstallersetNamePrefixes {
		if !openshift.IsCapabilityEnabled(cs, "Console") &&
			(isp == "addon-custom-consolecli" || isp == "addon-custom-openshiftconsole") {
			log.Printf("OpenShift Console is not enabled, skipping validation of installer set %s", isp)
			continue
		}

		if config.Flags.IsDisconnected && isp == "addon-custom-communityclustertask" {
			log.Printf("Testing on a disconnected cluster, skipping validation of installer set %s", isp)
			continue
		}

		log.Printf("Verifying if the installerset with prefix %s is present\n", isp)
		found := false
		for _, is := range tis.Items {
			if strings.HasPrefix(is.Name, isp) {
				found = true
				log.Printf("Installerset with prefix %s is present\n", isp)
				break
			}
		}

		if !found {
			missingInstallersets = append(missingInstallersets, isp)
		}
	}

	Expect(missingInstallersets).To(BeEmpty(),
		"installersets with prefix %s not found",
		strings.Join(missingInstallersets, ","))
}

// ValidateOperatorInstalled verifies the operator is installed and running (post-upgrade).
func ValidateOperatorInstalled(cs *clients.Clients) {
	rnames := utils.ResourceNames{TektonConfig: "config"}
	ValidateOperatorInstallStatus(cs, rnames)
}
