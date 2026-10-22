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
	RunSpecs(t, "Kind Multicluster", Label("kind", "multicluster"))
}

var _ = Describe("Hub dispatch and live proxy logs", Serial, Label("kind-one-spoke"), func() {
	It("executes only on the spoke, streams live logs through the hub, and returns success", NodeTimeout(15*time.Minute), func(ctx SpecContext) {
		hub, spoke := os.Getenv("KIND_HUB_KUBECONFIG"), os.Getenv("KIND_SPOKE_KUBECONFIG")
		Expect(hub).NotTo(BeEmpty(), "run through bash scripts/kind-multicluster.sh")
		Expect(spoke).NotTo(BeEmpty(), "run through bash scripts/kind-multicluster.sh")
		Expect(kindmulticluster.Execute(ctx, hub, spoke)).To(Succeed())
	})
})

var _ = Describe("Two-spoke quota-ten batch", Serial, Label("kind-two-spoke"), func() {
	It("executes ten runs with deterministic coverage of both spokes", NodeTimeout(35*time.Minute), func(ctx SpecContext) {
		hub := os.Getenv("KIND_HUB_KUBECONFIG")
		spokes := []string{os.Getenv("KIND_SPOKE_KUBECONFIG"), os.Getenv("KIND_SECOND_SPOKE_KUBECONFIG")}
		Expect(hub).NotTo(BeEmpty(), "run bash scripts/kind-multicluster.sh --spokes=2")
		Expect(spokes[0]).NotTo(BeEmpty())
		Expect(spokes[1]).NotTo(BeEmpty())
		Expect(kindmulticluster.ExecuteScale(ctx, hub, spokes)).To(Succeed())
	})
})
