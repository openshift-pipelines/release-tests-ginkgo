package operator

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

// VerifyVersionedTasks checks if the required tasks are available with the expected version.
func VerifyVersionedTasks() {
	taskList := cmd.MustSucceed("oc", "get", "task", "-n", "openshift-pipelines").Stdout()
	requiredTasks := []string{
		"buildah", "git-cli", "git-clone", "maven", "openshift-client",
		"s2i-dotnet", "s2i-go", "s2i-java", "s2i-nodejs", "s2i-perl",
		"s2i-php", "s2i-python", "s2i-ruby", "skopeo-copy", "tkn",
	}
	expectedVersion := os.Getenv("OSP_VERSION")

	// kn and kn-apply tasks are not available on arm64 clusters
	if config.Flags.ClusterArch != "arm64" {
		requiredTasks = append(requiredTasks, "kn", "kn-apply")
	}

	Expect(expectedVersion).NotTo(BeEmpty(),
		"OSP_VERSION is not set. Cannot determine the required version for tasks.")

	// Remove z-stream version from OSP_VERSION
	versionParts := strings.Split(expectedVersion, ".")
	Expect(len(versionParts)).To(BeNumerically(">=", 2),
		"Invalid OSP_VERSION Version: %s", expectedVersion)

	requiredVersion := versionParts[0] + "-" + versionParts[1] + "-0"

	for _, task := range requiredTasks {
		taskWithVersion := task + "-" + requiredVersion
		Expect(taskList).To(ContainSubstring(taskWithVersion),
			"Task %s not found in namespace openshift-pipelines", taskWithVersion)
	}
}

// VerifyVersionedStepActions checks if the required step actions are available with the expected version.
func VerifyVersionedStepActions() {
	stepActionList := cmd.MustSucceed("oc", "get", "stepaction", "-n", "openshift-pipelines").Stdout()
	requiredStepActions := []string{"git-clone", "cache-fetch", "cache-upload"}
	expectedVersion := os.Getenv("OSP_VERSION")

	Expect(expectedVersion).NotTo(BeEmpty(),
		"OSP_VERSION is not set. Cannot determine the required version for step actions.")

	// Remove z-stream version from OSP_VERSION
	versionParts := strings.Split(expectedVersion, ".")
	Expect(len(versionParts)).To(BeNumerically(">=", 2),
		"Invalid OSP_VERSION Version: %s", expectedVersion)

	requiredVersion := versionParts[0] + "-" + versionParts[1] + "-0"

	for _, stepAction := range requiredStepActions {
		stepActionWithVersion := stepAction + "-" + requiredVersion
		Expect(stepActionList).To(ContainSubstring(stepActionWithVersion),
			"Step action %s not found in namespace openshift-pipelines", stepActionWithVersion)
	}
}

// Ensure unused imports don't fire.
var _ = GinkgoWriter
