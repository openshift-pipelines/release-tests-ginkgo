package pac_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pac"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
	gitlab "github.com/xanzy/go-gitlab"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Pipelines As Code GitLab tests", func() {

	// =========================================================================
	// PIPELINES-30-TC01: Configure PAC with push and pull_request events
	// =========================================================================
	Describe("PIPELINES-30-TC01: Configure PAC with push and pull_request events", Ordered, Label("pac", "sanity", "e2e"), func() {
		var (
			gitlabClient *gitlab.Client
			project      *gitlab.Project
			projectID    int
			mrID         int
			smeeURL      string
			namespace    string
		)

		BeforeAll(func() {
			namespace = store.Namespace()
			lastNamespace = namespace

			gitlabClient = pac.InitGitLabClient(namespace)
			pac.SetGitLabClient(gitlabClient)

			var err error
			smeeURL, err = pac.SetupSmeeDeployment(sharedClients, namespace)
			Expect(err).NotTo(HaveOccurred(), "failed to setup Smee deployment")

			k8s.ValidateDeployments(sharedClients, namespace, "gosmee-client")

			project, err = pac.SetupGitLabProject(sharedClients, namespace, smeeURL)
			Expect(err).NotTo(HaveOccurred(), "failed to setup GitLab project")
			projectID = project.ID

			DeferCleanup(func() {
				err := pac.CleanupPAC(sharedClients, namespace, projectID, "gosmee-client")
				if err != nil {
					log.Printf("CleanupPAC warning: %v", err)
				}
			})
		})

		It("should generate pull_request PipelineRun YAML", func() {
			err := pac.GeneratePipelineRunYaml("pull_request", "main")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should generate push PipelineRun YAML", func() {
			err := pac.GeneratePipelineRunYaml("push", "main")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should configure preview changes with both files", func() {
			var err error
			mrID, err = pac.ConfigurePreviewChanges(projectID)
			Expect(err).NotTo(HaveOccurred())
			Expect(mrID).To(BeNumerically(">", 0))
		})

		It("should validate pull_request PipelineRun succeeds", func() {
			pipelineName, err := pac.GetPipelineNameFromMR(sharedClients, namespace, projectID, mrID)
			Expect(err).NotTo(HaveOccurred())
			pipelines.ValidatePipelineRun(sharedClients, pipelineName, "success", namespace)
		})

		It("should trigger push event on main branch", func() {
			err := pac.TriggerPushOnForkMain(projectID)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should validate push PipelineRun succeeds", func() {
			pipelineName, err := pac.GetPushPipelineNameFromMain(sharedClients, namespace)
			Expect(err).NotTo(HaveOccurred())
			pipelines.ValidatePipelineRun(sharedClients, pipelineName, "success", namespace)
		})
	})

	// =========================================================================
	// PIPELINES-30-TC02: Configure PAC with on-label annotation
	// =========================================================================
	Describe("PIPELINES-30-TC02: Configure PAC with on-label annotation", Ordered, Label("pac", "e2e"), func() {
		var (
			gitlabClient *gitlab.Client
			project      *gitlab.Project
			projectID    int
			mrID         int
			smeeURL      string
			namespace    string
		)

		BeforeAll(func() {
			namespace = store.Namespace()
			lastNamespace = namespace

			gitlabClient = pac.InitGitLabClient(namespace)
			pac.SetGitLabClient(gitlabClient)

			var err error
			smeeURL, err = pac.SetupSmeeDeployment(sharedClients, namespace)
			Expect(err).NotTo(HaveOccurred(), "failed to setup Smee deployment")

			k8s.ValidateDeployments(sharedClients, namespace, "gosmee-client")

			project, err = pac.SetupGitLabProject(sharedClients, namespace, smeeURL)
			Expect(err).NotTo(HaveOccurred(), "failed to setup GitLab project")
			projectID = project.ID

			DeferCleanup(func() {
				err := pac.CleanupPAC(sharedClients, namespace, projectID, "gosmee-client")
				if err != nil {
					log.Printf("CleanupPAC warning: %v", err)
				}
			})
		})

		It("should generate pull_request PipelineRun YAML", func() {
			err := pac.GeneratePipelineRunYaml("pull_request", "main")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should update on-label annotation with [bug]", func() {
			_, err := pac.UpdateAnnotation("pipelinesascode.tekton.dev/on-label", "[bug]")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should configure preview changes", func() {
			var err error
			mrID, err = pac.ConfigurePreviewChanges(projectID)
			Expect(err).NotTo(HaveOccurred())
			Expect(mrID).To(BeNumerically(">", 0))
		})

		It("should have 0 pipelineruns within 10 seconds", func() {
			// Use Consistently to assert no PipelineRuns appear within the timeout
			Consistently(func() int {
				prlist, err := sharedClients.PipelineRunClient.List(sharedClients.Ctx, metav1.ListOptions{})
				if err != nil {
					return -1
				}
				return len(prlist.Items)
			}).WithTimeout(10 * time.Second).WithPolling(2 * time.Second).Should(Equal(0))
		})

		It("should add label bug to the merge request", func() {
			err := pac.AddLabel(projectID, mrID, "bug", "red", "Identify a Issue")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should validate pull_request PipelineRun succeeds", func() {
			pipelineName, err := pac.GetPipelineNameFromMR(sharedClients, namespace, projectID, mrID)
			Expect(err).NotTo(HaveOccurred())
			pipelines.ValidatePipelineRun(sharedClients, pipelineName, "success", namespace)
		})
	})

	// =========================================================================
	// PIPELINES-30-TC03: Configure PAC with on-comment annotation
	// =========================================================================
	Describe("PIPELINES-30-TC03: Configure PAC with on-comment annotation", Ordered, Label("pac", "e2e"), func() {
		var (
			gitlabClient *gitlab.Client
			project      *gitlab.Project
			projectID    int
			mrID         int
			smeeURL      string
			namespace    string
		)

		BeforeAll(func() {
			namespace = store.Namespace()
			lastNamespace = namespace

			gitlabClient = pac.InitGitLabClient(namespace)
			pac.SetGitLabClient(gitlabClient)

			var err error
			smeeURL, err = pac.SetupSmeeDeployment(sharedClients, namespace)
			Expect(err).NotTo(HaveOccurred(), "failed to setup Smee deployment")

			k8s.ValidateDeployments(sharedClients, namespace, "gosmee-client")

			project, err = pac.SetupGitLabProject(sharedClients, namespace, smeeURL)
			Expect(err).NotTo(HaveOccurred(), "failed to setup GitLab project")
			projectID = project.ID

			DeferCleanup(func() {
				err := pac.CleanupPAC(sharedClients, namespace, projectID, "gosmee-client")
				if err != nil {
					log.Printf("CleanupPAC warning: %v", err)
				}
			})
		})

		It("should generate pull_request PipelineRun YAML", func() {
			err := pac.GeneratePipelineRunYaml("pull_request", "main")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should update on-comment annotation with ^/hello-world", func() {
			_, err := pac.UpdateAnnotation("pipelinesascode.tekton.dev/on-comment", "^/hello-world")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should configure preview changes", func() {
			var err error
			mrID, err = pac.ConfigurePreviewChanges(projectID)
			Expect(err).NotTo(HaveOccurred())
			Expect(mrID).To(BeNumerically(">", 0))
		})

		It("should validate first pull_request PipelineRun succeeds", func() {
			pipelineName, err := pac.GetPipelineNameFromMR(sharedClients, namespace, projectID, mrID)
			Expect(err).NotTo(HaveOccurred())
			pipelines.ValidatePipelineRun(sharedClients, pipelineName, "success", namespace)
		})

		It("should add comment /hello-world in MR", func() {
			err := pac.AddComment(projectID, mrID, "/hello-world")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should have 2 pipelineruns within 10 seconds", func() {
			// Use Eventually to poll for 2 PipelineRuns appearing
			Eventually(func() int {
				prlist, err := sharedClients.PipelineRunClient.List(sharedClients.Ctx, metav1.ListOptions{})
				if err != nil {
					return -1
				}
				return len(prlist.Items)
			}).WithTimeout(10 * time.Second).WithPolling(2 * time.Second).Should(Equal(2))
		})

		It("should validate second pull_request PipelineRun succeeds", func() {
			pipelineName, err := pac.GetPipelineNameFromMR(sharedClients, namespace, projectID, mrID)
			Expect(err).NotTo(HaveOccurred())
			pipelines.ValidatePipelineRun(sharedClients, pipelineName, "success", namespace)
		})
	})
})

var _ = Describe("Pipelines As Code TektonConfig tests", func() {

	// =========================================================================
	// PIPELINES-20-TC01: Enable/Disable PAC
	// =========================================================================
	Describe("PIPELINES-20-TC01: Enable/Disable PAC", Ordered, Serial, Label("pac", "sanity"), func() {

		BeforeAll(func() {
			lastNamespace = config.TargetNamespace
		})

		// setPACEnabled sets the pipelinesAsCode.enable field in the TektonConfig CR.
		setPACEnabled := func(enabled bool) {
			tc, err := sharedClients.TektonConfig().Get(context.Background(), "config", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get TektonConfig")

			// Parse the spec to update pipelinesAsCode.enable
			specBytes, err := json.Marshal(tc.Spec)
			Expect(err).NotTo(HaveOccurred())

			var specMap map[string]any
			err = json.Unmarshal(specBytes, &specMap)
			Expect(err).NotTo(HaveOccurred())

			// Navigate to or create the pipelinesAsCode section
			pacSection, ok := specMap["platforms"].(map[string]any)
			if !ok {
				pacSection = make(map[string]any)
				specMap["platforms"] = pacSection
			}
			openshiftSection, ok := pacSection["openshift"].(map[string]any)
			if !ok {
				openshiftSection = make(map[string]any)
				pacSection["openshift"] = openshiftSection
			}
			pacConfig, ok := openshiftSection["pipelinesAsCode"].(map[string]any)
			if !ok {
				pacConfig = make(map[string]any)
				openshiftSection["pipelinesAsCode"] = pacConfig
			}

			pacConfig["enable"] = enabled

			updatedSpec, err := json.Marshal(specMap)
			Expect(err).NotTo(HaveOccurred())

			err = json.Unmarshal(updatedSpec, &tc.Spec)
			Expect(err).NotTo(HaveOccurred())

			_, err = sharedClients.TektonConfig().Update(context.Background(), tc, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to update TektonConfig pipelinesAsCode.enable to %v", enabled))

			log.Printf("Set pipelinesAsCode.enable to %v", enabled)
		}

		It("should disable pipelinesAsCode", func() {
			setPACEnabled(false)
		})

		It("should verify PAC installersets are not present when disabled", func() {
			Eventually(func() bool {
				tis, err := sharedClients.Operator.TektonInstallerSets().List(context.Background(), metav1.ListOptions{})
				if err != nil {
					return false
				}
				for _, is := range tis.Items {
					if strings.HasPrefix(is.Name, "openshiftpipelinesascode") {
						return false // still present
					}
				}
				return true // none found
			}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(BeTrue(),
				"PAC installersets should not be present when disabled")
		})

		It("should verify PAC pods are not present when disabled", func() {
			pacPodLabels := []string{config.PacControllerName, config.PacWatcherName, config.PacWebhookName}
			Eventually(func() bool {
				pods, err := sharedClients.KubeClient.Kube.CoreV1().Pods(config.TargetNamespace).List(
					context.Background(), metav1.ListOptions{})
				if err != nil {
					return false
				}
				for _, pod := range pods.Items {
					for _, name := range pacPodLabels {
						if strings.Contains(pod.Name, name) {
							return false // PAC pod still present
						}
					}
				}
				return true
			}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(BeTrue(),
				"PAC pods should not be present when disabled")
		})

		It("should verify pipelines-as-code CR is removed when disabled", func() {
			Eventually(func() bool {
				_, err := sharedClients.PipelinesAsCode().Get(context.Background(), "pipelines-as-code", metav1.GetOptions{})
				return err != nil // should return NotFound error
			}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(BeTrue(),
				"pipelines-as-code CR should be removed when PAC is disabled")
		})

		It("should re-enable pipelinesAsCode", func() {
			setPACEnabled(true)

			DeferCleanup(func() {
				// Ensure PAC is re-enabled even if later verification steps fail
				setPACEnabled(true)
			})
		})

		It("should verify PAC installersets are present when enabled", func() {
			Eventually(func() bool {
				tis, err := sharedClients.Operator.TektonInstallerSets().List(context.Background(), metav1.ListOptions{})
				if err != nil {
					return false
				}
				for _, is := range tis.Items {
					if strings.HasPrefix(is.Name, "openshiftpipelinesascode") {
						return true // found
					}
				}
				return false
			}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(BeTrue(),
				"PAC installersets should be present when enabled")
		})

		It("should verify PAC pods are present when enabled", func() {
			pacPodNames := []string{config.PacControllerName, config.PacWatcherName, config.PacWebhookName}
			Eventually(func() int {
				foundCount := 0
				pods, err := sharedClients.KubeClient.Kube.CoreV1().Pods(config.TargetNamespace).List(
					context.Background(), metav1.ListOptions{})
				if err != nil {
					return 0
				}
				for _, pod := range pods.Items {
					for _, name := range pacPodNames {
						if strings.Contains(pod.Name, name) {
							foundCount++
							break
						}
					}
				}
				return foundCount
			}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(BeNumerically(">=", len(pacPodNames)),
				"PAC pods should be present when enabled")
		})

		It("should verify pipelines-as-code CR state after re-enable", func() {
			// After re-enable, the pipelines-as-code CR may or may not be recreated
			// depending on the operator version. Verify the operator reconciles properly.
			Eventually(func() error {
				_, err := sharedClients.PipelinesAsCode().Get(context.Background(), "pipelines-as-code", metav1.GetOptions{})
				return err
			}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Succeed(),
				"pipelines-as-code CR should exist after re-enable")
		})
	})

	// =========================================================================
	// PIPELINES-20-TC02: Application name change visible in GitHub UI (Pending)
	// =========================================================================
	PDescribe("PIPELINES-20-TC02: Application name change visible in GitHub UI", Label("pac", "sanity"), func() {
		// TODO: Requires GitHub App configuration not available in current test infrastructure
		// Steps from Gauge spec:
		// - Change application-name in tektonconfig
		// - Configure PAC using GitHub app
		// - Create repo, repo CRD, configure push event
		// - Verify pipelinerun created
		// - Verify pipelinerun status in GitHub
		// - Verify application name shown in GitHub UI
		It("should reflect application name change in GitHub UI", func() {})
	})

	// =========================================================================
	// PIPELINES-20-TC03: Auto-configure new GitHub repo (Pending)
	// =========================================================================
	PDescribe("PIPELINES-20-TC03: Auto-configure new GitHub repo", Label("pac", "sanity"), func() {
		// TODO: Requires GitHub App configuration not available in current test infrastructure
		// Steps from Gauge spec:
		// - Set auto-configure-new-github-repo to true
		// - Configure PAC using GitHub app
		// - Create new repo in GitHub
		// - Verify repo CR created
		// - Set auto-configure-new-github-repo to false
		// - Create another repo
		// - Verify repo CR is not created
		It("should auto-create repo CR when enabled", func() {})
		It("should not create repo CR when disabled", func() {})
	})

	// =========================================================================
	// PIPELINES-20-TC04: Error log snippet visibility (Pending)
	// =========================================================================
	PDescribe("PIPELINES-20-TC04: Error log snippet visibility", Label("pac", "sanity"), func() {
		// TODO: Requires GitHub App configuration not available in current test infrastructure
		// Steps from Gauge spec:
		// - Set error-log-snippet to false
		// - Configure PAC using GitHub app
		// - Create repo and failing pipelinerun
		// - Verify error log NOT shown in GitHub UI
		// - Set error-log-snippet to true
		// - Trigger push and verify error log IS shown
		It("should not show error log when disabled", func() {})
		It("should show error log when enabled", func() {})
	})
})
