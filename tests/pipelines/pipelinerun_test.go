package pipelines_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

// Ensure Gomega and time are used.
var _ = Expect
var _ = time.Minute

var _ = Describe("PIPELINES-03: Pipeline E2E", Label("e2e", "pipelines"), func() {
	var ns string

	BeforeEach(func() {
		ns = uniqueNS("pr-e2e")
		oc.CreateNewProject(ns)
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})
	})

	It("PIPELINES-03-TC01: Run sample pipeline", Label("non-admin"), func() {
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "output-pipeline-run-v1b1", "successful", ns)
	})

	It("PIPELINES-03-TC04: Pipelinerun Timeout failure", Label("non-admin", "sanity"), SpecTimeout(10*time.Minute), func() {
		oc.Create("testdata/v1beta1/pipelinerun/pipelineruntimeout.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "pear", "timeout", ns)
	})

	It("PIPELINES-03-TC05: Configure execution results at Task level", Label("integration", "non-admin", "sanity"), func() {
		oc.Create("testdata/v1beta1/pipelinerun/task_results_example.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "task-level-results", "successful", ns)
	})

	It("PIPELINES-03-TC06: Cancel pipelinerun", Label("integration", "non-admin", "sanity"), SpecTimeout(10*time.Minute), func() {
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "output-pipeline-run-v1b1", "cancelled", ns)
	})

	It("PIPELINES-03-TC07: Pipelinerun with pipelinespec and taskspec", Label("integration", "non-admin"), func() {
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun-with-pipelinespec-and-taskspec.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "pipelinerun-with-pipelinespec-taskspec-vb", "successful", ns)
	})

	It("PIPELINES-03-TC08: Pipelinerun with large result", Label("integration", "non-admin", "results", "sanity"), func() {
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun-with-large-result.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "result-test-run", "successful", ns)
	})
})
