package pipelines_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

// Ensure Gomega is used.
var _ = Expect

var _ = Describe("PIPELINES-25: Bundles Resolver", Label("e2e"), func() {
	var ns string

	BeforeEach(func() {
		ns = uniqueNS("resolver-bun")
		oc.CreateNewProject(ns)
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})
	})

	It("PIPELINES-25-TC01: Test bundles resolver functionality", func() {
		oc.Create("testdata/resolvers/pipelineruns/bundles-resolver-pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "bundles-resolver-pipelinerun", "successful", ns)
	})

	It("PIPELINES-25-TC02: Test bundles resolver with parameter", Label("sanity"), func() {
		oc.Create("testdata/resolvers/pipelineruns/bundles-resolver-pipelinerun-param.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "bundles-resolver-pipelinerun-param", "successful", ns)
	})
})

var _ = Describe("PIPELINES-23: Cluster Resolvers", Ordered, Label("e2e"), func() {
	var ns string

	BeforeAll(func() {
		// Create separate projects for tasks and pipelines (cross-namespace resolution precondition)
		oc.CreateNewProject("releasetest-tasks")
		oc.Apply("testdata/resolvers/tasks/resolver-task.yaml", "releasetest-tasks")
		oc.Apply("testdata/resolvers/tasks/resolver-task2.yaml", "releasetest-tasks")
		oc.CreateNewProject("releasetest-pipelines")
		oc.Apply("testdata/resolvers/pipelines/resolver-pipeline.yaml", "releasetest-pipelines")
	})

	AfterAll(func() {
		oc.DeleteProjectIgnoreErrors("releasetest-tasks")
		oc.DeleteProjectIgnoreErrors("releasetest-pipelines")
	})

	It("PIPELINES-23-TC01: Cluster resolver cross-namespace resolution", Label("sanity"), func() {
		ns = uniqueNS("resolver-cluster")
		oc.CreateNewProject(ns)
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})

		oc.Create("testdata/resolvers/pipelineruns/resolver-pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "resolver-pipelinerun", "successful", ns)
	})

	It("PIPELINES-23-TC02: Cluster resolver same-namespace resolution", func() {
		ns = uniqueNS("resolver-cluster-sns")
		oc.CreateNewProject(ns)
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})

		oc.Create("testdata/resolvers/pipelines/resolver-pipeline-same-ns.yaml", ns)
		oc.Create("testdata/resolvers/pipelineruns/resolver-pipelinerun-same-ns.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "resolver-pipelinerun-same-ns", "successful", ns)
	})
})

var _ = Describe("PIPELINES-24: Git Resolvers", Label("e2e"), func() {
	var ns string

	BeforeEach(func() {
		ns = uniqueNS("resolver-git")
		oc.CreateNewProject(ns)
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})
	})

	It("PIPELINES-24-TC01: Test git resolver functionality", Label("sanity"), func() {
		oc.Create("testdata/resolvers/pipelineruns/git-resolver-pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "git-resolver-pipelinerun", "successful", ns)
	})

	It("PIPELINES-24-TC02: Test git resolver with authentication and token", func() {
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

var _ = Describe("PIPELINES-31: HTTP Resolvers", Label("e2e"), func() {
	It("PIPELINES-31-TC01: Test HTTP resolver functionality", Label("sanity"), func() {
		ns := uniqueNS("resolver-http")
		oc.CreateNewProject(ns)
		sharedClients.NewClientSet(ns)
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})

		oc.Create("testdata/resolvers/pipelineruns/http-resolver-pipelinerun.yaml", ns)
		pipelines.ValidatePipelineRun(sharedClients, "http-resolver-pipelinerun", "successful", ns)
	})
})

// Hub resolvers spec had no Polarion test case ID in the original Gauge spec.
// Assigned PIPELINES-32-TC01 for Ginkgo migration.
var _ = Describe("PIPELINES-32: Hub Resolvers", Label("e2e"), func() {
	It("PIPELINES-32-TC01: Test hub resolver functionality", Label("sanity"), func() {
		ns := uniqueNS("resolver-hub")
		oc.CreateNewProject(ns)
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
