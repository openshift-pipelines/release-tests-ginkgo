package pipelines_test

import (
	. "github.com/onsi/ginkgo/v2"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

var _ = Describe("PIPELINES-31-TC01: Test HTTP resolver functionality", Label("e2e", "sanity"), func() {
	var ns string

	BeforeEach(func() {
		ns = store.Namespace()
		sharedClients.NewClientSet(ns)
	})

	It("should resolve tasks from HTTP URL", func() {
		oc.Create("testdata/resolvers/pipelineruns/http-resolver-pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "http-resolver-pipelinerun", "successful", ns)
	})
})
