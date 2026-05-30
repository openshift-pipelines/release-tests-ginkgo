package mag_test

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	approvalgate "github.com/openshift-pipelines/release-tests-ginkgo/pkg/manualapprovalgate"
	occmd "github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
)

var oc = occmd.OC{}
var _ = Describe("Manual Approval Gate", Label("approvalgate", "e2e", "sanity"), func() {

	BeforeEach(func() {
		lastNamespace = config.TargetNamespace
	})

	Describe("Approve Manual Approval gate pipeline", Ordered, func() {
		It("validates MAG deployment is ready", func() {
			approvalgate.ValidateMAGDeployment(sharedClients)
		})

		It("creates the manual approval pipeline", func() {
			ns := config.TargetNamespace
			oc.Create("testdata/manualapprovalgate/manual-approval-pipeline.yaml", ns)
		})

		It("starts the pipeline with workspace", func() {
			cmd.MustSucceed("opc", "pipeline", "start", "manual-approval-pipeline",
				"-n", config.TargetNamespace)
		})

		It("approves the approval task", func() {
			tasks, err := approvalgate.ListApprovalTask(sharedClients)
			Expect(err).NotTo(HaveOccurred(), "Failed to list approval tasks")
			Expect(tasks).NotTo(BeEmpty(), "No approval tasks found")
			approvalgate.ApproveApprovalGatePipeline(tasks[0].Name)
		})

		It("validates pipeline is in Approved state", func() {
			result, err := approvalgate.ValidateApprovalGatePipeline(sharedClients, "Approved")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue(), "Pipeline not in Approved state")
		})

		It("verifies the latest pipelinerun succeeded", func() {
			reason := cmd.MustSucceedIncreasedTimeout(time.Minute*5,
				"opc", "pipelinerun", "describe", "--last", "-o", "jsonpath={.status.conditions[0].reason}",
				"-n", config.TargetNamespace).Stdout()
			reason = strings.TrimSpace(reason)
			Expect(strings.ToLower(reason)).To(Equal("succeeded"),
				"Expected pipelinerun to succeed but got: %s", reason)
		})
	})

	Describe("Reject Manual Approval gate pipeline", Ordered, func() {
		It("creates the manual approval pipeline", func() {
			ns := config.TargetNamespace
			oc.Create("testdata/manualapprovalgate/manual-approval-pipeline.yaml", ns)
		})

		It("starts the pipeline with workspace", func() {
			cmd.MustSucceed("opc", "pipeline", "start", "manual-approval-pipeline",
				"-n", config.TargetNamespace)
		})

		It("rejects the approval task", func() {
			tasks, err := approvalgate.ListApprovalTask(sharedClients)
			Expect(err).NotTo(HaveOccurred(), "Failed to list approval tasks")
			Expect(tasks).NotTo(BeEmpty(), "No approval tasks found")
			approvalgate.RejectApprovalGatePipeline(tasks[0].Name)
		})

		It("validates pipeline is in Rejected state", func() {
			result, err := approvalgate.ValidateApprovalGatePipeline(sharedClients, "Rejected")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue(), "Pipeline not in Rejected state")
		})

		It("verifies the latest pipelinerun failed", func() {
			reason := cmd.MustSucceedIncreasedTimeout(time.Minute*5,
				"opc", "pipelinerun", "describe", "--last", "-o", "jsonpath={.status.conditions[0].reason}",
				"-n", config.TargetNamespace).Stdout()
			reason = strings.TrimSpace(reason)
			// MAG rejection causes the pipelinerun to fail or be canceled
			Expect(strings.ToLower(reason)).To(SatisfyAny(
				Equal("failed"),
				Equal("pipelineruntimeout"),
				ContainSubstring("cancel"),
			), "Expected pipelinerun to fail but got: %s", reason)
		})
	})
})
