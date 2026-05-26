package pipelines_test

import (
	. "github.com/onsi/ginkgo/v2"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// Hub resolvers spec had no Polarion test case ID in the original Gauge spec.
// Assigned PIPELINES-32-TC01 for Ginkgo migration.
var _ = Describe("PIPELINES-32-TC01: Test hub resolver functionality", Label("e2e", "sanity"), func() {
	var ns string

	BeforeEach(func() {
		ns = store.Namespace()
		sharedClients.NewClientSet(ns)
	})

	It("should resolve tasks from Tekton Hub", func() {
		oc.Apply("testdata/resolvers/pipelines/git-cli-hub.yaml", ns)
		oc.Apply("testdata/pvc/pvc.yaml", ns)
		oc.Apply("testdata/resolvers/pipelineruns/git-cli-hub.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "hub-git-cli-run", "successful", ns)
	})
})
