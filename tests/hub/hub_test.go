package hub_test

import (
	. "github.com/onsi/ginkgo/v2"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
)

var _ = Describe("Hub", Serial, Label("hub"), func() {

	Describe("PIPELINES-21-TC01: Install HUB without authentication", Label("sanity"), Ordered, func() {

		It("creates TektonHub resource", func() {
			oc.Apply("testdata/hub/tektonhub.yaml", "")
		})

		It("verifies that the hub API deployment is up and running", func() {
			k8s.ValidateDeployments(sharedClients, config.TargetNamespace, config.HubApiName)
		})

		It("verifies that the hub DB deployment is up and running", func() {
			k8s.ValidateDeployments(sharedClients, config.TargetNamespace, config.HubDbName)
		})

		It("verifies that the hub UI deployment is up and running", func() {
			k8s.ValidateDeployments(sharedClients, config.TargetNamespace, config.HubUiName)
		})
	})
})
