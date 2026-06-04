package pipelines_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

var _ = Describe("Bundles Resolver", Label("e2e"), func() {
	var ns string

	BeforeEach(func() {
		ns = uniqueNS("resolver-bun")
		oc.CreateNewNamespace(ns)
		lastNamespace = ns
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})
	})

	It("Test bundles resolver functionality", func() {
		oc.Create("testdata/resolvers/pipelineruns/bundles-resolver-pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "bundles-resolver-pipelinerun", "successful", ns)
	})

	It("Test bundles resolver with parameter", Label("sanity"), func() {
		oc.Create("testdata/resolvers/pipelineruns/bundles-resolver-pipelinerun-param.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "bundles-resolver-pipelinerun-param", "successful", ns)
	})
})

var _ = Describe("Cluster Resolvers", Ordered, Label("e2e"), func() {
	var ns string

	BeforeAll(func() {
		// Create separate projects for tasks and pipelines (cross-namespace resolution precondition)
		oc.CreateNewNamespace("releasetest-tasks")
		oc.Apply("testdata/resolvers/tasks/resolver-task.yaml", "releasetest-tasks")
		oc.Apply("testdata/resolvers/tasks/resolver-task2.yaml", "releasetest-tasks")
		oc.CreateNewNamespace("releasetest-pipelines")
		oc.Apply("testdata/resolvers/pipelines/resolver-pipeline.yaml", "releasetest-pipelines")
	})

	AfterAll(func() {
		oc.DeleteProjectIgnoreErrors("releasetest-tasks")
		oc.DeleteProjectIgnoreErrors("releasetest-pipelines")
	})

	It("Cluster resolver cross-namespace resolution", Label("sanity"), func() {
		ns = uniqueNS("resolver-cluster")
		oc.CreateNewNamespace(ns)
		lastNamespace = ns
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})

		oc.Create("testdata/resolvers/pipelineruns/resolver-pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "resolver-pipelinerun", "successful", ns)
	})

	It("Cluster resolver same-namespace resolution", func() {
		ns = uniqueNS("resolver-cluster-sns")
		oc.CreateNewNamespace(ns)
		lastNamespace = ns
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})

		oc.Create("testdata/resolvers/pipelines/resolver-pipeline-same-ns.yaml", ns)
		oc.Create("testdata/resolvers/pipelineruns/resolver-pipelinerun-same-ns.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "resolver-pipelinerun-same-ns", "successful", ns)
	})
})

var _ = Describe("Git Resolvers", Label("e2e"), func() {
	var ns string

	BeforeEach(func() {
		ns = uniqueNS("resolver-git")
		oc.CreateNewNamespace(ns)
		lastNamespace = ns
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})
	})

	It("Test git resolver functionality", Label("sanity"), func() {
		oc.Create("testdata/resolvers/pipelineruns/git-resolver-pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "git-resolver-pipelinerun", "successful", ns)
	})

	It("Test git resolver with authentication and token", func() {
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			Skip("GITHUB_TOKEN not set -- skipping private repo resolver test")
		}

		// Create secret with GitHub token for private repo access
		cmd.MustSucceed("oc", "create", "secret", "generic", "private-repo-auth-secret",
			"--from-literal=token="+token, "-n", ns)
		DeferCleanup(func() {
			cmd.Run("oc", "delete", "secret", "private-repo-auth-secret", "-n", ns)
		})

		oc.Create("testdata/resolvers/pipelineruns/git-resolver-pipelinerun-private.yaml", ns)
		oc.Create("testdata/resolvers/pipelineruns/git-resolver-pipelinerun-private-token-auth.yaml", ns)
		oc.Create("testdata/resolvers/pipelineruns/git-resolver-pipelinerun-private-url.yaml", ns)

		pipelines.ValidatePipelineRun(sharedClients, "git-resolver-pipelinerun-private", "successful", ns)
		pipelines.ValidatePipelineRun(sharedClients, "git-resolver-pipelinerun-private-token-auth", "successful", ns)
		pipelines.ValidatePipelineRun(sharedClients, "git-resolver-pipelinerun-private-url", "successful", ns)
	})
})

var _ = Describe("HTTP Resolvers", Label("e2e"), func() {
	It("Test HTTP resolver functionality", Label("sanity"), func() {
		ns := uniqueNS("resolver-http")
		oc.CreateNewNamespace(ns)
		lastNamespace = ns
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})

		oc.Create("testdata/resolvers/pipelineruns/http-resolver-pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "http-resolver-pipelinerun", "successful", ns)
	})
})

// Hub resolvers spec had no Polarion test case ID in the original Gauge spec.
var _ = Describe("Hub Resolvers", Label("e2e"), func() {
	It("Test hub resolver functionality", Label("sanity"), func() {
		ns := uniqueNS("resolver-hub")
		oc.CreateNewNamespace(ns)
		lastNamespace = ns
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})

		oc.Apply("testdata/resolvers/pipelines/git-cli-hub.yaml", ns)
		oc.Apply("testdata/pvc/pvc.yaml", ns)
		oc.Apply("testdata/resolvers/pipelineruns/git-cli-hub.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "hub-git-cli-run", "successful", ns)
	})
})
