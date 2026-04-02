package ecosystem_test

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/openshift"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/opc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/triggers"
)

// -----------------------------------------------------------------------
// S2I Ecosystem Tests (9 tests)
//
// Source-to-Image (S2I) tests verify that S2I builder images can compile
// and deploy applications. Most S2I tests follow the "imagestream-start"
// pattern: retrieve imagestream tags at runtime, then start and verify
// a pipeline run for each version tag.
//
// ECO-02 CRITICAL: Imagestream tags are retrieved INSIDE the table body
// function at spec execution time. Entry parameters contain only string
// and []string literals (pipeline name, imagestream name, resource paths,
// skip architectures). This demonstrates correct data parameter passing --
// runtime-evaluated values are NOT passed through Entry.
// -----------------------------------------------------------------------

// -----------------------------------------------------------------------
// DescribeTable: S2I imagestream-start pattern (8 tests: TC02-TC09)
//
// For each imagestream tag (excluding "latest"):
//   1. Set VERSION param to the tag value
//   2. Start pipeline via opc with --prefix-name for unique pipelinerun names
//   3. Validate each pipelinerun succeeds
//
// The dotnet imagestream (TC02) has special EXAMPLE_REVISION param logic:
//   - version >= 5.0 -> EXAMPLE_REVISION = "dotnet-<version>"
//   - version < 5.0  -> EXAMPLE_REVISION = "dotnetcore-<version>"
// -----------------------------------------------------------------------
var _ = DescribeTable("S2I Ecosystem Task Pipelines",
	func(testcaseID, pipelineName, imagestreamName string, resources []string, skipArchs []string) {
		// Skip if cluster architecture matches skip list
		if len(skipArchs) > 0 {
			for _, arch := range skipArchs {
				if config.Flags.ClusterArch == arch {
					Skip(fmt.Sprintf("test skipped on architecture: %s", arch))
				}
			}
		}

		ns := createTestNamespace("eco-s2i")
		lastNamespace = ns
		DeferCleanup(oc.DeleteProjectIgnoreErrors, ns)
		sharedClients.NewClientSet(ns)

		// Create pipeline and PVC resources
		for _, resource := range resources {
			oc.Create(resource, ns)
		}

		// Retrieve imagestream tags at RUNTIME (not tree construction time).
		// This is the critical ECO-02 pattern: dynamic data is fetched inside
		// the body function, not passed via Entry parameters.
		tags := openshift.GetImageStreamTags(sharedClients, "openshift", imagestreamName)
		Expect(tags).NotTo(BeEmpty(), "no tags found for imagestream %s", imagestreamName)

		workspaces := map[string]string{"name=source": "claimName=shared-pvc"}

		re := regexp.MustCompile(`\d+\.\d+`)

		for _, tag := range tags {
			if tag == "latest" {
				continue
			}

			params := map[string]string{"VERSION": tag}

			// Dotnet-specific EXAMPLE_REVISION logic
			if imagestreamName == "dotnet" {
				versionStr := re.FindString(tag)
				if versionStr != "" {
					versionFloat, _ := strconv.ParseFloat(versionStr, 64)
					if versionFloat >= 5.0 {
						params["EXAMPLE_REVISION"] = "dotnet-" + versionStr
					} else {
						params["EXAMPLE_REVISION"] = "dotnetcore-" + versionStr
					}
				}
			}

			customRunName := pipelineName + "-run-" + tag
			prName := opc.StartPipeline(pipelineName, params, workspaces, ns,
				"--use-param-defaults", "--prefix-name", customRunName)
			pipelines.ValidatePipelineRun(sharedClients, prName, "successful", ns)
		}
	},

	Label("ecosystem", "e2e"),

	Entry("S2I dotnet pipelinerun: PIPELINES-33-TC02", Label("s2i"),
		"PIPELINES-33-TC02", "s2i-dotnet-pipeline", "dotnet",
		[]string{
			"testdata/ecosystem/pipelines/s2i-dotnet.yaml",
			"testdata/pvc/pvc.yaml",
		},
		[]string{"ppc64le"}), // skip on ppc64le

	Entry("S2I golang pipelinerun: PIPELINES-33-TC03", Label("sanity", "s2i"),
		"PIPELINES-33-TC03", "s2i-go-pipeline", "golang",
		[]string{
			"testdata/ecosystem/pipelines/s2i-go.yaml",
			"testdata/pvc/pvc.yaml",
		},
		[]string{}),

	Entry("S2I java pipelinerun: PIPELINES-33-TC04", Label("s2i"),
		"PIPELINES-33-TC04", "s2i-java-pipeline", "java",
		[]string{
			"testdata/ecosystem/pipelines/s2i-java.yaml",
			"testdata/pvc/pvc.yaml",
		},
		[]string{}),

	Entry("S2I nodejs pipelinerun: PIPELINES-33-TC05", Label("s2i"),
		"PIPELINES-33-TC05", "s2i-nodejs-pipeline", "nodejs",
		[]string{
			"testdata/ecosystem/pipelines/s2i-nodejs.yaml",
			"testdata/pvc/pvc.yaml",
		},
		[]string{}),

	Entry("S2I perl pipelinerun: PIPELINES-33-TC06", Label("s2i"),
		"PIPELINES-33-TC06", "s2i-perl-pipeline", "perl",
		[]string{
			"testdata/ecosystem/pipelines/s2i-perl.yaml",
			"testdata/pvc/pvc.yaml",
		},
		[]string{}),

	Entry("S2I php pipelinerun: PIPELINES-33-TC07", Label("s2i"),
		"PIPELINES-33-TC07", "s2i-php-pipeline", "php",
		[]string{
			"testdata/ecosystem/pipelines/s2i-php.yaml",
			"testdata/pvc/pvc.yaml",
		},
		[]string{}),

	Entry("S2I python pipelinerun: PIPELINES-33-TC08", Label("s2i"),
		"PIPELINES-33-TC08", "s2i-python-pipeline", "python",
		[]string{
			"testdata/ecosystem/pipelines/s2i-python.yaml",
			"testdata/pvc/pvc.yaml",
		},
		[]string{}),

	Entry("S2I ruby pipelinerun: PIPELINES-33-TC09", Label("s2i"),
		"PIPELINES-33-TC09", "s2i-ruby-pipeline", "ruby",
		[]string{
			"testdata/ecosystem/pipelines/s2i-ruby.yaml",
			"testdata/pvc/pvc.yaml",
		},
		[]string{}),
)

// -----------------------------------------------------------------------
// S2I nodejs full flow (TC01) -- standalone It block
//
// This test is unique: after verifying the pipelinerun, it exposes a
// deployment config, retrieves the route URL, and validates the HTTP
// response contains expected content. It cannot use the DescribeTable
// pattern because of the post-verification steps.
// -----------------------------------------------------------------------

var _ = Describe("S2I Full Flow Tests", Label("ecosystem", "e2e", "s2i"), func() {

	It("S2I nodejs pipelinerun with route validation: PIPELINES-33-TC01", Label("sanity"), func() {
		ns := createTestNamespace("eco-s2i-nodejs")
		lastNamespace = ns
		DeferCleanup(oc.DeleteProjectIgnoreErrors, ns)
		sharedClients.NewClientSet(ns)

		// Create all resources
		oc.Create("testdata/ecosystem/pipelines/nodejs-ex-git.yaml", ns)
		oc.Create("testdata/pvc/pvc.yaml", ns)
		oc.Create("testdata/ecosystem/deploymentconfigs/nodejs-ex-git.yaml", ns)
		oc.Create("testdata/ecosystem/imagestreams/nodejs-ex-git.yaml", ns)
		oc.Create("testdata/ecosystem/pipelineruns/nodejs-ex-git.yaml", ns)

		// Verify pipelinerun
		pipelines.ValidatePipelineRun(sharedClients, "nodejs-ex-git-pr", "successful", ns)

		// Expose deployment config on port 3000
		triggers.ExposeDeploymentConfig("nodejs-ex-git", "3000", ns)

		// Get route URL
		routeURL := triggers.GetRouteURL("nodejs-ex-git", ns)

		// Validate route response contains expected content
		output := cmd.MustSucceedIncreasedTimeout(180*time.Second, "curl", "-kL", routeURL).Stdout()
		Expect(output).To(ContainSubstring("See Also"))
	})
})
