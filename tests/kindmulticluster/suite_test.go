package kindmulticluster_test

import (
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // Ginkgo DSL
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // Gomega DSL

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/kindmulticluster"
)

func TestKindMulticluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kind Multicluster Admission", Label("kind", "multicluster", "webhook"))
}

var _ = Describe("PipelineRun admission and spoke execution: PIPELINES-46", Serial, func() {
	DescribeTable("mutates admission fields and returns spoke execution status and logs",
		func(ctx SpecContext, prelabelled bool) {
			hub, spoke := os.Getenv("KIND_HUB_KUBECONFIG"), os.Getenv("KIND_SPOKE_KUBECONFIG")
			Expect(hub).NotTo(BeEmpty(), "run through bash scripts/kind-multicluster.sh")
			Expect(spoke).NotTo(BeEmpty(), "run through bash scripts/kind-multicluster.sh")
			Expect(kindmulticluster.Execute(ctx, hub, spoke, prelabelled)).To(Succeed())
		},
		Entry("defaults an absent queue label: PIPELINES-46-TC01", false, NodeTimeout(10*time.Minute)),
		Entry("preserves a distinct explicit LocalQueue: PIPELINES-46-TC02", true, NodeTimeout(10*time.Minute)),
	)
})
