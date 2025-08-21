package release_tests_ginkgo_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	cli "github.com/srivickynesh/release-tests-ginkgo/pkg/clients"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/k8s"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/pac"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/pipelines"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/store"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	pacclientset "github.com/openshift-pipelines/pipelines-as-code/pkg/generated/clientset/versioned/typed/pipelinesascode/v1alpha1"
)

var _ = BeforeSuite(func() {
	// Initialize Kubernetes client for BeforeSuite cleanup
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)
	restConfig, err := kubeconfig.ClientConfig()
	Expect(err).ToNot(HaveOccurred())

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	Expect(err).ToNot(HaveOccurred())

	c := &cli.Clients{ // Use cli alias here
		KubeClient: &cli.KubeClient{ // Use cli alias here
			Kube: kubeClient,
		},
		KubeConfig: restConfig,
	}

	// Perform cleanup with the initialized client
	pac.CleanupPAC(c, "gosmee-client", "openshift-pipelines")
})

var _ = BeforeEach(func() {
	// Initialize Kubernetes client
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)
	restConfig, err := kubeconfig.ClientConfig()
	Expect(err).ToNot(HaveOccurred())

	kubeClient, err := kubernetes.NewForConfig(restConfig)
	Expect(err).ToNot(HaveOccurred())

	// Initialize PacClientset
	pacClientset, err := pacclientset.NewForConfig(restConfig)
	Expect(err).ToNot(HaveOccurred())

	c := &cli.Clients{
		KubeClient: &cli.KubeClient{
			Kube: kubeClient,
		},
		KubeConfig: restConfig,
		PacClientset: pacClientset, // Add PacClientset here
	}
	store.PutScenarioData("clients", c)
	store.PutScenarioData("namespace", "openshift-pipelines")
})

var _ = Describe("Pipelines As Code tests", func() {

	Describe("Configure PAC in GitLab Project: PIPELINES-30-TC01", func() {
		It("Setup Gitlab Client", func() {
			c := pac.InitGitLabClient()
			pac.SetGitLabClient(c)
		})

		It("Validate PAC Info Install", func() {
			pac.AssertPACInfoInstall()
		})

		It("Create Smee deployment", func() {
			pac.SetupSmeeDeployment()
			k8s.ValidateDeployments(store.Clients(), store.Namespace(), store.GetScenarioData("smeeDeploymentName").(string))
		})

		It("Configure GitLab repo for \"pull_request\" in \"main\"", func() {
			pac.SetupGitLabProject()
			pac.GeneratePipelineRunYaml("pull_request", "main")
		})

		It("Configure PipelineRun", func() {
			pac.ConfigurePreviewChanges()
		})

		It("Validate PipelineRun for \"success\"", func() {
			pipelineName := pac.GetPipelineNameFromMR()
			pipelines.ValidatePipelineRun(store.Clients(), pipelineName, "success", "no", store.Namespace())
		})

		

	AfterEach(func() {
		smeeDeploymentName := store.GetScenarioData("smeeDeploymentName")
		namespace := store.Namespace()

		var smeeName string
		if smeeDeploymentName != nil {
			smeeName = smeeDeploymentName.(string)
		}

		pac.CleanupPAC(store.Clients(), smeeName, namespace)
	})
	})
})
