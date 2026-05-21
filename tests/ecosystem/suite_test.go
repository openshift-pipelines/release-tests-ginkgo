package ecosystem_test

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/hooks"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
)

var sharedClients *clients.Clients

// lastNamespace tracks the current test namespace for diagnostic collection.
// Set in BeforeEach by test specs; read in ReportAfterEach by diagnostics collector.
var lastNamespace string

func TestEcosystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ecosystem Suite", Label("ecosystem"))
}

// clientConfig holds the serialized configuration passed between parallel nodes.
type clientConfig struct {
	Kubeconfig      string `json:"kubeconfig"`
	Cluster         string `json:"cluster"`
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

		// Detect cluster architecture if not set via ARCH environment variable
		if config.Flags.ClusterArch == "" {
			detectedArch := k8s.GetClusterArchitecture(cs)
			if detectedArch != "" {
				config.Flags.ClusterArch = detectedArch
			}
		}

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

		// Detect cluster architecture on parallel nodes if not already set
		if config.Flags.ClusterArch == "" {
			detectedArch := k8s.GetClusterArchitecture(sharedClients)
			if detectedArch != "" {
				config.Flags.ClusterArch = detectedArch
			}
		}
	},
)

// Enable namespace isolation for parallel test execution in CI.
// Each Describe block gets its own namespace automatically.
var _ = hooks.AutoNamespacePerDescribe(&lastNamespace, func() *clients.Clients { return sharedClients })

var _ = AfterSuite(func() {
	hooks.CleanupNamespaces()
	_ = config.RemoveTempDir()
})
