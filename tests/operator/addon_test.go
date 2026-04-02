package operator_test

import (
	"fmt"
	"log"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// updateAddonConfig patches the TektonConfig CR's addon section with the given
// resolverTasks and pipelineTemplates values. After patching, it waits for the
// TektonConfig status to return to installed.
func updateAddonConfig(resolverTasks, pipelineTemplates string) {
	patch := fmt.Sprintf(
		`{"spec":{"addon":{"params":[{"name":"resolverTasks","value":"%s"},{"name":"pipelineTemplates","value":"%s"}]}}}`,
		resolverTasks, pipelineTemplates,
	)
	log.Printf("Patching TektonConfig addon: resolverTasks=%s, pipelineTemplates=%s\n", resolverTasks, pipelineTemplates)
	cmd.MustSucceed("oc", "patch", "TektonConfig", "config", "--type=merge", "-p", patch)
	operator.EnsureTektonConfigStatusInstalled(sharedClients.TektonConfig(), store.GetCRNames())
}

// updateAddonConfigExpectError patches the TektonConfig CR's addon section and
// expects the patch to fail. It returns the stderr output for assertion.
func updateAddonConfigExpectError(resolverTasks, pipelineTemplates string) string {
	patch := fmt.Sprintf(
		`{"spec":{"addon":{"params":[{"name":"resolverTasks","value":"%s"},{"name":"pipelineTemplates","value":"%s"}]}}}`,
		resolverTasks, pipelineTemplates,
	)
	log.Printf("Patching TektonConfig addon (expecting error): resolverTasks=%s, pipelineTemplates=%s\n", resolverTasks, pipelineTemplates)
	result := cmd.Run("oc", "patch", "TektonConfig", "config", "--type=merge", "-p", patch)
	return result.Stderr()
}

// assertTaskPresence uses Eventually to poll for the presence or absence of a
// task with the given name in the specified namespace.
func assertTaskPresence(taskName, namespace string, shouldBePresent bool) {
	if shouldBePresent {
		Eventually(func(g Gomega) {
			result := cmd.Run("oc", "get", "task", taskName, "-n", namespace)
			g.Expect(result.ExitCode).To(Equal(0),
				"expected task %s to be present in namespace %s", taskName, namespace)
		}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Succeed())
	} else {
		Eventually(func(g Gomega) {
			result := cmd.Run("oc", "get", "task", taskName, "-n", namespace)
			g.Expect(result.ExitCode).NotTo(Equal(0),
				"expected task %s to NOT be present in namespace %s", taskName, namespace)
		}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Succeed())
	}
}

// assertPipelinesPresence uses Eventually to poll for the presence or absence of
// pipelines in the specified namespace.
func assertPipelinesPresence(namespace string, shouldBePresent bool) {
	if shouldBePresent {
		Eventually(func(g Gomega) {
			result := cmd.Run("oc", "get", "pipeline", "-n", namespace, "-o", "name")
			g.Expect(result.ExitCode).To(Equal(0),
				"expected pipelines to be present in namespace %s", namespace)
			g.Expect(strings.TrimSpace(result.Stdout())).NotTo(BeEmpty(),
				"expected pipelines to exist in namespace %s", namespace)
		}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Succeed())
	} else {
		Eventually(func(g Gomega) {
			result := cmd.Run("oc", "get", "pipeline", "-n", namespace, "-o", "name")
			output := strings.TrimSpace(result.Stdout())
			// Either the command fails or returns empty output
			g.Expect(output).To(BeEmpty(),
				"expected no pipelines in namespace %s, got: %s", namespace, output)
		}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Succeed())
	}
}

var _ = Describe("PIPELINES-15: Verify Addon E2E", Serial, Ordered,
	Label("e2e", "operator", "addon", "admin"), func() {

		BeforeAll(func() {
			lastNamespace = "openshift-pipelines"
			operator.ValidateOperatorInstallStatus(sharedClients, store.GetCRNames())

			// Restore resolverTasks and pipelineTemplates to defaults on cleanup
			DeferCleanup(func() {
				log.Println("Restoring addon config to defaults: resolverTasks=true, pipelineTemplates=true")
				cmd.MustSucceed("oc", "patch", "TektonConfig", "config", "--type=merge", "-p",
					`{"spec":{"addon":{"params":[{"name":"resolverTasks","value":"true"},{"name":"pipelineTemplates","value":"true"}]}}}`)
			})
		})

		It("PIPELINES-15-TC06: Disable/Enable resolverTasks", Label("sanity"), func() {
			updateAddonConfig("false", "false")
			assertTaskPresence("s2i-java", "openshift-pipelines", false)

			updateAddonConfig("true", "false")
			assertTaskPresence("s2i-java", "openshift-pipelines", true)
		})

		It("PIPELINES-15-TC07: Disable/Enable resolverTasks with additional Tasks", func() {
			updateAddonConfig("true", "false")
			assertTaskPresence("s2i-java", "openshift-pipelines", true)

			// Apply a custom "hello" task
			cmd.MustSucceed("oc", "apply", "-f", config.Path("testdata/ecosystem/tasks/hello.yaml"), "-n", "openshift-pipelines")
			DeferCleanup(func() {
				cmd.Run("oc", "delete", "task", "hello", "-n", "openshift-pipelines")
			})
			assertTaskPresence("hello", "openshift-pipelines", true)

			// Disable resolverTasks -- s2i-java should be gone, hello should remain
			updateAddonConfig("false", "false")
			assertTaskPresence("s2i-java", "openshift-pipelines", false)
			assertTaskPresence("hello", "openshift-pipelines", true)

			// Re-enable resolverTasks -- both should be present
			updateAddonConfig("true", "false")
			assertTaskPresence("s2i-java", "openshift-pipelines", true)
			assertTaskPresence("hello", "openshift-pipelines", true)
		})

		It("PIPELINES-15-TC08: Disable/Enable pipeline templates", Label("sanity"), func() {
			updateAddonConfig("true", "true")
			assertPipelinesPresence("openshift", true)

			updateAddonConfig("true", "false")
			assertPipelinesPresence("openshift", false)

			updateAddonConfig("true", "true")
			assertPipelinesPresence("openshift", true)
		})

		It("PIPELINES-15-TC05: Enable pipeline templates when clustertask is disabled", Label("negative"), func() {
			stderr := updateAddonConfigExpectError("false", "true")
			Expect(stderr).To(ContainSubstring("pipelineTemplates cannot be true if resolverTask is false"),
				"expected validation error when enabling pipelineTemplates with resolverTasks disabled")
		})

		It("PIPELINES-15-TC09: Verify versioned ecosystem tasks", func() {
			operator.VerifyVersionedTasks()
		})

		It("PIPELINES-15-TC010: Verify versioned stepaction tasks", func() {
			operator.VerifyVersionedStepActions()
		})
	})

// Ensure unused import warning doesn't fire.
var _ = time.Second
