package operator_test

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// NOTE: The original Gauge spec for this test is tagged "to-do", indicating it
// may be incomplete or not fully validated. The test is implemented as described
// in the spec regardless.

var _ = Describe("PIPELINES-13: Verify HPA", Serial,
	Label("operator", "admin", "hpa"), func() {

		BeforeEach(func() {
			lastNamespace = "openshift-pipelines"
			operator.ValidateOperatorInstallStatus(sharedClients, store.GetCRNames())
		})

		It("PIPELINES-13-TC01: Test HPA for tekton-pipelines-webhook deployment", func() {
			// Scale the tekton-pipelines-webhook deployment to 3 replicas
			cmd.MustSucceed("oc", "-n", "openshift-pipelines", "scale", "--replicas=3",
				"deployment/tekton-pipelines-webhook")

			// Register cleanup to restore original replica count
			DeferCleanup(func() {
				cmd.MustSucceed("oc", "-n", "openshift-pipelines", "scale", "--replicas=1",
					"deployment/tekton-pipelines-webhook")
			})

			// Use Eventually (NOT Sleep) to poll until 3 pods are running
			Eventually(func(g Gomega) {
				output := cmd.MustSucceed("oc", "get", "pods", "-n", "openshift-pipelines",
					"-l", "app=tekton-pipelines-webhook", "--field-selector=status.phase=Running",
					"-o", "name").Stdout()
				pods := strings.Split(strings.TrimSpace(output), "\n")
				// Filter out empty strings from split
				var runningPods []string
				for _, p := range pods {
					if strings.TrimSpace(p) != "" {
						runningPods = append(runningPods, p)
					}
				}
				g.Expect(runningPods).To(HaveLen(3), "expected 3 running webhook pods")
			}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
		})
	})
