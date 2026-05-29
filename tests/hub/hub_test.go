package hub_test

import (
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
	occmd "github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
)

var oc = occmd.OC{}
var _ = Describe("Hub", Serial, Label("hub"), func() {

	Describe("PIPELINES-21-TC01: Install HUB without authentication", Label("sanity"), Ordered, func() {

		It("creates TektonHub resource", func() {
			oc.Apply("testdata/hub/tektonhub.yaml", "")
		})

		It("verifies that the hub API deployment is up and running", func() {
			k8s.ValidateDeployments(sharedClients, config.TargetNamespace, config.HubAPIName)
		})

		It("verifies that the hub DB deployment is up and running", func() {
			k8s.ValidateDeployments(sharedClients, config.TargetNamespace, config.HubDBName)
		})

		It("verifies that the hub UI deployment is up and running", func() {
			k8s.ValidateDeployments(sharedClients, config.TargetNamespace, config.HubUIName)
		})
	})
})
