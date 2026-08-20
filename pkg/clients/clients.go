// Package clients provides Kubernetes and Tekton client wrappers for use in integration tests.
package clients

import (
	"context"
	"fmt"

	pacclientset "github.com/openshift-pipelines/pipelines-as-code/pkg/generated/clientset/versioned/typed/pipelinesascode/v1alpha1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	olm "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apclient "github.com/openshift-pipelines/manual-approval-gate/pkg/client/clientset/versioned/typed/approvaltask/v1alpha1"
	configV1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	consolev1 "github.com/openshift/client-go/console/clientset/versioned/typed/console/v1"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	olmversioned "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	"github.com/tektoncd/operator/pkg/client/clientset/versioned"
	operatorv1alpha1 "github.com/tektoncd/operator/pkg/client/clientset/versioned/typed/operator/v1alpha1"
	pversioned "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	v1 "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1"
	triggersclientset "github.com/tektoncd/triggers/pkg/client/clientset/versioned"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// KubeClient holds instances of interfaces for making requests to kubernetes client.
type KubeClient struct {
	Kube *kubernetes.Clientset
}

// Clients holds instances of interfaces for making requests to Tekton Pipelines.
type Clients struct {
	KubeClient         *KubeClient
	Ctx                context.Context
	Dynamic            dynamic.Interface
	Operator           operatorv1alpha1.OperatorV1alpha1Interface
	KubeConfig         *rest.Config
	Scheme             *runtime.Scheme
	OLM                olmversioned.Interface
	Route              routev1.RouteV1Interface
	ProxyConfig        configV1.ConfigV1Interface
	ClusterVersion     configV1.ClusterVersionInterface
	ConsoleCLIDownload consolev1.ConsoleCLIDownloadInterface
	Tekton             pversioned.Interface
	PipelineClient     v1.PipelineInterface
	PacClientset       pacclientset.PipelinesascodeV1alpha1Interface
	TaskClient         v1.TaskInterface
	TaskRunClient      v1.TaskRunInterface
	PipelineRunClient  v1.PipelineRunInterface
	TriggersClient     triggersclientset.Interface
	// NOTE: ClusterTaskInterface (v1beta1) was removed in tektoncd/pipeline v1.9.x.
	// ClusterTask resources are no longer supported upstream. Use Task instead.
	ApprovalTask apclient.ApprovalTaskInterface
}

// NewClients instantiates the clientsets using the selected kubeconfig and cluster.
func NewClients(configPath, clusterName, namespace string) (*Clients, error) {
	return newClients(configPath, clusterName, "", namespace)
}

// NewClientsWithContext instantiates the clientsets using an explicit context override.
func NewClientsWithContext(configPath, clusterName, contextName, namespace string) (*Clients, error) {
	return newClients(configPath, clusterName, contextName, namespace)
}

// newClients contains the shared clientset construction for both public entry points.
func newClients(configPath, clusterName, contextName, namespace string) (*Clients, error) {
	var err error
	scheme := createScheme()

	clients := &Clients{
		Scheme: scheme,
	}

	connection := "standard kubeconfig loading rules"
	if configPath != "" {
		connection = fmt.Sprintf("kubeconfig %q", configPath)
	}
	if contextName != "" {
		connection += fmt.Sprintf(", context %q", contextName)
	}
	if clusterName != "" {
		connection += fmt.Sprintf(", cluster %q", clusterName)
	}

	clients.KubeClient, clients.KubeConfig, err = newKubeClient(configPath, clusterName, contextName)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubeclient using %s: %w", connection, err)
	}

	// We poll, so set our limits high.
	clients.KubeConfig.QPS = 100
	clients.KubeConfig.Burst = 200

	ctx := context.Background()
	// ctx, cancel := context.WithCancel(ctx)
	// defer cancel()
	clients.Ctx = ctx

	clients.Dynamic, err = dynamic.NewForConfig(clients.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic clients using %s: %w", connection, err)
	}

	clients.Operator, err = newTektonOperatorAlphaClients(clients.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Operator v1alpha1 clients using %s: %w", connection, err)
	}

	clients.OLM, err = olmversioned.NewForConfig(clients.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create olm clients using %s: %w", connection, err)
	}

	clients.Tekton, err = pversioned.NewForConfig(clients.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline clientset using %s: %w", connection, err)
	}

	clients.TriggersClient, err = triggersclientset.NewForConfig(clients.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create triggers clientset using %s: %w", connection, err)
	}

	clients.PacClientset, err = pacclientset.NewForConfig(clients.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pac clientset using %s: %w", connection, err)
	}
	clients.NewClientSet(namespace)
	return clients, nil
}

// NewKubeClient creates a Kubernetes client using the selected kubeconfig and cluster.
func NewKubeClient(configPath, clusterName string) (*KubeClient, *rest.Config, error) {
	return newKubeClient(configPath, clusterName, "")
}

// newKubeClient creates a Kubernetes client with an optional context override.
func newKubeClient(configPath, clusterName, contextName string) (*KubeClient, *rest.Config, error) {
	cfg, err := buildClientConfig(configPath, clusterName, contextName)
	if err != nil {
		return nil, nil, err
	}

	k, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	return &KubeClient{Kube: k}, cfg, nil
}

// BuildClientConfig builds a REST config using client-go's standard loading rules.
func BuildClientConfig(kubeConfigPath, clusterName string) (*rest.Config, error) {
	return buildClientConfig(kubeConfigPath, clusterName, "")
}

// BuildClientConfigWithContext builds a REST config with an explicit context override.
func BuildClientConfigWithContext(kubeConfigPath, clusterName, contextName string) (*rest.Config, error) {
	return buildClientConfig(kubeConfigPath, clusterName, contextName)
}

// buildClientConfig applies an optional context override to the standard loading rules.
func buildClientConfig(kubeConfigPath, clusterName, contextName string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeConfigPath != "" {
		loadingRules.ExplicitPath = kubeConfigPath
	}

	overrides := clientcmd.ConfigOverrides{CurrentContext: contextName}
	if clusterName != "" {
		overrides.Context.Cluster = clusterName
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &overrides).ClientConfig()
}

func newTektonOperatorAlphaClients(cfg *rest.Config) (operatorv1alpha1.OperatorV1alpha1Interface, error) {
	cs, err := versioned.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return cs.OperatorV1alpha1(), nil
}

// TektonPipeline returns the TektonPipeline interface client.
func (c *Clients) TektonPipeline() operatorv1alpha1.TektonPipelineInterface {
	return c.Operator.TektonPipelines()
}

// TektonTrigger returns the TektonTrigger interface client.
func (c *Clients) TektonTrigger() operatorv1alpha1.TektonTriggerInterface {
	return c.Operator.TektonTriggers()
}

// TektonChains returns the TektonChain interface client.
func (c *Clients) TektonChains() operatorv1alpha1.TektonChainInterface {
	return c.Operator.TektonChains()
}

// TektonHub returns the TektonHub interface client.
func (c *Clients) TektonHub() operatorv1alpha1.TektonHubInterface {
	return c.Operator.TektonHubs()
}

// TektonDashboard returns the TektonDashboard interface client.
func (c *Clients) TektonDashboard() operatorv1alpha1.TektonDashboardInterface {
	return c.Operator.TektonDashboards()
}

// TektonAddon returns the TektonAddon interface client.
func (c *Clients) TektonAddon() operatorv1alpha1.TektonAddonInterface {
	return c.Operator.TektonAddons()
}

// TektonConfig returns the TektonConfig interface client.
func (c *Clients) TektonConfig() operatorv1alpha1.TektonConfigInterface {
	return c.Operator.TektonConfigs()
}

// ManualApprovalGate returns the ManualApprovalGate interface client.
func (c *Clients) ManualApprovalGate() operatorv1alpha1.ManualApprovalGateInterface {
	return c.Operator.ManualApprovalGates()
}

// PipelinesAsCode returns the OpenShiftPipelinesAsCode interface client.
func (c *Clients) PipelinesAsCode() operatorv1alpha1.OpenShiftPipelinesAsCodeInterface {
	return c.Operator.OpenShiftPipelinesAsCodes()
}

// NewClientSet initializes the per-namespace Tekton resource clients.
func (c *Clients) NewClientSet(namespace string) {
	c.PipelineClient = c.Tekton.TektonV1().Pipelines(namespace)
	c.TaskClient = c.Tekton.TektonV1().Tasks(namespace)
	c.TaskRunClient = c.Tekton.TektonV1().TaskRuns(namespace)
	c.PipelineRunClient = c.Tekton.TektonV1().PipelineRuns(namespace)
	c.Route = routev1.NewForConfigOrDie(c.KubeConfig)
	c.ProxyConfig = configV1.NewForConfigOrDie(c.KubeConfig)
	c.ClusterVersion = configV1.NewForConfigOrDie(c.KubeConfig).ClusterVersions()
	c.ConsoleCLIDownload = consolev1.NewForConfigOrDie(c.KubeConfig).ConsoleCLIDownloads()
	c.ApprovalTask = apclient.NewForConfigOrDie(c.KubeConfig).ApprovalTasks(namespace)
	c.PacClientset = pacclientset.NewForConfigOrDie(c.KubeConfig)
}

// NewClientFromKubeconfig creates a controller-runtime client from the provided kubeconfig.
func (c *Clients) NewClientFromKubeconfig(kubeconfigPath, clusterName, contextName string) (client.Client, error) {
	config, err := BuildClientConfigWithContext(kubeconfigPath, clusterName, contextName)
	if err != nil {
		return nil, fmt.Errorf("failed to build REST config from kubeconfig: %w", err)
	}

	if c == nil {
		return nil, fmt.Errorf("cannot create controller-runtime client from a nil Clients receiver")
	}
	if c.Scheme == nil {
		return nil, fmt.Errorf("cannot create controller-runtime client without a configured scheme")
	}

	k8sClient, err := client.New(config, client.Options{Scheme: c.Scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create controller-runtime client: %w", err)
	}
	return k8sClient, nil
}

func createScheme() *runtime.Scheme {
	// Register standard Kubernetes API types to the scheme
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(olm.AddToScheme(scheme))
	utilruntime.Must(operatorsv1.AddToScheme(scheme))

	return scheme
}
