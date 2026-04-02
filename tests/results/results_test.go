package results_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
)

var _ = Describe("Tekton Results", Label("results", "e2e"), func() {

	Describe("PIPELINES-26-TC01: Test Tekton results with TaskRun", Label("sanity"), Ordered, func() {
		It("verifies golang imagestream exists", func() {
			cmd.MustSucceed("oc", "get", "is", "golang", "-n", "openshift")
		})

		It("applies taskrun fixture and verifies completion", func() {
			ns := config.TargetNamespace
			oc.Apply("testdata/results/taskrun.yaml", ns)

			// Wait for taskrun to complete
			cmd.MustSucceedIncreasedTimeout(time.Minute*5, "oc", "wait", "--for=condition=Succeeded", "taskrun/results-task", "-n", ns, "--timeout=120s")
		})

		It("verifies taskrun results are stored", func() {
			err := operator.VerifyResultsAnnotationStored(sharedClients, "taskrun")
			Expect(err).NotTo(HaveOccurred(), "Results annotation not stored for taskrun")
		})

		It("verifies taskrun results records", func() {
			err := operator.VerifyResultsRecords("taskrun")
			Expect(err).NotTo(HaveOccurred(), "Results records verification failed for taskrun")
		})

		It("verifies taskrun results logs", func() {
			err := operator.VerifyResultsLogs("taskrun")
			Expect(err).NotTo(HaveOccurred(), "Results logs verification failed for taskrun")
		})
	})

	Describe("PIPELINES-26-TC02: Test Tekton results with PipelineRun", Label("sanity"), Ordered, func() {
		It("verifies golang imagestream exists", func() {
			cmd.MustSucceed("oc", "get", "is", "golang", "-n", "openshift")
		})

		It("applies pipeline and pipelinerun fixtures", func() {
			ns := config.TargetNamespace
			oc.Apply("testdata/results/pipeline.yaml", ns)
			oc.Apply("testdata/results/pipelinerun.yaml", ns)

			// Wait for pipelinerun to complete
			cmd.MustSucceedIncreasedTimeout(time.Minute*5, "oc", "wait", "--for=condition=Succeeded", "pipelinerun/pipeline-results", "-n", ns, "--timeout=120s")
		})

		It("verifies pipelinerun results are stored", func() {
			err := operator.VerifyResultsAnnotationStored(sharedClients, "pipelinerun")
			Expect(err).NotTo(HaveOccurred(), "Results annotation not stored for pipelinerun")
		})

		It("verifies pipelinerun results records", func() {
			err := operator.VerifyResultsRecords("pipelinerun")
			Expect(err).NotTo(HaveOccurred(), "Results records verification failed for pipelinerun")
		})

		It("verifies pipelinerun results logs", func() {
			err := operator.VerifyResultsLogs("pipelinerun")
			Expect(err).NotTo(HaveOccurred(), "Results logs verification failed for pipelinerun")
		})
	})
})
