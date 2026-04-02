package pipelines_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

var _ = Describe("PIPELINES-02: Pipeline Failures", Label("e2e", "negative"), func() {
	var ns string

	BeforeEach(func() {
		ns = uniqueNS("fail-e2e")
		oc.CreateNewProject(ns)
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})
	})

	It("PIPELINES-02-TC01: Run Pipeline with non-existent ServiceAccount", Label("non-admin", "sanity"), func() {
		// Verify ServiceAccount "foobar" does not exist
		result := cmd.Run("oc", "get", "serviceaccount", "foobar", "-n", ns)
		Expect(result.ExitCode).NotTo(Equal(0), "ServiceAccount 'foobar' should not exist")

		oc.Create("testdata/negative/v1beta1/pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "output-pipeline-run-vb", "Failure", ns)
	})

	It("PIPELINES-02-TC02: Run Task with non-existent ServiceAccount", Label("non-admin"), func() {
		// Verify ServiceAccount "foobar" does not exist
		result := cmd.Run("oc", "get", "serviceaccount", "foobar", "-n", ns)
		Expect(result.ExitCode).NotTo(Equal(0), "ServiceAccount 'foobar' should not exist")

		oc.Create("testdata/negative/v1beta1/pull-request.yaml", ns)
		pipelines.ValidateTaskRun(sharedClients, "pullrequest-vb", "Failure", ns)
	})
})
