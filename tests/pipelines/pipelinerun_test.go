package pipelines_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega

	occmd "github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

var oc = occmd.OC{}

// Ensure Gomega and time are used.
var _ = Expect
var _ = time.Minute

var _ = Describe("Pipeline E2E", Label("e2e", "pipelines"), func() {
	var ns string

	BeforeEach(func() {
		ns = uniqueNS("pr-e2e")
		oc.CreateNewProject(ns)
		lastNamespace = ns
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})
	})

	It("Run sample pipeline", Label("non-admin"), func() {
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "output-pipeline-run-v1b1", "successful", ns)
	})

	It("Pipelinerun Timeout failure", Label("non-admin", "sanity"), SpecTimeout(10*time.Minute), func(_ SpecContext) {
		oc.Create("testdata/v1beta1/pipelinerun/pipelineruntimeout.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "pear", "timeout", ns)
	})

	It("Configure execution results at Task level", Label("integration", "non-admin", "sanity"), func() {
		oc.Create("testdata/v1beta1/pipelinerun/task_results_example.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "task-level-results", "successful", ns)
	})

	It("Cancel pipelinerun", Label("integration", "non-admin", "sanity"), SpecTimeout(10*time.Minute), func(_ SpecContext) {
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "output-pipeline-run-v1b1", "canceled", ns)
	})

	It("Pipelinerun with pipelinespec and taskspec", Label("integration", "non-admin"), func() {
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun-with-pipelinespec-and-taskspec.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "pipelinerun-with-pipelinespec-taskspec-vb", "successful", ns)
	})

	It("Pipelinerun with large result", Label("integration", "non-admin", "results", "sanity"), func() {
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun-with-large-result.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "result-test-run", "successful", ns)
	})
})
