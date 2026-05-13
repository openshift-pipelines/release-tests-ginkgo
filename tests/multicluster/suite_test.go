package multicluster

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

var sharedClients *clients.Clients

// clientConfig holds the serialized configuration passed between parallel nodes.
type clientConfig struct {
	Kubeconfig      string `json:"kubeconfig"`
	Context         string `json:"context"`
	TargetNamespace string `json:"targetNamespace"`
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
			Context:         config.Flags.Context,
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
		sharedClients, err = clients.NewClients(cfg.Kubeconfig, cfg.Context, cfg.TargetNamespace)
		Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes clients")
	},
)

func TestMultiCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OLM Suite", Label("multi-cluster"))
}

var _ = AfterSuite(func() {
	_ = config.RemoveTempDir()
})
