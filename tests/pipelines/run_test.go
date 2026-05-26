package pipelines_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// Ensure Gomega and time are used.
var _ = Expect
var _ = time.Minute

var _ = Describe("PIPELINES-03-TC01: Run sample pipeline", Label("e2e", "pipelines", "non-admin"), func() {
	var ns string

	BeforeEach(func() {
		ns = store.Namespace()
		sharedClients.NewClientSet(ns)
	})

	It("should execute pipelinerun successfully", func() {
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "output-pipeline-run-v1b1", "successful", ns)
	})
})

var _ = Describe("PIPELINES-03-TC04: Pipelinerun Timeout failure", Label("e2e", "pipelines", "non-admin", "sanity"), func() {
	var ns string

	BeforeEach(func() {
		ns = store.Namespace()
		sharedClients.NewClientSet(ns)
	})

	It("should timeout when pipeline exceeds timeout", SpecTimeout(10*time.Minute), func(_ SpecContext) {
		oc.Create("testdata/v1beta1/pipelinerun/pipelineruntimeout.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "pear", "timeout", ns)
	})
})

var _ = Describe("PIPELINES-03-TC05: Configure execution results at Task level", Label("integration", "non-admin", "sanity"), func() {
	var ns string

	BeforeEach(func() {
		ns = store.Namespace()
		sharedClients.NewClientSet(ns)
	})

	It("should pass task results to pipeline", func() {
		oc.Create("testdata/v1beta1/pipelinerun/task_results_example.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "task-level-results", "successful", ns)
	})
})

var _ = Describe("PIPELINES-03-TC06: Cancel pipelinerun", Label("integration", "non-admin", "sanity"), func() {
	var ns string

	BeforeEach(func() {
		ns = store.Namespace()
		sharedClients.NewClientSet(ns)
	})

	It("should cancel running pipelinerun", SpecTimeout(10*time.Minute), func(_ SpecContext) {
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "output-pipeline-run-v1b1", "canceled", ns)
	})
})

var _ = Describe("PIPELINES-03-TC07: Pipelinerun with pipelinespec and taskspec", Label("integration", "non-admin"), func() {
	var ns string

	BeforeEach(func() {
		ns = store.Namespace()
		sharedClients.NewClientSet(ns)
	})

	It("should execute pipelinerun with embedded specs", func() {
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun-with-pipelinespec-and-taskspec.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "pipelinerun-with-pipelinespec-taskspec-vb", "successful", ns)
	})
})

var _ = Describe("PIPELINES-03-TC08: Pipelinerun with large result", Label("integration", "non-admin", "results", "sanity"), func() {
	var ns string

	BeforeEach(func() {
		ns = store.Namespace()
		sharedClients.NewClientSet(ns)
	})

	It("should handle large result values", func() {
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun-with-large-result.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "result-test-run", "successful", ns)
	})
})
