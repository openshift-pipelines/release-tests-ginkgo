// Package config provides shared constants, flags, and configuration helpers for integration tests.
package config

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// APIRetry defines the frequency at which we check for updates against the
	// k8s api when waiting for a specific condition to be true.
	APIRetry = time.Second * 5

	// APITimeout defines the amount of time we should spend querying the k8s api
	// when waiting for a specific condition to be true.
	APITimeout = time.Minute * 10
	// CLITimeout defines the amount of maximum execution time for CLI commands
	CLITimeout = time.Second * 90

	// ConsistentlyDuration sets  the default duration for Consistently. Consistently will verify that your condition is satisfied for this long.
	ConsistentlyDuration = 30 * time.Second

	// ResourceTimeout is the default timeout when waiting for a resource condition.
	ResourceTimeout = 60 * time.Second

	// TargetNamespace specify the name of Target namespace
	TargetNamespace = "openshift-pipelines"

	// PipelineControllerName is the name of the pipeline controller deployment.
	PipelineControllerName = "tekton-pipelines-controller"
	// PipelineControllerSA is the service account name for the pipeline controller.
	PipelineControllerSA = "tekton-pipelines-controller"

	// PipelineWebhookName is the name of the pipeline webhook deployment.
	PipelineWebhookName = "tekton-pipelines-webhook"
	// PipelineWebhookConfiguration is the name of the pipeline webhook configuration.
	PipelineWebhookConfiguration = "webhook.tekton.dev"
	// SccAnnotationKey is the annotation key used by the operator for SCCs.
	SccAnnotationKey = "operator.tekton.dev"

	// TriggerControllerName is the name of the trigger controller deployment.
	TriggerControllerName = "tekton-triggers-controller"
	// TriggerWebhookName is the name of the triggers webhook deployment.
	TriggerWebhookName = "tekton-triggers-webhook"

	// ChainsControllerName is the name of the chains controller deployment.
	ChainsControllerName = "tekton-chains-controller"

	// HubAPIName is the name of the Tekton Hub API deployment.
	HubAPIName = "tekton-hub-api"
	// HubDBName is the name of the Tekton Hub database deployment.
	HubDBName = "tekton-hub-db"
	// HubUIName is the name of the Tekton Hub UI deployment.
	HubUIName = "tekton-hub-ui"

	// MAGController is the name of the manual approval gate controller deployment.
	MAGController = "manual-approval-gate-controller"
	// MAGWebHook is the name of the manual approval gate webhook deployment.
	MAGWebHook = "manual-approval-gate-webhook"

	// PrunerSchedule is the default cron schedule for the auto pruner.
	PrunerSchedule = "0 8 * * *"
	// PrunerNamePrefix is the prefix used for pruner job names.
	PrunerNamePrefix = "tekton-resource-pruner-"

	// PacControllerName is the name of the PAC controller deployment.
	PacControllerName = "pipelines-as-code-controller"
	// PacWatcherName is the name of the PAC watcher deployment.
	PacWatcherName = "pipelines-as-code-watcher"
	// PacWebhookName is the name of the PAC webhook deployment.
	PacWebhookName = "pipelines-as-code-webhook"

	// TknDeployment is the name of the tkn CLI serve deployment.
	TknDeployment = "tkn-cli-serve"

	// ConsolePluginDeployment is the name of the Pipelines console plugin deployment.
	ConsolePluginDeployment = "pipelines-console-plugin"

	// TriggersSecretToken is a token used in triggers tests.
	TriggersSecretToken = "1234567"
)

// TektonInstallersetNamePrefixes lists the name prefixes of all TektonInstallerSet resources.
var TektonInstallersetNamePrefixes = [34]string{
	"addon-custom-consolecli",
	"addon-custom-openshiftconsole",
	"addon-custom-pipelinestemplate",
	"addon-custom-resolverstepaction",
	"addon-custom-resolvertask",
	"addon-custom-triggersresources",
	"addon-versioned-resolverstepactions",
	"addon-versioned-resolvertasks",
	"chain",
	"chain-config",
	"chain-secret",
	"console-link-hub",
	"manualapprovalgate-main-deployment",
	"manualapprovalgate-main-static",
	"openshiftpipelinesascode-main-deployment",
	"openshiftpipelinesascode-main-static",
	"openshiftpipelinesascode-post",
	"pipeline-main-statefulset",
	"pipeline-main-static",
	"pipeline-post",
	"pipeline-pre",
	"result",
	"result-post",
	"result-pre",
	"rhosp-rbac",
	"tekton-config-console-plugin-manifests",
	"tekton-hub-api",
	"tekton-hub-db",
	"tekton-hub-db-migration",
	"tekton-hub-ui",
	"tektoncd-pruner",
	"trigger-main-deployment",
	"trigger-main-static",
	"validating-mutating-webhook",
}

// PrefixesOfDefaultPipelines lists the name prefixes of default pipeline resources.
var PrefixesOfDefaultPipelines = [9]string{"buildah", "s2i-dotnet", "s2i-go", "s2i-java", "s2i-nodejs", "s2i-perl", "s2i-php", "s2i-python", "s2i-ruby"}

// Flags holds the command line flags or defaults for settings in the user's environment.
// See EnvironmentFlags for a list of supported fields
// Todo: change initialization of falgs when required by parsing them or from environment variable
var Flags = initializeFlags()

// StringArray is a flag type that allows a flag to be specified multiple times.
type StringArray []string

// String is the method to format the flag's value, part of the flag.Value interface.
func (s *StringArray) String() string {
	return strings.Join(*s, ", ")
}

// Set is the method to set the flag value, part of the flag.Value interface.
// Each time the flag is seen on the command line, Set is called.
// You can pass the addition values like
// --spoke-kubeconfig=$HOME/.kube/spoke-1 --spoke-kubeconfig=$HOME/.kube/spoke-2
// OR
// --spoke-kubeconfig=$HOME/.kube/spoke-1,$HOME/.kube/spoke-2
// OR Mix of both

// Set implements the flag.Value interface by appending comma-separated values to the array.
func (s *StringArray) Set(value string) error {
	*s = append(*s, strings.Split(value, ",")...)
	return nil
}

// EnvironmentFlags define the flags that are needed to run the e2e tests.
type EnvironmentFlags struct {
	Cluster          string      // K8s cluster (defaults to cluster in kubeconfig)
	Kubeconfig       string      // Path to kubeconfig (defaults to ./kube/config)
	Context          string      // K8s cluster (defaults to cluster in kubeconfig)
	SpokeKubeconfigs StringArray // Path to Spoke kubeconfig (No Defaults)
	SpokeContexts    StringArray // Name of the  Spoke Context (defaults to CurrentContext from SpokeKubeconfig)
	DockerRepo       string      // Docker repo (defaults to $KO_DOCKER_REPO)
	CSV              string      // Default csv openshift-pipelines-operator.v0.9.1
	Channel          string      // Default channel canary
	CatalogSource    string
	SubscriptionName string
	InstallPlan      string // Default Installationplan Automatic
	OperatorVersion  string
	TknVersion       string
	ClusterArch      string // Architecture of the cluster
	IsDisconnected   bool
}

func initializeFlags() *EnvironmentFlags {
	var f EnvironmentFlags
	flag.StringVar(&f.Cluster, "cluster", "",
		"Provide the cluster to test against. Defaults to the current cluster in kubeconfig.")

	flag.StringVar(&f.Context, "context", "",
		"Provide the context to test against. Defaults to the current context in kubeconfig.")

	var defaultKubeconfig string
	if os.Getenv("KUBECONFIG") != "" {
		defaultKubeconfig = os.Getenv("KUBECONFIG")
	} else if usr, err := user.Current(); err == nil {
		defaultKubeconfig = path.Join(usr.HomeDir, ".kube/config")
	}

	flag.StringVar(&f.Kubeconfig, "kubeconfig", defaultKubeconfig,
		"Provide the path to the `kubeconfig` file you'd like to use for these tests. The `current-context` will be used.")

	// SpokeKubeconfig is a Kubeconfig file which points to Spoke Cluster in MultiCluster environment.
	// When SpokeKubeconfig is not provided then there is no default.
	flag.Var(&f.SpokeKubeconfigs, "spoke-kubeconfig",
		"Provide the path to the `kubeconfig` file you'd like to use for these spoke tests.")

	// SpokeKubeconfig is a Kubeconfig file which points to Spoke Cluster in MultiCluster environment.
	// When SpokeKubeconfig is not provided then there is no default.
	flag.Var(&f.SpokeContexts, "spoke-context",
		"Provide the path to the `kubeconfig` file you'd like to use for these spoke tests.")

	defaultRepo := os.Getenv("KO_DOCKER_REPO")
	flag.StringVar(&f.DockerRepo, "dockerrepo", defaultRepo,
		"Provide the uri of the docker repo you have uploaded the test image to using `uploadtestimage.sh`. Defaults to $KO_DOCKER_REPO")

	defaultChannel := os.Getenv("CHANNEL")
	if defaultChannel == "" {
		defaultChannel = "latest"
	}
	flag.StringVar(&f.Channel, "channel", defaultChannel,
		"Provide channel to subcribe your operator you'd like to use for these tests. By default `canary` will be used.")

	defaultCatalogSource := os.Getenv("CATALOG_SOURCE")
	if defaultCatalogSource == "" {
		defaultCatalogSource = "redhat-operators"
	}
	flag.StringVar(&f.CatalogSource, "catalogsource", defaultCatalogSource,
		"Provide defaultCatalogSource to subscribe operator from. By default `custom-operators` will be used.")

	defaultSubscriptionName := os.Getenv("SUBSCRIPTION_NAME")
	if defaultSubscriptionName == "" {
		defaultSubscriptionName = "openshift-pipelines-operator-rh"
	}
	flag.StringVar(&f.SubscriptionName, "subscriptionName", defaultSubscriptionName,
		"Provide defaultSubscriptionName to operator, By default `openshift-pipelines-operator-rh` will be used.")

	defaultPlan := os.Getenv("INSTALL_PLAN")
	flag.StringVar(&f.InstallPlan, "installplan", defaultPlan,
		"Provide Install Approval plan for your operator you'd like to use for these tests. By default `Automatic` will be used.")

	defaultOpVersion := os.Getenv("CSV_VERSION")
	flag.StringVar(&f.OperatorVersion, "opversion", defaultOpVersion,
		"Provide Operator version for your operator you'd like to use for these tests. By default `v0.9.1` ")

	defaultCsv := os.Getenv("CSV")
	flag.StringVar(&f.CSV, "csv", defaultCsv+defaultOpVersion,
		"Provide csv for your operator you'd like to use for these tests. By default `openshift-pipelines-operator.v0.9.1` will be used.")

	defaultTkn := os.Getenv("TKN_VERSION")
	flag.StringVar(&f.TknVersion, "tknversion", defaultTkn,
		"Provide tknversion to download specified cli binary you'd like to use for these tests. By default `0.6.0` will be used.")

	defaultClusterArch := os.Getenv("ARCH")
	if defaultClusterArch != "" && strings.Contains(defaultClusterArch, "/") {
		defaultClusterArch = strings.Split(defaultClusterArch, "/")[1]
	}
	flag.StringVar(&f.ClusterArch, "clusterarch", defaultClusterArch,
		"Provide the architecture of testing cluster. By default `amd64` will be used.")

	isDiconnectedEnv := os.Getenv("IS_DISCONNECTED")
	defaultIsDiconnected, err := strconv.ParseBool(isDiconnectedEnv)
	if err != nil {
		defaultIsDiconnected = false
	}
	flag.BoolVar(&f.IsDisconnected, "isdisconnected", defaultIsDiconnected,
		"Provide the info if the testing cluster is disconnected. By default `false` will be used.")

	// Directly assign environment variable values to fields since flag.Parse() is not called
	// in Ginkgo tests. This ensures config values are available immediately.
	f.Kubeconfig = defaultKubeconfig
	f.DockerRepo = defaultRepo
	f.Channel = defaultChannel
	f.CatalogSource = defaultCatalogSource
	f.SubscriptionName = defaultSubscriptionName
	f.InstallPlan = defaultPlan
	f.OperatorVersion = defaultOpVersion
	f.CSV = defaultCsv + defaultOpVersion
	f.TknVersion = defaultTkn
	f.ClusterArch = defaultClusterArch
	f.IsDisconnected = defaultIsDiconnected

	return &f
}

// Dir returns the absolute path to the template directory.
func Dir() string {
	_, b, _, _ := runtime.Caller(0)
	configDir := path.Join(path.Dir(b), "..", "..", "template")
	return configDir
}

// File returns the absolute path of a file under the template directory.
func File(elem ...string) string {
	path := append([]string{Dir()}, elem...)
	return filepath.Join(path...)
}

// Read reads the contents of a file from the template directory.
func Read(path string) ([]byte, error) {
	return os.ReadFile(File(path))
}

// TempDir returns the path to the temporary directory, creating it if it does not exist.
func TempDir() (string, error) {
	tmp := filepath.Join(Dir(), "..", "tmp")
	if _, err := os.Stat(tmp); os.IsNotExist(err) {
		err := os.Mkdir(tmp, 0750)
		return tmp, err
	}
	return tmp, nil
}

// TempFile returns the full path of a file within the temporary directory.
func TempFile(elem ...string) (string, error) {
	tmp, err := TempDir()
	if err != nil {
		return "", err
	}
	path := append([]string{tmp}, elem...)
	return filepath.Join(path...), nil
}

// RemoveTempDir removes the temporary directory and all its contents.
func RemoveTempDir() error {
	tmp, err := TempDir()
	if err != nil {
		return fmt.Errorf("failed to get temp dir: %w", err)
	}
	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("error deleting directory %s: %w", tmp, err)
	}
	return nil
}

// Path returns the absolute path to a file under the testdata directory.
func Path(elem ...string) string {
	td := filepath.Join(Dir(), "..")
	if _, err := os.Stat(td); os.IsNotExist(err) {
		panic(fmt.Sprintf("test data path not found: %s", td))
	}
	return filepath.Join(append([]string{td}, elem...)...)
}
