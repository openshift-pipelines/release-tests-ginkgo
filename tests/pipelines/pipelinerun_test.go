package pipelines_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // Gomega convention
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	occmd "github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

var oc = occmd.OC{}

var _ = Describe("Run sample pipeline: PIPELINES-03-TC01", Label("e2e", "pipelines", "non-admin"), func() {
	It("should run sample pipeline", func() {
		ns := lastNamespace
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "output-pipeline-run-v1b1", "successful", ns)
	})
})

var _ = Describe("Pipelinerun Timeout failure: PIPELINES-03-TC04", Label("e2e", "pipelines", "non-admin", "sanity"), func() {
	It("should fail with timeout", SpecTimeout(10*time.Minute), func(_ SpecContext) {
		ns := lastNamespace
		oc.Create("testdata/v1beta1/pipelinerun/pipelineruntimeout.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "pear", "timeout", ns)
	})
})

var _ = Describe("Configure execution results at Task level: PIPELINES-03-TC05", Label("e2e", "pipelines", "integration", "non-admin", "sanity"), func() {
	It("should configure task level results", func() {
		ns := lastNamespace
		oc.Create("testdata/v1beta1/pipelinerun/task_results_example.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "task-level-results", "successful", ns)
	})
})

var _ = Describe("Cancel pipelinerun: PIPELINES-03-TC06", Label("e2e", "pipelines", "integration", "non-admin", "sanity"), func() {
	It("should cancel a running pipelinerun", SpecTimeout(10*time.Minute), func(_ SpecContext) {
		ns := lastNamespace
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "output-pipeline-run-v1b1", "canceled", ns)
	})
})

var _ = Describe("Pipelinerun with pipelinespec and taskspec: PIPELINES-03-TC07", Label("e2e", "pipelines", "integration", "non-admin"), func() {
	It("should run pipelinerun with pipelinespec and taskspec", SpecTimeout(10*time.Minute), func(ctx SpecContext) {
		ns := lastNamespace
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun-with-pipelinespec-and-taskspec.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "pipelinerun-with-pipelinespec-taskspec-vb", "successful", ns)
		parent, err := sharedClients.PipelineRunClient.Get(ctx, "pipelinerun-with-pipelinespec-taskspec-vb", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		expectNestedRuns(ctx, parent)
		task := nestedTask(ctx, parent, "echo-message")
		tasks, err := sharedClients.TaskRunClient.List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(tasks.Items).To(HaveLen(1), "the ordinary control must execute only its own TaskRun")
		Expect(tasks.Items[0].UID).To(Equal(task.UID))
	})
})

var _ = Describe("Pipelinerun with large result: PIPELINES-03-TC08", Label("e2e", "pipelines", "integration", "non-admin", "results", "sanity"), func() {
	It("should run pipelinerun with large result", func() {
		ns := lastNamespace
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun-with-large-result.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "result-test-run", "successful", ns)
	})
})
