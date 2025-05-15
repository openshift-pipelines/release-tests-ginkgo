package pac_test

import (
	"fmt"
	"log"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pac"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

var (
	testNamespace string
)

var _ = BeforeSuite(func() {
	// Initialize clients
	flags := config.Flags
	cs, err := clients.NewClients(flags.Kubeconfig, flags.Cluster, "")
	Expect(err).NotTo(HaveOccurred(), "failed to create clients")
	store.SetClients(cs)

	// Create or use test namespace
	testNamespace = os.Getenv("TEST_NAMESPACE")
	if testNamespace == "" {
		// Try GITLAB_GROUP_NAMESPACE or GITLAB_GROUP_NS as fallback
		testNamespace = os.Getenv("GITLAB_GROUP_NAMESPACE")
		if testNamespace == "" {
			testNamespace = os.Getenv("GITLAB_GROUP_NS")
		}
		if testNamespace == "" {
			testNamespace = fmt.Sprintf("pac-test-%d", time.Now().Unix())
		}
	}

	// Create namespace if it doesn't exist
	if !oc.CheckProjectExists(testNamespace) {
		oc.CreateNewProject(testNamespace)
	}
	store.SetNamespace(testNamespace)
})

var _ = AfterSuite(func() {
	// Cleanup can be added here if needed
	// For now, we'll leave the namespace for inspection
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
			// Verify the deployment exists
			smeeDeploymentName := store.GetScenarioData("smeeDeploymentName")
			Expect(smeeDeploymentName).NotTo(BeEmpty(), "Smee deployment name should be set")
			// Validate deployment exists using k8s package
			err := k8s.ValidateDeployments(store.Clients(), store.Namespace(), smeeDeploymentName)
			Expect(err).NotTo(HaveOccurred(), "Smee deployment should exist")
		})

		It("Configure GitLab repo for \"pull_request\" in \"main\"", func() {
			pac.SetupGitLabProject()
			pac.GeneratePipelineRunYaml("pull_request", "main")
		})

		It("Configure PipelineRun", func() {
			pac.ConfigurePreviewChanges()
		})

		It("Validate PipelineRun for \"success\"", func() {
			// This test requires a merge request to be created first (from ConfigurePreviewChanges)
			mrID := store.GetScenarioData("mrID")
			if mrID == "" {
				Skip("MR ID not found in scenario data. Ensure ConfigurePreviewChanges() ran successfully.")
			}
			pipelineName := pac.GetPipelineNameFromMR()
			Expect(pipelineName).NotTo(BeEmpty(), "Pipeline name should be retrieved from MR")
			// Validate the PipelineRun reached success status
			pipelines.ValidatePipelineRun(store.Clients(), pipelineName, "success", store.Namespace())
			log.Printf("PipelineRun %s validated successfully", pipelineName)
		})

		It("Cleanup PAC", func() {
			smeeDeploymentName := store.GetScenarioData("smeeDeploymentName")
			pac.CleanupPAC(store.Clients(), smeeDeploymentName, store.Namespace())
		})
	})
})
