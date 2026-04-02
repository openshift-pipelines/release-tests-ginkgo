package ecosystem_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

// nsCounter provides unique namespace names per test within this file.
// Using GinkgoParallelProcess ensures uniqueness across parallel processes.
var ecoNsCounter int

// createTestNamespace creates a new OpenShift project with a unique name derived from
// the given prefix and returns the namespace name. The namespace name includes the
// Ginkgo parallel process index and an incrementing counter to avoid collisions.
func createTestNamespace(prefix string) string {
	ecoNsCounter++
	ns := fmt.Sprintf("%s-%d-%d", prefix, GinkgoParallelProcess(), ecoNsCounter)
	oc.CreateNewProject(ns)
	return ns
}

// -----------------------------------------------------------------------
// DescribeTable 1: Ecosystem Task Pipelines (simple create-verify pattern)
//
// These tests follow the most common ecosystem pattern:
//   1. Create a unique namespace and register cleanup
//   2. Create YAML resources (pipelines, PVCs, pipelineruns) via oc
//   3. Verify pipelinerun reaches expected status
//
// CRITICAL: Entry parameters are evaluated at TREE CONSTRUCTION TIME.
//   - Only string literals, slice literals, and constant values are used as Entry args.
//   - Dynamic values (namespace, client handles) are created INSIDE the table body
//     function at spec execution time, never passed through Entry.
//   - The `resources []string` parameter is a slice literal, safe for tree-construction.
// -----------------------------------------------------------------------
var _ = DescribeTable("Ecosystem Task Pipelines",
	func(testcaseID, pipelineRunName string, resources []string, expectedStatus string) {
		// Create isolated namespace for this test
		ns := createTestNamespace("eco-simple")
		DeferCleanup(oc.DeleteProjectIgnoreErrors, ns)

		// Initialize typed clients scoped to the new namespace
		sharedClients.NewClientSet(ns)

		// Create all resources in order
		for _, resource := range resources {
			oc.Create(resource, ns)
		}

		// Verify the pipelinerun reaches the expected status
		pipelines.ValidatePipelineRun(sharedClients, pipelineRunName, expectedStatus, ns)
	},

	// Labels applied to every Entry in this table
	Label("ecosystem", "e2e"),

	Entry("buildah pipelinerun: PIPELINES-29-TC01", Label("sanity", "buildah"),
		"PIPELINES-29-TC01", "buildah-run",
		[]string{
			"testdata/ecosystem/pipelines/buildah.yaml",
			"testdata/pvc/pvc.yaml",
			"testdata/ecosystem/pipelineruns/buildah.yaml",
		},
		"successful"),

	Entry("buildah-ns pipelinerun: PIPELINES-29-TC20", Label("sanity", "buildah-ns"),
		"PIPELINES-29-TC20", "buildah-ns-run",
		[]string{
			"testdata/ecosystem/pipelines/buildah-ns.yaml",
			"testdata/pvc/pvc.yaml",
			"testdata/ecosystem/pipelineruns/buildah-ns.yaml",
		},
		"successful"),

	Entry("git-cli pipelinerun: PIPELINES-29-TC03", Label("git-cli"),
		"PIPELINES-29-TC03", "git-cli-run",
		[]string{
			"testdata/ecosystem/pipelines/git-cli.yaml",
			"testdata/pvc/pvc.yaml",
			"testdata/ecosystem/pipelineruns/git-cli.yaml",
		},
		"successful"),

	Entry("openshift-client pipelinerun: PIPELINES-29-TC08", Label("openshift-client"),
		"PIPELINES-29-TC08", "openshift-client-run",
		[]string{
			"testdata/ecosystem/pipelineruns/openshift-client.yaml",
		},
		"successful"),

	Entry("skopeo-copy pipelinerun: PIPELINES-29-TC09", Label("skopeo-copy"),
		"PIPELINES-29-TC09", "skopeo-copy-run",
		[]string{
			"testdata/ecosystem/pipelineruns/skopeo-copy.yaml",
		},
		"successful"),

	Entry("tkn pipelinerun: PIPELINES-29-TC10", Label("tkn"),
		"PIPELINES-29-TC10", "tkn-run",
		[]string{
			"testdata/ecosystem/pipelineruns/tkn.yaml",
		},
		"successful"),

	Entry("maven pipelinerun: PIPELINES-29-TC13", Label("maven"),
		"PIPELINES-29-TC13", "maven-run",
		[]string{
			"testdata/ecosystem/pipelines/maven.yaml",
			"testdata/pvc/pvc.yaml",
			"testdata/ecosystem/configmaps/maven-settings.yaml",
			"testdata/ecosystem/pipelineruns/maven.yaml",
		},
		"successful"),

	Entry("step action resolvers: PIPELINES-29-TC14", Label("sanity"),
		"PIPELINES-29-TC14", "git-clone-stepaction-run",
		[]string{
			"testdata/ecosystem/tasks/git-clone-stepaction.yaml",
			"testdata/pvc/pvc.yaml",
			"testdata/ecosystem/pipelineruns/git-clone-stepaction.yaml",
		},
		"successful"),

	Entry("opc task pipelinerun: PIPELINES-29-TC21", Label("sanity", "opc"),
		"PIPELINES-29-TC21", "opc-task-run",
		[]string{
			"testdata/ecosystem/pipelines/opc-task.yaml",
			"testdata/ecosystem/pipelineruns/opc-task.yaml",
		},
		"successful"),
)

// -----------------------------------------------------------------------
// DescribeTable 2: Ecosystem Task Pipelines with Extra Verification
//
// These tests extend the simple create-verify pattern with a post-verification
// callback. After the pipelinerun succeeds, the postVerify function executes
// additional validation (log checks, deployment readiness, etc.).
//
// The postVerify parameter is a function literal defined inline in each Entry.
// Function literals are safe for tree-construction time since they are not
// variables initialized at runtime -- they are compile-time constants.
// -----------------------------------------------------------------------
var _ = DescribeTable("Ecosystem Task Pipelines with Extra Verification",
	func(testcaseID, pipelineRunName string, resources []string, expectedStatus string, postVerify func(ns string)) {
		ns := createTestNamespace("eco-extra")
		DeferCleanup(oc.DeleteProjectIgnoreErrors, ns)
		sharedClients.NewClientSet(ns)

		for _, resource := range resources {
			oc.Create(resource, ns)
		}

		pipelines.ValidatePipelineRun(sharedClients, pipelineRunName, expectedStatus, ns)

		// Run post-verification callback
		if postVerify != nil {
			postVerify(ns)
		}
	},

	Label("ecosystem", "e2e"),

	Entry("tkn pac pipelinerun: PIPELINES-29-TC11", Label("tkn"),
		"PIPELINES-29-TC11", "tkn-pac-run",
		[]string{
			"testdata/ecosystem/pipelineruns/tkn-pac.yaml",
		},
		"successful",
		func(ns string) { pipelines.CheckLogVersion(sharedClients, "tkn-pac", ns) }),

	Entry("tkn version pipelinerun: PIPELINES-29-TC12", Label("tkn"),
		"PIPELINES-29-TC12", "tkn-version-run",
		[]string{
			"testdata/ecosystem/pipelineruns/tkn-version.yaml",
		},
		"successful",
		func(ns string) { pipelines.CheckLogVersion(sharedClients, "tkn", ns) }),

	Entry("helm-upgrade-from-repo pipelinerun: PIPELINES-29-TC17", Label("helm"),
		"PIPELINES-29-TC17", "helm-upgrade-from-repo-run",
		[]string{
			"testdata/ecosystem/pipelines/helm-upgrade-from-repo.yaml",
			"testdata/pvc/pvc.yaml",
			"testdata/ecosystem/pipelineruns/helm-upgrade-from-repo.yaml",
		},
		"successful",
		func(ns string) { k8s.ValidateDeployments(sharedClients, ns, "test-hello-world") }),

	Entry("helm-upgrade-from-source pipelinerun: PIPELINES-29-TC18", Label("helm"),
		"PIPELINES-29-TC18", "helm-upgrade-from-source-run",
		[]string{
			"testdata/ecosystem/pipelines/helm-upgrade-from-source.yaml",
			"testdata/pvc/pvc.yaml",
			"testdata/ecosystem/pipelineruns/helm-upgrade-from-source.yaml",
		},
		"successful",
		func(ns string) { k8s.ValidateDeployments(sharedClients, ns, "test-hello-world") }),
)

// -----------------------------------------------------------------------
// Standalone It blocks for special patterns
//
// These tests have unique setup requirements that do not fit cleanly into a
// DescribeTable. Each test is a standalone It block within a Describe container.
// -----------------------------------------------------------------------

var _ = Describe("Ecosystem Special Tasks", Label("ecosystem", "e2e"), func() {

	// TC19: pull-request pipeline requires copying a secret from the
	// openshift-pipelines namespace before creating the pipelinerun.
	It("pull-request pipelinerun: PIPELINES-29-TC19", Label("pull-request"), func() {
		ns := createTestNamespace("eco-pull-request")
		DeferCleanup(oc.DeleteProjectIgnoreErrors, ns)
		sharedClients.NewClientSet(ns)

		// Copy github-auth-secret from openshift-pipelines namespace
		if !oc.SecretExists("github-auth-secret", "openshift-pipelines") {
			Skip("github-auth-secret not found in openshift-pipelines namespace")
		}
		oc.CopySecret("github-auth-secret", "openshift-pipelines", ns)

		oc.Create("testdata/ecosystem/pipelines/pull-request.yaml", ns)
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/ecosystem/pipelineruns/pull-request.yaml", ns)

		pipelines.ValidatePipelineRun(sharedClients, "pull-request-pipeline-run", "successful", ns)
	})

	// TC02: buildah disconnected requires IS_DISCONNECTED=true
	It("buildah disconnected pipelinerun: PIPELINES-29-TC02", Label("disconnected", "buildah"), func() {
		if !config.Flags.IsDisconnected {
			Skip("requires disconnected cluster (set IS_DISCONNECTED=true)")
		}

		ns := createTestNamespace("eco-buildah-disc")
		DeferCleanup(oc.DeleteProjectIgnoreErrors, ns)
		sharedClients.NewClientSet(ns)

		oc.Create("testdata/ecosystem/pipelines/buildah.yaml", ns)
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/ecosystem/pipelineruns/buildah-disconnected.yaml", ns)

		pipelines.ValidatePipelineRun(sharedClients, "buildah-disconnected-run", "successful", ns)
	})
})

// -----------------------------------------------------------------------
// DescribeTable 3: Multiarch Ecosystem Task Pipelines
//
// Architecture-specific tests. The requiredArchs parameter specifies which
// cluster architectures the test supports. If the current cluster architecture
// does not match any entry in requiredArchs, the test is skipped.
//
// The requiredArchs parameter is a []string literal, safe for tree-construction.
// Architecture checks happen inside the body function at spec execution time.
// -----------------------------------------------------------------------
var _ = DescribeTable("Multiarch Ecosystem Task Pipelines",
	func(testcaseID, pipelineRunName string, resources []string, expectedStatus string, requiredArchs []string) {
		// Skip if cluster architecture does not match
		if len(requiredArchs) > 0 {
			archMatch := false
			for _, arch := range requiredArchs {
				if config.Flags.ClusterArch == arch {
					archMatch = true
					break
				}
			}
			if !archMatch {
				Skip(fmt.Sprintf("requires one of architectures: %v (cluster is %s)", requiredArchs, config.Flags.ClusterArch))
			}
		}

		ns := createTestNamespace("eco-multiarch")
		DeferCleanup(oc.DeleteProjectIgnoreErrors, ns)
		sharedClients.NewClientSet(ns)

		for _, resource := range resources {
			oc.Create(resource, ns)
		}

		pipelines.ValidatePipelineRun(sharedClients, pipelineRunName, expectedStatus, ns)
	},

	Label("ecosystem", "e2e"),

	Entry("jib-maven pipelinerun: PIPELINES-32-TC01", Label("sanity", "jib-maven"),
		"PIPELINES-32-TC01", "jib-maven-run",
		[]string{
			"testdata/ecosystem/pipelines/jib-maven.yaml",
			"testdata/pvc/pvc.yaml",
			"testdata/ecosystem/pipelineruns/jib-maven.yaml",
		},
		"successful", []string{"amd64"}),

	Entry("jib-maven P&Z pipelinerun: PIPELINES-32-TC02", Label("sanity", "jib-maven"),
		"PIPELINES-32-TC02", "jib-maven-pz-run",
		[]string{
			"testdata/ecosystem/pipelines/jib-maven-pz.yaml",
			"testdata/pvc/pvc.yaml",
			"testdata/ecosystem/pipelineruns/jib-maven-pz.yaml",
		},
		"successful", []string{"ppc64le", "s390x", "arm64"}),

	Entry("kn-apply pipelinerun: PIPELINES-32-TC03", Label("kn-apply"),
		"PIPELINES-32-TC03", "kn-apply-run",
		[]string{
			"testdata/ecosystem/pipelineruns/kn-apply.yaml",
		},
		"successful", []string{"amd64"}),

	Entry("kn-apply p&z pipelinerun: PIPELINES-32-TC04", Label("kn-apply"),
		"PIPELINES-32-TC04", "kn-apply-pz-run",
		[]string{
			"testdata/ecosystem/pipelineruns/kn-apply-multiarch.yaml",
		},
		"successful", []string{"ppc64le", "s390x"}),

	Entry("kn pipelinerun: PIPELINES-32-TC05", Label("kn"),
		"PIPELINES-32-TC05", "kn-run",
		[]string{
			"testdata/ecosystem/pipelineruns/kn.yaml",
		},
		"successful", []string{"amd64"}),

	Entry("kn p&z pipelinerun: PIPELINES-32-TC06", Label("kn"),
		"PIPELINES-32-TC06", "kn-pz-run",
		[]string{
			"testdata/ecosystem/pipelineruns/kn-pz.yaml",
		},
		"successful", []string{"ppc64le", "s390x"}),
)

// Ensure gomega import is used (for Expect in standalone It blocks)
var _ = Expect
