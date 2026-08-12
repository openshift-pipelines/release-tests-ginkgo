package tektonkueue_test

import (
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	olmpkg "github.com/openshift-pipelines/release-tests-ginkgo/pkg/olm"
)

var _ = Describe("TektonKueue  Tests", Ordered, Label("tekton-kueue"), func() {
	var hubCluster olmpkg.ClusterBootstrap

	It("Creating a Hub Client", func() {
		hubKubeConfig := config.Flags.Kubeconfig
		hubClient, err := sharedClients.NewClientFromKubeconfig(hubKubeConfig)
		Expect(err).NotTo(HaveOccurred(), "Failed to create Client")

		hubCluster = olmpkg.ClusterBootstrap{
			Client: hubClient,
		}

	})

	Describe("Bootstrap Clusters", Label("install", "sanity"), Ordered, func() {
		It("Setup Hub Cluster", func() {
			err := hubCluster.Bootstrap(sharedClients.Ctx)
			Expect(err).NotTo(HaveOccurred(), "Failed to subscribe to operators")
		})
	})

})
