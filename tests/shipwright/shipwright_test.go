package shipwright_test

import (
	. "github.com/onsi/ginkgo/v2"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/shipwright"
)

func buildWithStrategy(buildFile string) {
	shipwright.SkipIfNotInstalled(sharedClients)
	shipwright.RunBuild(sharedClients, buildFile, lastNamespace)
}

var _ = Describe("Shipwright Build with buildah strategy: PIPELINES-44-TC01", Label("e2e", "shipwright", "sanity", "buildah"), func() {
	It("should build the golang sample and push the image", func() {
		buildWithStrategy("testdata/shipwright/builds/buildah.yaml")
	})
})

var _ = Describe("Shipwright Build with source-to-image strategy (nodejs): PIPELINES-44-TC02", Label("e2e", "shipwright", "sanity", "s2i"), func() {
	It("should build the nodejs sample and push the image", func() {
		buildWithStrategy("testdata/shipwright/builds/s2i-nodejs.yaml")
	})
})

var _ = Describe("Shipwright Build with source-to-image strategy (java): PIPELINES-44-TC03", Label("e2e", "shipwright", "s2i"), func() {
	It("should build the java sample and push the image", func() {
		buildWithStrategy("testdata/shipwright/builds/s2i-java.yaml")
	})
})

var _ = Describe("Shipwright Build with buildpacks strategy: PIPELINES-44-TC04", Label("e2e", "shipwright", "buildpacks"), func() {
	It("should build the quarkus sample and push the image", func() {
		buildWithStrategy("testdata/shipwright/builds/buildpacks.yaml")
	})
})

var _ = Describe("Shipwright Build with buildpacks-extender strategy: PIPELINES-44-TC05", Label("e2e", "shipwright", "buildpacks", "buildpacks-extender"), func() {
	It("should build the nodejs sample and push the image", func() {
		buildWithStrategy("testdata/shipwright/builds/buildpacks-extender.yaml")
	})
})
