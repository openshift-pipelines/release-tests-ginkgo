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
	RunSpecs(t, "Kind One Hub One Spoke", Label("kind", "multicluster"))
}

var _ = Describe("Hub dispatch and live proxy logs", Serial, func() {
	It("executes only on the spoke, streams live logs through the hub, and returns success", NodeTimeout(15*time.Minute), func(ctx SpecContext) {
		hub, spoke := os.Getenv("KIND_HUB_KUBECONFIG"), os.Getenv("KIND_SPOKE_KUBECONFIG")
		Expect(hub).NotTo(BeEmpty(), "run through bash scripts/kind-multicluster.sh")
		Expect(spoke).NotTo(BeEmpty(), "run through bash scripts/kind-multicluster.sh")
		Expect(kindmulticluster.Execute(ctx, hub, spoke)).To(Succeed())
	})
})
