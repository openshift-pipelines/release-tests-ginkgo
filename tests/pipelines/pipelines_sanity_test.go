package pipelines_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

// nsCounter provides unique namespace names per test within this file.
var nsCounter int

func uniqueNS(prefix string) string {
	nsCounter++
	return fmt.Sprintf("%s-%d-%d", prefix, GinkgoParallelProcess(), nsCounter)
}

var _ = Describe("PIPELINES-03: Pipeline Runs", Label("sanity", "e2e", "pipelines"), func() {
	var ns string

	BeforeEach(func() {
		ns = uniqueNS("sanity-pr")
		oc.CreateNewProject(ns)
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})
	})

	It("PIPELINES-03-TC04: Pipelinerun Timeout failure", func() {
		oc.Create("testdata/v1beta1/pipelinerun/pipelineruntimeout.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "pear", "timeout", ns)
	})

	It("PIPELINES-03-TC05: Configure execution results at Task level", Label("integration"), func() {
		oc.Create("testdata/v1beta1/pipelinerun/task_results_example.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "task-level-results", "successful", ns)
	})

	It("PIPELINES-03-TC06: Cancel pipelinerun", Label("integration"), func() {
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "output-pipeline-run-v1b1", "cancelled", ns)
	})

	It("PIPELINES-03-TC08: Pipelinerun with large result", Label("integration", "results"), func() {
		oc.Create("testdata/v1beta1/pipelinerun/pipelinerun-with-large-result.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "result-test-run", "successful", ns)
	})
})

var _ = Describe("PIPELINES-02: Pipeline Failures", Label("sanity", "e2e", "pipelines"), func() {
	var ns string

	BeforeEach(func() {
		ns = uniqueNS("sanity-fail")
		oc.CreateNewProject(ns)
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})
	})

	It("PIPELINES-02-TC01: Run Pipeline with non-existent ServiceAccount", Label("negative"), func() {
		// Verify SA does not exist
		result := cmd.Run("oc", "get", "serviceaccount", "foobar", "-n", ns)
		Expect(result.ExitCode).NotTo(Equal(0), "ServiceAccount 'foobar' should not exist")

		oc.Create("testdata/negative/v1beta1/pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "output-pipeline-run-vb", "Failure", ns)
	})
})

var _ = Describe("PIPELINES-25: Bundles Resolver", Label("sanity", "e2e"), func() {
	var ns string

	BeforeEach(func() {
		ns = uniqueNS("sanity-bun")
		oc.CreateNewProject(ns)
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})
	})

	It("PIPELINES-25-TC02: Bundles resolver with parameter", func() {
		oc.Create("testdata/resolvers/pipelineruns/bundles-resolver-pipelinerun-param.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "bundles-resolver-pipelinerun-param", "successful", ns)
	})
})

var _ = Describe("PIPELINES-24: Git Resolver", Label("sanity", "e2e"), func() {
	var ns string

	BeforeEach(func() {
		ns = uniqueNS("sanity-git")
		oc.CreateNewProject(ns)
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})
	})

	It("PIPELINES-24-TC01: Git resolver", func() {
		oc.Create("testdata/resolvers/pipelineruns/git-resolver-pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "git-resolver-pipelinerun", "successful", ns)
	})
})
