package ecosystem_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

var sharedClients *clients.Clients

func TestEcosystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ecosystem Suite", Label("ecosystem"))
}

var _ = BeforeSuite(func() {
	var err error
	sharedClients, err = clients.NewClients(
		config.Flags.Kubeconfig,
		config.Flags.Cluster,
		config.TargetNamespace,
	)
	Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes clients")
})

var _ = AfterSuite(func() {
	_ = config.RemoveTempDir()
})
