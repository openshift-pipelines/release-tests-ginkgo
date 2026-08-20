package tektonkueue_test

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

var (
	sharedClients *clients.Clients
	hubConfig     clientConfig
	spokeConfigs  []clientConfig
)

type clientConfig struct {
	Kubeconfig      string `json:"kubeconfig"`
	Cluster         string `json:"cluster,omitempty"`
	Context         string `json:"context,omitempty"`
	TargetNamespace string `json:"targetNamespace"`
}

type suiteConfig struct {
	Hub    clientConfig   `json:"hub"`
	Spokes []clientConfig `json:"spokes"`
}

func TestTektonKueue(t *testing.T) {
	config.MustLoadEnvironment()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tekton Kueue Suite", Label("tekton-kueue"))
}

var _ = SynchronizedBeforeSuite(
	func() []byte {
		Expect(config.Flags.SpokeKubeconfigs).NotTo(BeEmpty(), "at least one --spoke-kubeconfig is required")
		Expect(len(config.Flags.SpokeContexts) == 0 || len(config.Flags.SpokeContexts) == len(config.Flags.SpokeKubeconfigs)).To(BeTrue(),
			"provide either no --spoke-context flags or one per --spoke-kubeconfig")

		hub := clientConfig{
			Kubeconfig:      config.Flags.Kubeconfig,
			Cluster:         config.Flags.Cluster,
			Context:         config.Flags.Context,
			TargetNamespace: config.TargetNamespace,
		}
		var err error
		sharedClients, err = clients.NewClientsWithContext(hub.Kubeconfig, hub.Cluster, hub.Context, hub.TargetNamespace)
		Expect(err).NotTo(HaveOccurred(), "failed to create hub clients")

		spokes := make([]clientConfig, len(config.Flags.SpokeKubeconfigs))
		for i, kubeconfig := range config.Flags.SpokeKubeconfigs {
			contextName := ""
			if len(config.Flags.SpokeContexts) != 0 {
				contextName = config.Flags.SpokeContexts[i]
			}
			spokes[i] = clientConfig{Kubeconfig: kubeconfig, Context: contextName, TargetNamespace: config.TargetNamespace}
			_, err := clients.BuildClientConfigWithContext(kubeconfig, "", contextName)
			Expect(err).NotTo(HaveOccurred(), "failed to load spoke-%d kubeconfig", i+1)
		}

		data, err := json.Marshal(suiteConfig{Hub: hub, Spokes: spokes})
		Expect(err).NotTo(HaveOccurred(), "failed to serialize cluster configuration")
		return data
	},
	func(data []byte) {
		var cfg suiteConfig
		Expect(json.Unmarshal(data, &cfg)).To(Succeed(), "failed to deserialize cluster configuration")

		var err error
		sharedClients, err = clients.NewClientsWithContext(cfg.Hub.Kubeconfig, cfg.Hub.Cluster, cfg.Hub.Context, cfg.Hub.TargetNamespace)
		Expect(err).NotTo(HaveOccurred(), "failed to create hub clients")
		hubConfig = cfg.Hub
		spokeConfigs = cfg.Spokes
	},
)

var _ = AfterSuite(func() {
	_ = config.RemoveTempDir()
})
