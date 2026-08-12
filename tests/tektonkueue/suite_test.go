package tektonkueue_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

var sharedClients *clients.Clients

type clientConfig struct {
	Kubeconfig      string `json:"kubeconfig"`
	Cluster         string `json:"cluster"`
	TargetNamespace string `json:"targetNamespace"`
}

func TestTektonKueue(t *testing.T) {
	// Step 1: Set the logger BEFORE calling client.New
	// Set Development: true inside zap.Options.
	//TektonKueue is using zap based logger so setting this at suite level
	log.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{
		Development: true,
	})))

	RegisterFailHandler(Fail)
	RunSpecs(t, "TektonKueue Suite", Label("tekton-kueue"))
}

var _ = SynchronizedBeforeSuite(
	// Node 1 only: validate cluster connectivity and serialize config
	func() []byte {
		// Verify cluster is reachable by creating clients
		cs, err := clients.NewClients(
			config.Flags.Kubeconfig,
			config.Flags.Cluster,
			config.TargetNamespace,
		)
		Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes clients on node 1")
		_ = cs // validation only on node 1

		cfg := clientConfig{
			Kubeconfig:      config.Flags.Kubeconfig,
			Cluster:         config.Flags.Cluster,
			TargetNamespace: config.TargetNamespace,
		}
		data, err := json.Marshal(cfg)
		Expect(err).NotTo(HaveOccurred(), "Failed to serialize client config")
		return data
	},
	// All nodes: deserialize config and create node-local clients
	func(data []byte) {
		var cfg clientConfig
		Expect(json.Unmarshal(data, &cfg)).To(Succeed(), "Failed to deserialize client config")

		var err error
		sharedClients, err = clients.NewClients(cfg.Kubeconfig, cfg.Cluster, cfg.TargetNamespace)
		Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes clients")
	},
)

var _ = AfterSuite(func() {
	_ = config.RemoveTempDir()
})
