package pac_test

import (
	. "github.com/onsi/ginkgo/v2"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pac"
)

var _ = Describe("Pipelines As Code tests", func() {

	Describe("Configure PAC in GitLab Project: PIPELINES-30-TC01", func() {
		It("Setup Gitlab Client", func() {
			c := pac.InitGitLabClient()
			pac.SetGitLabClient(c)
		})

		// It("Validate PAC Info Install", func() {
		// 	pac.AssertPACInfoInstall()
		// })

		// It("Create Smee deployment", func() {
		// 	pac.SetupSmeeDeployment()
		// 	k8s.ValidateDeployments(store.Clients(), store.Namespace(), store.GetScenarioData("smeeDeploymentName"))
		// })

		// It("Configure GitLab repo for \"pull_request\" in \"main\"", func() {
		// 	pac.SetupGitLabProject()
		// 	pac.GeneratePipelineRunYaml("pull_request", "main")
		// })

		// It("Configure PipelineRun", func() {
		// 	pac.ConfigurePreviewChanges()
		// })

		// It("Validate PipelineRun for \"success\"", func() {
		// 	pipelineName := pac.GetPipelineNameFromMR()
		// 	pipelines.ValidatePipelineRun(store.Clients(), pipelineName, "success", "no", store.Namespace())
		// })

		// It("Cleanup PAC", func() {
		// 	pac.CleanupPAC(store.Clients(), store.GetScenarioData("smeeDeploymentName"), store.Namespace())
		// })
	})
})
