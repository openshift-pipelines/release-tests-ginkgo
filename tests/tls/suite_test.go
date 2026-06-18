package tls_test

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/diagnostics"
)

var sharedClients *clients.Clients

// lastNamespace tracks the current test namespace for diagnostic collection.
var lastNamespace string

func TestTLS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TLS Suite", Label("tls"))
}

// clientConfig holds the serialized configuration passed between parallel nodes.
type clientConfig struct {
	Kubeconfig      string `json:"kubeconfig"`
	Cluster         string `json:"cluster"`
	TargetNamespace string `json:"targetNamespace"`
}

var _ = SynchronizedBeforeSuite(
	func() []byte {
		cs, err := clients.NewClients(
			config.Flags.Kubeconfig,
			config.Flags.Cluster,
			config.TargetNamespace,
		)
		Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes clients on node 1")
		_ = cs

		cfg := clientConfig{
			Kubeconfig:      config.Flags.Kubeconfig,
			Cluster:         config.Flags.Cluster,
			TargetNamespace: config.TargetNamespace,
		}
		data, err := json.Marshal(cfg)
		Expect(err).NotTo(HaveOccurred(), "Failed to serialize client config")
		return data
	},
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

var _ = ReportAfterEach(diagnostics.CollectOnFailure(&lastNamespace))
