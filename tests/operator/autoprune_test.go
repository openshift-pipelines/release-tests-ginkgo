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
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// removePrunerConfig patches TektonConfig to remove the pruner configuration.
// It wraps the removal in a recovery since the pruner section may not exist.
func removePrunerConfig() {
	oc.RemovePrunerConfig()
	// Wait for TektonConfig to stabilize after removing pruner
	operator.EnsureTektonConfigStatusInstalled(sharedClients.TektonConfig(), store.GetCRNames())
}

// updatePrunerConfig patches TektonConfig with pruner configuration.
// It builds the JSON patch dynamically based on which strategy fields are provided.
func updatePrunerConfig(keep, schedule, resources, keepSince string, withKeep, withKeepSince bool) {
	var prunerFields []string
	prunerFields = append(prunerFields, fmt.Sprintf(`"schedule":"%s"`, schedule))

	// Build resources array
	resList := strings.Split(resources, ",")
	resJSON := make([]string, len(resList))
	for i, r := range resList {
		resJSON[i] = fmt.Sprintf(`"%s"`, strings.TrimSpace(r))
	}
	prunerFields = append(prunerFields, fmt.Sprintf(`"resources":[%s]`, strings.Join(resJSON, ",")))

	if withKeep {
		prunerFields = append(prunerFields, fmt.Sprintf(`"keep":%s`, keep))
	}
	if withKeepSince {
		prunerFields = append(prunerFields, fmt.Sprintf(`"keep-since":%s`, keepSince))
	}

	patch := fmt.Sprintf(`{"spec":{"pruner":{%s}}}`, strings.Join(prunerFields, ","))
	log.Printf("Patching TektonConfig pruner: %s\n", patch)
	cmd.MustSucceed("oc", "patch", "TektonConfig", "config", "--type=merge", "-p", patch)
	operator.EnsureTektonConfigStatusInstalled(sharedClients.TektonConfig(), store.GetCRNames())
}

// updatePrunerConfigWithInvalidData attempts to patch TektonConfig with invalid pruner config
// and returns the error output.
func updatePrunerConfigWithInvalidData(keep, schedule, resources, keepSince string, withKeep, withKeepSince bool) string {
	var prunerFields []string
	prunerFields = append(prunerFields, fmt.Sprintf(`"schedule":"%s"`, schedule))

	resList := strings.Split(resources, ",")
	resJSON := make([]string, len(resList))
	for i, r := range resList {
		resJSON[i] = fmt.Sprintf(`"%s"`, strings.TrimSpace(r))
	}
	prunerFields = append(prunerFields, fmt.Sprintf(`"resources":[%s]`, strings.Join(resJSON, ",")))

	if withKeep {
		prunerFields = append(prunerFields, fmt.Sprintf(`"keep":%s`, keep))
	}
	if withKeepSince {
		prunerFields = append(prunerFields, fmt.Sprintf(`"keep-since":%s`, keepSince))
	}

	patch := fmt.Sprintf(`{"spec":{"pruner":{%s}}}`, strings.Join(prunerFields, ","))
	log.Printf("Patching TektonConfig pruner with invalid data: %s\n", patch)
	result := cmd.Run("oc", "patch", "TektonConfig", "config", "--type=merge", "-p", patch)
	return result.Stderr()
}

// assertCronjobPresence uses Eventually to poll for the presence or absence of
// a cronjob with the given prefix in the target namespace.
func assertCronjobPresence(prefix, targetNamespace string, shouldBePresent bool) {
	if shouldBePresent {
		Eventually(func(g Gomega) {
			output := cmd.MustSucceed("oc", "get", "cronjob", "-n", targetNamespace, "-o", "name").Stdout()
			g.Expect(output).To(ContainSubstring(prefix),
				"expected cronjob with prefix %s to be present in namespace %s", prefix, targetNamespace)
		}).WithTimeout(2 * time.Minute).WithPolling(config.APIRetry).Should(Succeed())
	} else {
		Eventually(func(g Gomega) {
			output := cmd.Run("oc", "get", "cronjob", "-n", targetNamespace, "-o", "name").Stdout()
			g.Expect(output).NotTo(ContainSubstring(prefix),
				"expected cronjob with prefix %s to NOT be present in namespace %s", prefix, targetNamespace)
		}).WithTimeout(2 * time.Minute).WithPolling(config.APIRetry).Should(Succeed())
	}
}

// assertResourceCount uses Eventually to poll for the expected count of a resource type
// in the target namespace.
func assertResourceCount(resourceType string, expectedCount, timeoutSeconds int) {
	ns := store.Namespace()
	Eventually(func(g Gomega) {
		output := cmd.Run("oc", "get", resourceType, "-n", ns, "-o", "name").Stdout()
		lines := strings.Split(strings.TrimSpace(output), "\n")
		count := 0
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				count++
			}
		}
		g.Expect(count).To(Equal(expectedCount),
			"expected %d %s(s) in namespace %s, got %d", expectedCount, resourceType, ns, count)
	}).WithTimeout(time.Duration(timeoutSeconds) * time.Second).WithPolling(config.APIRetry).Should(Succeed())
}

// storeCronjobName gets the name of the cronjob with the given schedule in the namespace.
func storeCronjobName(namespace, schedule string) string {
	output := cmd.MustSucceed("oc", "get", "cronjob", "-n", namespace,
		"-o", "jsonpath={range .items[?(@.spec.schedule==\""+schedule+"\")]}{.metadata.name}{end}").Stdout()
	Expect(strings.TrimSpace(output)).NotTo(BeEmpty(),
		"expected to find cronjob with schedule %s in namespace %s", schedule, namespace)
	return strings.TrimSpace(output)
}

// createPrunerResources creates the 4 standard pruner test resources.
func createPrunerResources() {
	ns := store.Namespace()
	oc.Create("testdata/pruner/pipeline/pipeline-for-pruner.yaml", ns)
	oc.Create("testdata/pruner/pipeline/pipelinerun-for-pruner.yaml", ns)
	oc.Create("testdata/pruner/task/task-for-pruner.yaml", ns)
	oc.Create("testdata/pruner/task/taskrun-for-pruner.yaml", ns)
}

// createAdditionalPrunerResources creates additional pipelinerun and taskrun resources.
func createAdditionalPrunerResources() {
	ns := store.Namespace()
	oc.Create("testdata/pruner/pipeline/pipelinerun-for-pruner.yaml", ns)
	oc.Create("testdata/pruner/task/taskrun-for-pruner.yaml", ns)
}

var _ = Describe("PIPELINES-12: Verify auto-prune E2E", Serial,
	Label("e2e", "operator", "auto-prune", "admin"), func() {

		BeforeEach(func() {
			lastNamespace = store.Namespace()
			operator.ValidateOperatorInstallStatus(sharedClients, store.GetCRNames())
		})

		// TC01: Verify auto prune for taskrun
		Context("PIPELINES-12-TC01: Verify auto prune for taskrun", Ordered, Label("sanity"), func() {
			It("should prune taskruns to keep 2", func() {
				removePrunerConfig()
				createPrunerResources()

				updatePrunerConfig("2", "*/1 * * * *", "taskrun", "", true, false)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				assertResourceCount("taskrun", 2, 180)
				assertResourceCount("pipelinerun", 5, 120)

				DeferCleanup(func() {
					removePrunerConfig()
					assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)
				})
			})
		})

		// TC02: Verify auto prune for pipelinerun
		Context("PIPELINES-12-TC02: Verify auto prune for pipelinerun", Ordered, func() {
			It("should prune pipelineruns to keep 2", func() {
				removePrunerConfig()
				createPrunerResources()

				updatePrunerConfig("2", "*/1 * * * *", "pipelinerun", "", true, false)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				assertResourceCount("pipelinerun", 2, 120)
				assertResourceCount("taskrun", 7, 180)

				DeferCleanup(func() {
					removePrunerConfig()
					assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)
				})
			})
		})

		// TC03: Verify auto prune for pipelinerun and taskrun
		Context("PIPELINES-12-TC03: Verify auto prune for pipelinerun and taskrun", Ordered, func() {
			It("should prune both pipelineruns and taskruns to keep 2", func() {
				removePrunerConfig()
				createPrunerResources()

				updatePrunerConfig("2", "*/1 * * * *", "pipelinerun,taskrun", "", true, false)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				assertResourceCount("pipelinerun", 2, 120)
				assertResourceCount("taskrun", 2, 180)

				DeferCleanup(func() {
					removePrunerConfig()
					assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)
				})
			})
		})

		// TC04: Verify auto prune with keep-since
		Context("PIPELINES-12-TC04: Verify auto prune with keep-since", Ordered, Label("sanity"), func() {
			It("should prune with keep-since strategy", func() {
				removePrunerConfig()
				createPrunerResources()

				// INTENTIONAL SLEEP: Creates a time gap between "old" and "new" resources
				// so that keep-since pruning can distinguish them. This is NOT a polling
				// wait -- it is required to establish resource age difference.
				time.Sleep(120 * time.Second)

				createAdditionalPrunerResources()

				updatePrunerConfig("", "*/1 * * * *", "pipelinerun,taskrun", "2", false, true)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				assertResourceCount("pipelinerun", 5, 120)
				assertResourceCount("taskrun", 10, 180)

				DeferCleanup(func() {
					removePrunerConfig()
					assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)
				})
			})
		})

		// TC05: Verify auto prune skip namespace with annotation
		Context("PIPELINES-12-TC05: Verify auto prune skip namespace with annotation", Ordered, func() {
			It("should not prune resources in namespace with prune.skip annotation", func() {
				removePrunerConfig()
				createPrunerResources()

				oc.AnnotateNamespace(store.Namespace(), "operator.tekton.dev/prune.skip=true")

				updatePrunerConfig("2", "*/1 * * * *", "pipelinerun,taskrun", "", true, false)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				// With skip annotation, resources should NOT be pruned
				assertResourceCount("pipelinerun", 5, 120)
				assertResourceCount("taskrun", 10, 180)

				// Remove skip annotation -- pruning should resume
				cmd.MustSucceed("oc", "annotate", "namespace", store.Namespace(), "operator.tekton.dev/prune.skip-")
				assertResourceCount("pipelinerun", 2, 120)
				assertResourceCount("taskrun", 2, 180)

				DeferCleanup(func() {
					removePrunerConfig()
					assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)
				})
			})
		})

		// TC06: Verify auto prune add resources taskrun per namespace with annotation
		Context("PIPELINES-12-TC06: Verify auto prune add resources taskrun per namespace", Ordered, func() {
			It("should only prune taskruns when namespace annotation specifies taskrun", func() {
				removePrunerConfig()
				createPrunerResources()

				oc.AnnotateNamespace(store.Namespace(), "operator.tekton.dev/prune.resources=taskrun")

				updatePrunerConfig("2", "*/1 * * * *", "pipelinerun,taskrun", "", true, false)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				// With resources=taskrun annotation, only taskruns should be pruned
				assertResourceCount("pipelinerun", 5, 120)
				assertResourceCount("taskrun", 2, 180)

				// Remove annotation -- pipelineruns should now also be pruned
				cmd.MustSucceed("oc", "annotate", "namespace", store.Namespace(), "operator.tekton.dev/prune.resources-")
				assertResourceCount("pipelinerun", 2, 120)

				DeferCleanup(func() {
					removePrunerConfig()
					assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)
				})
			})
		})

		// TC07: Verify auto prune add resources taskrun and pipelinerun per namespace
		Context("PIPELINES-12-TC07: Verify auto prune add resources taskrun and pipelinerun per namespace", Ordered, Label("sanity"), func() {
			It("should prune both resources with per-namespace annotation override", func() {
				removePrunerConfig()
				createPrunerResources()

				oc.AnnotateNamespace(store.Namespace(), "operator.tekton.dev/prune.resources=pipelinerun,taskrun")

				// Global config only prunes taskrun, but namespace annotation overrides to both
				updatePrunerConfig("2", "*/1 * * * *", "taskrun", "", true, false)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				assertResourceCount("pipelinerun", 2, 120)
				assertResourceCount("taskrun", 2, 180)

				// Remove annotation and create more resources -- only taskrun should be pruned now
				cmd.MustSucceed("oc", "annotate", "namespace", store.Namespace(), "operator.tekton.dev/prune.resources-")
				createAdditionalPrunerResources()

				assertResourceCount("pipelinerun", 7, 120)
				assertResourceCount("taskrun", 2, 180)

				DeferCleanup(func() {
					removePrunerConfig()
					assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)
				})
			})
		})

		// TC08: Verify auto prune add keep per namespace with global strategy keep
		Context("PIPELINES-12-TC08: Verify auto prune add keep per namespace with global keep", Ordered, func() {
			It("should use per-namespace keep annotation override", func() {
				removePrunerConfig()
				createPrunerResources()

				oc.AnnotateNamespace(store.Namespace(), "operator.tekton.dev/prune.keep=3")

				updatePrunerConfig("2", "*/1 * * * *", "pipelinerun,taskrun", "", true, false)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				// Namespace annotation says keep=3
				assertResourceCount("pipelinerun", 3, 120)
				assertResourceCount("taskrun", 3, 180)

				// Remove annotation -- global keep=2 should apply
				cmd.MustSucceed("oc", "annotate", "namespace", store.Namespace(), "operator.tekton.dev/prune.keep-")
				assertResourceCount("pipelinerun", 2, 120)

				DeferCleanup(func() {
					removePrunerConfig()
					assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)
				})
			})
		})

		// TC09: Verify auto prune with keep-since per namespace with global keep-since
		Context("PIPELINES-12-TC09: Verify auto prune with keep-since per namespace", Ordered, func() {
			It("should use per-namespace keep-since annotation override", func() {
				removePrunerConfig()
				createPrunerResources()

				// INTENTIONAL SLEEP: Creates a time gap between "old" and "new" resources
				// for keep-since strategy differentiation. Not a polling scenario.
				time.Sleep(120 * time.Second)

				createAdditionalPrunerResources()

				oc.AnnotateNamespace(store.Namespace(), "operator.tekton.dev/prune.keep-since=2")

				// Global keep-since=10 (keeps more), namespace annotation keep-since=2 (keeps fewer)
				updatePrunerConfig("", "*/1 * * * *", "pipelinerun,taskrun", "10", false, true)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				assertResourceCount("pipelinerun", 5, 120)
				assertResourceCount("taskrun", 10, 180)

				// Remove annotation
				cmd.MustSucceed("oc", "annotate", "namespace", store.Namespace(), "operator.tekton.dev/prune.keep-since-")

				DeferCleanup(func() {
					removePrunerConfig()
					assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)
				})
			})
		})

		// TC10: Verify auto prune with keep per namespace with global keep-since
		Context("PIPELINES-12-TC10: Verify auto prune with keep per namespace with global keep-since", Ordered, func() {
			It("should use per-namespace keep with strategy annotation when global uses keep-since", func() {
				removePrunerConfig()
				createPrunerResources()

				oc.AnnotateNamespace(store.Namespace(), "operator.tekton.dev/prune.keep=2")

				// Global uses keep-since=10. Namespace annotation uses keep=2 but
				// without strategy annotation, the global strategy is used.
				updatePrunerConfig("", "*/1 * * * *", "pipelinerun,taskrun", "10", false, true)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				assertResourceCount("pipelinerun", 5, 120)
				assertResourceCount("taskrun", 10, 180)

				// Add strategy annotation -- now keep=2 should be applied
				oc.AnnotateNamespace(store.Namespace(), "operator.tekton.dev/prune.strategy=keep")
				assertResourceCount("pipelinerun", 2, 120)
				assertResourceCount("taskrun", 2, 180)

				DeferCleanup(func() {
					cmd.Run("oc", "annotate", "namespace", store.Namespace(), "operator.tekton.dev/prune.keep-")
					cmd.Run("oc", "annotate", "namespace", store.Namespace(), "operator.tekton.dev/prune.strategy-")
					removePrunerConfig()
					assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)
				})
			})
		})

		// TC11: Verify auto prune schedule per namespace
		Context("PIPELINES-12-TC11: Verify auto prune schedule per namespace", Ordered, func() {
			It("should use per-namespace schedule annotation override", func() {
				removePrunerConfig()
				createPrunerResources()

				// Namespace gets faster schedule via annotation
				oc.AnnotateNamespace(store.Namespace(), "operator.tekton.dev/prune.schedule=*/1 * * * *")

				// Global schedule is slow (*/8), but namespace annotation overrides to */1
				updatePrunerConfig("2", "*/8 * * * *", "pipelinerun,taskrun", "", true, false)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				assertResourceCount("pipelinerun", 2, 120)
				assertResourceCount("taskrun", 2, 180)

				// Remove schedule annotation -- global slow schedule applies
				cmd.MustSucceed("oc", "annotate", "namespace", store.Namespace(), "operator.tekton.dev/prune.schedule-")

				// Wait for schedule change to take effect, then create more resources
				// The global schedule is */8 so pruning won't happen quickly
				Eventually(func(g Gomega) {
					// Verify the annotation was removed
					result := cmd.MustSucceed("oc", "get", "namespace", store.Namespace(),
						"-o", "jsonpath={.metadata.annotations.operator\\.tekton\\.dev/prune\\.schedule}")
					g.Expect(strings.TrimSpace(result.Stdout())).To(BeEmpty())
				}).WithTimeout(30 * time.Second).WithPolling(5 * time.Second).Should(Succeed())

				createAdditionalPrunerResources()

				// With slow global schedule, resources should accumulate
				assertResourceCount("pipelinerun", 7, 120)
				assertResourceCount("taskrun", 12, 180)

				DeferCleanup(func() {
					removePrunerConfig()
					assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)
				})
			})
		})

		// TC12: Verify auto prune validation
		Context("PIPELINES-12-TC12: Verify auto prune validation", Ordered, func() {
			It("should reject invalid pruner configurations", func() {
				removePrunerConfig()

				// Test 1: Both keep and keep-since specified
				stderr1 := updatePrunerConfigWithInvalidData("2", "*/8 * * * *", "pipelinerun,taskrun", "2", true, true)
				Expect(stderr1).To(ContainSubstring("expected exactly one, got both: spec.pruner.keep, spec.pruner.keep-since"),
					"expected validation error for both keep and keep-since")

				// Test 2: Invalid resource type "taskrunas"
				stderr2 := updatePrunerConfigWithInvalidData("2", "*/8 * * * *", "pipelinerun,taskrunas", "", true, false)
				Expect(stderr2).To(ContainSubstring("invalid value: taskrunas"),
					"expected validation error for invalid resource taskrunas")

				// Test 3: Invalid resource type "pipelinerunas"
				stderr3 := updatePrunerConfigWithInvalidData("2", "*/8 * * * *", "pipelinerunas,taskrun", "", true, false)
				Expect(stderr3).To(ContainSubstring("invalid value: pipelinerunas"),
					"expected validation error for invalid resource pipelinerunas")
			})
		})

		// TC13: Verify auto prune cronjob re-creation for addition of random annotation/label
		Context("PIPELINES-12-TC13: Verify auto prune cronjob stability", Ordered, func() {
			It("should not re-create cronjob for random annotation/label changes", func() {
				removePrunerConfig()

				updatePrunerConfig("2", "10 * * * *", "pipelinerun,taskrun", "", true, false)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				preAnnotationName := storeCronjobName(store.Namespace(), "10 * * * *")

				// Add random annotation
				oc.AnnotateNamespace(store.Namespace(), "random-annotation=true")
				// Brief wait for annotation propagation; not a polling scenario
				time.Sleep(5 * time.Second)

				postAnnotationName := storeCronjobName(store.Namespace(), "10 * * * *")
				Expect(postAnnotationName).To(Equal(preAnnotationName),
					"cronjob should not be re-created after adding random annotation")

				// Remove random annotation
				cmd.MustSucceed("oc", "annotate", "namespace", store.Namespace(), "random-annotation-")
				postAnnotationRemovalName := storeCronjobName(store.Namespace(), "10 * * * *")
				Expect(postAnnotationRemovalName).To(Equal(preAnnotationName),
					"cronjob should not be re-created after removing random annotation")

				// Add random label
				oc.LabelNamespace(store.Namespace(), "random=true")
				postLabelName := storeCronjobName(store.Namespace(), "10 * * * *")
				Expect(postLabelName).To(Equal(preAnnotationName),
					"cronjob should not be re-created after adding random label")

				// Remove random label
				cmd.MustSucceed("oc", "label", "namespace", store.Namespace(), "random-")
				postLabelRemovalName := storeCronjobName(store.Namespace(), "10 * * * *")
				Expect(postLabelRemovalName).To(Equal(preAnnotationName),
					"cronjob should not be re-created after removing random label")

				DeferCleanup(func() {
					removePrunerConfig()
				})
			})
		})

		// TC14: Verify auto prune cronjob contains single container
		Context("PIPELINES-12-TC14: Verify auto prune cronjob single container", Ordered, Label("sanity"), func() {
			It("should have a single container in pruner cronjob", func() {
				updatePrunerConfig("2", "20 * * * *", "taskrun", "", true, false)

				oc.CreateNewProject("test-project-1")
				oc.CreateNewProject("test-project-2")

				DeferCleanup(func() {
					oc.DeleteProjectIgnoreErrors("test-project-1")
					oc.DeleteProjectIgnoreErrors("test-project-2")
					removePrunerConfig()
				})

				// Wait for cronjob to appear, then check container count
				Eventually(func(g Gomega) {
					output := cmd.MustSucceed("oc", "get", "cronjob", "-n", store.Namespace(),
						"-o", "jsonpath={range .items[*]}{.spec.jobTemplate.spec.template.spec.containers[*].name}{' '}{end}").Stdout()
					containers := strings.Fields(strings.TrimSpace(output))
					g.Expect(containers).To(HaveLen(1),
						"expected pruner cronjob to have 1 container, got %d", len(containers))
				}).WithTimeout(2 * time.Minute).WithPolling(config.APIRetry).Should(Succeed())
			})
		})

		// TC15: Verify operator stability after deleting namespace with pruner annotation
		Context("PIPELINES-12-TC15: Verify operator stability after namespace deletion", Ordered, Label("sanity"), func() {
			It("should remain stable after deleting namespace with pruner annotation", func() {
				removePrunerConfig()
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), false)

				// Create namespace-one (has pruner annotations)
				ns := store.Namespace()
				oc.Create("testdata/pruner/namespaces/namespace-one.yaml", ns)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				// Delete namespace-one
				oc.DeleteProjectIgnoreErrors("namespace-one")
				// Wait for cronjob to disappear after namespace deletion
				Eventually(func(g Gomega) {
					output := cmd.Run("oc", "get", "cronjob", "-n", store.Namespace(), "-o", "name").Stdout()
					g.Expect(output).NotTo(ContainSubstring(config.PrunerNamePrefix),
						"expected cronjob to be removed after namespace deletion")
				}).WithTimeout(2 * time.Minute).WithPolling(config.APIRetry).Should(Succeed())

				// Verify operator is still installed
				operator.ValidateOperatorInstallStatus(sharedClients, store.GetCRNames())

				// Create namespace-two (also has pruner annotations)
				oc.Create("testdata/pruner/namespaces/namespace-two.yaml", ns)
				assertCronjobPresence(config.PrunerNamePrefix, store.Namespace(), true)

				// Delete namespace-two
				oc.DeleteProjectIgnoreErrors("namespace-two")
				Eventually(func(g Gomega) {
					output := cmd.Run("oc", "get", "cronjob", "-n", store.Namespace(), "-o", "name").Stdout()
					g.Expect(output).NotTo(ContainSubstring(config.PrunerNamePrefix),
						"expected cronjob to be removed after namespace deletion")
				}).WithTimeout(2 * time.Minute).WithPolling(config.APIRetry).Should(Succeed())
			})
		})
	})
