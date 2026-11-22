package operator_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega
	"github.com/tektoncd/pipeline/pkg/names"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// Covers issue #76: e2e coverage for the NamespaceSyncController
// (spec.platforms.openshift.namespaceSync), including the RFE-7814 "Quay
// Robot credential" secret-binding use case.
//
// This suite manages its own namespaces (Label("no-auto-namespace")) instead
// of relying on the per-Describe auto-namespace, because most specs here
// need to create/delete several namespaces and mutate the cluster-singleton
// TektonConfig CR itself.
var _ = Describe("NamespaceSyncController", Serial, Ordered, ContinueOnFailure,
	Label("e2e", "admin", "namespacesync", "no-auto-namespace"), func() {

		BeforeAll(func() {
			lastNamespace = store.GetCRNames().TektonConfig
			operator.ValidateOperatorInstallStatus(sharedClients, store.GetCRNames())

			DeferCleanup(func() {
				operator.RestoreNamespaceSyncDefaults(sharedClients)
			})
		})

		Context("Baseline sync on namespace creation", func() {
			It("creates the pipeline SA, CA bundles, edit RoleBinding and SCC RoleBinding", func() {
				operator.RestoreNamespaceSyncDefaults(sharedClients)

				ns := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-baseline")
				oc.CreateNewNamespace(ns)
				DeferCleanup(func() {
					oc.DeleteProjectIgnoreErrors(ns)
				})

				operator.AssertServiceAccountPresent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
				operator.AssertConfigMapPresent(sharedClients, ns, operator.NamespaceSyncTrustedCABundleConfigMap)
				operator.AssertConfigMapPresent(sharedClients, ns, operator.NamespaceSyncServiceCABundleConfigMap)
				operator.AssertRoleBindingPresent(sharedClients, ns, operator.NamespaceSyncEditRoleBinding)
				operator.AssertRoleBindingPresent(sharedClients, ns, operator.NamespaceSyncSCCRoleBinding)
				operator.AssertClusterRoleBindingSubject(sharedClients, operator.NamespaceSyncClusterInterceptorsCRB, ns, operator.NamespaceSyncPipelineSA, true)
			})
		})

		Context("Per-feature toggles", func() {
			// Each toggle test disables exactly one feature flag, creates a
			// namespace, and asserts only that resource is absent while the
			// other three are still created. It then re-enables the flag and
			// asserts the resource appears retroactively in that same
			// namespace, without recreating it.
			DescribeTable("disabling a single feature flag only affects its own resource",
				func(field, resourceKind string, assertAbsent, assertPresent, assertOthersStillPresent func(ns string)) {
					operator.RestoreNamespaceSyncDefaults(sharedClients)
					operator.PatchNamespaceSync(sharedClients, field+":false")

					ns := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-toggle")
					oc.CreateNewNamespace(ns)
					DeferCleanup(func() {
						oc.DeleteProjectIgnoreErrors(ns)
						operator.RestoreNamespaceSyncDefaults(sharedClients)
					})

					By("the toggled-off resource (" + resourceKind + ") is absent, the others are present")
					assertAbsent(ns)
					assertOthersStillPresent(ns)

					By("re-enabling the flag retroactively syncs the resource into the existing namespace")
					operator.PatchNamespaceSync(sharedClients, field+":true")
					assertPresent(ns)
				},
				Entry("createPipelineSA", "\"createPipelineSA\"", "pipeline SA",
					func(ns string) {
						operator.AssertServiceAccountAbsent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
					},
					func(ns string) {
						operator.AssertServiceAccountPresent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
					},
					func(ns string) {
						operator.AssertConfigMapPresent(sharedClients, ns, operator.NamespaceSyncTrustedCABundleConfigMap)
						operator.AssertRoleBindingPresent(sharedClients, ns, operator.NamespaceSyncEditRoleBinding)
						// pipelines-scc-rolebinding is intentionally NOT asserted here:
						// ensureSCCRoleBinding defers creation until the pipeline SA
						// exists (it relies on the SA-created event to re-enqueue), so
						// with createPipelineSA:false it never appears. This differs
						// from ensureEditRoleBinding, which binds the SA name
						// unconditionally regardless of whether the SA object exists.
					},
				),
				Entry("createCABundles", "\"createCABundles\"", "CA bundle ConfigMaps",
					func(ns string) {
						operator.AssertConfigMapNotPresent(sharedClients, ns, operator.NamespaceSyncTrustedCABundleConfigMap)
					},
					func(ns string) {
						operator.AssertConfigMapPresent(sharedClients, ns, operator.NamespaceSyncTrustedCABundleConfigMap)
					},
					func(ns string) {
						operator.AssertServiceAccountPresent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
					},
				),
				Entry("createEditRoleBinding", "\"createEditRoleBinding\"", "edit RoleBinding",
					func(ns string) {
						operator.AssertRoleBindingNotPresent(sharedClients, ns, operator.NamespaceSyncEditRoleBinding)
					},
					func(ns string) {
						operator.AssertRoleBindingPresent(sharedClients, ns, operator.NamespaceSyncEditRoleBinding)
					},
					func(ns string) {
						operator.AssertServiceAccountPresent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
					},
				),
				Entry("createSCCRoleBinding", "\"createSCCRoleBinding\"", "SCC RoleBinding",
					func(ns string) {
						operator.AssertRoleBindingNotPresent(sharedClients, ns, operator.NamespaceSyncSCCRoleBinding)
					},
					func(ns string) {
						operator.AssertRoleBindingPresent(sharedClients, ns, operator.NamespaceSyncSCCRoleBinding)
					},
					func(ns string) {
						operator.AssertServiceAccountPresent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
					},
				),
			)
		})

		Context("Secret binding — named secret", func() {
			It("binds on create, unbinds on delete, and stays unbound once the binding rule is removed", func() {
				operator.RestoreNamespaceSyncDefaults(sharedClients)

				ns := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-secret-named")
				oc.CreateNewNamespace(ns)
				secretName := "quay-robot"
				DeferCleanup(func() {
					oc.DeleteProjectIgnoreErrors(ns)
					operator.RestoreNamespaceSyncDefaults(sharedClients)
				})

				operator.AssertServiceAccountPresent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
				operator.PatchNamespaceSync(sharedClients, `"secretBindings":[{"secretName":"`+secretName+`"}]`)

				By("the Secret does not exist yet: the SA must not reference it")
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretName, false)

				By("creating the Secret binds it to the pipeline SA")
				operator.CreateDockerConfigSecret(sharedClients, ns, secretName, nil)
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretName, true)
				operator.AssertServiceAccountSecretRef(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretName, true)

				By("deleting the Secret removes the reference again")
				operator.DeleteSecretIgnoreNotFound(sharedClients, ns, secretName)
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretName, false)

				By("recreating the Secret and then dropping the binding rule leaves it unbound")
				operator.CreateDockerConfigSecret(sharedClients, ns, secretName, nil)
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretName, true)
				operator.PatchNamespaceSync(sharedClients, `"secretBindings":[]`)
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretName, false)
			})
		})

		Context("Secret binding — label selector", func() {
			It("binds all matching secrets and unbinds only the one that stops matching", func() {
				operator.RestoreNamespaceSyncDefaults(sharedClients)

				ns := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-secret-label")
				oc.CreateNewNamespace(ns)
				const labelKey, labelValue = "quay.io/robot-token", "true"
				const secretA, secretB, secretUnrelated = "quay-robot-a", "quay-robot-b", "unrelated-secret"
				DeferCleanup(func() {
					oc.DeleteProjectIgnoreErrors(ns)
					operator.RestoreNamespaceSyncDefaults(sharedClients)
				})

				operator.AssertServiceAccountPresent(sharedClients, ns, operator.NamespaceSyncPipelineSA)

				// A Secret that never matches any binding rule must be left
				// alone by the controller throughout this whole spec.
				operator.CreateDockerConfigSecret(sharedClients, ns, secretUnrelated, nil)

				operator.PatchNamespaceSync(sharedClients,
					`"secretBindings":[{"labelSelector":{"matchLabels":{"`+labelKey+`":"`+labelValue+`"}}}]`)

				operator.CreateDockerConfigSecret(sharedClients, ns, secretA, map[string]string{labelKey: labelValue})
				operator.CreateDockerConfigSecret(sharedClients, ns, secretB, map[string]string{labelKey: labelValue})

				By("both matching secrets get bound")
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretA, true)
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretB, true)
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretUnrelated, false)

				By("removing the label from one secret unbinds only that one")
				operator.RemoveSecretLabel(sharedClients, ns, secretB, labelKey)
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretB, false)
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretA, true)
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretUnrelated, false)
			})
		})

		Context("Self-healing", func() {
			It("recreates manually deleted resources and re-adds a manually removed secret ref", func() {
				operator.RestoreNamespaceSyncDefaults(sharedClients)

				ns := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-selfheal")
				oc.CreateNewNamespace(ns)
				secretName := "quay-robot"
				DeferCleanup(func() {
					oc.DeleteProjectIgnoreErrors(ns)
					operator.RestoreNamespaceSyncDefaults(sharedClients)
				})

				operator.AssertServiceAccountPresent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
				operator.AssertRoleBindingPresent(sharedClients, ns, operator.NamespaceSyncEditRoleBinding)
				operator.AssertRoleBindingPresent(sharedClients, ns, operator.NamespaceSyncSCCRoleBinding)

				By("deleting the pipeline SA: it must be recreated without any TektonConfig change")
				oc.DeleteResourceInNamespace("serviceaccount", operator.NamespaceSyncPipelineSA, ns)
				operator.AssertServiceAccountPresent(sharedClients, ns, operator.NamespaceSyncPipelineSA)

				By("deleting the edit RoleBinding: it must be recreated")
				oc.DeleteResourceInNamespace("rolebinding", operator.NamespaceSyncEditRoleBinding, ns)
				operator.AssertRoleBindingPresent(sharedClients, ns, operator.NamespaceSyncEditRoleBinding)

				By("deleting the SCC RoleBinding: it must be recreated")
				oc.DeleteResourceInNamespace("rolebinding", operator.NamespaceSyncSCCRoleBinding, ns)
				operator.AssertRoleBindingPresent(sharedClients, ns, operator.NamespaceSyncSCCRoleBinding)

				By("manually clearing a managed secret ref: it must be re-added on the next reconcile")
				operator.PatchNamespaceSync(sharedClients, `"secretBindings":[{"secretName":"`+secretName+`"}]`)
				operator.CreateDockerConfigSecret(sharedClients, ns, secretName, nil)
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretName, true)

				operator.ClearServiceAccountSecretRefs(sharedClients, ns, operator.NamespaceSyncPipelineSA)
				operator.AssertServiceAccountImagePullSecret(sharedClients, ns, operator.NamespaceSyncPipelineSA, secretName, true)
			})
		})

		Context("Namespace deletion", func() {
			It("removes the pipeline SA subject from the cluster-scoped ClusterInterceptors CRB", func() {
				operator.RestoreNamespaceSyncDefaults(sharedClients)

				ns := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-nsdelete")
				oc.CreateNewNamespace(ns)

				operator.AssertServiceAccountPresent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
				operator.AssertClusterRoleBindingSubject(sharedClients, operator.NamespaceSyncClusterInterceptorsCRB, ns, operator.NamespaceSyncPipelineSA, true)

				oc.DeleteProjectIgnoreErrors(ns)
				operator.AssertClusterRoleBindingSubject(sharedClients, operator.NamespaceSyncClusterInterceptorsCRB, ns, operator.NamespaceSyncPipelineSA, false)
			})
		})

		Context("namespaceSelector scoping", func() {
			It("syncs only namespaces matching matchLabels", func() {
				operator.RestoreNamespaceSyncDefaults(sharedClients)

				const labelKey, labelValue = "pipelines.tekton.dev/nssync-e2e", "true"
				matching := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-sel-match")
				nonMatching := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-sel-nomatch")

				// The selector is applied before either namespace is created.
				// namespaceMatchesSelector short-circuits reconcileNamespace
				// entirely for non-matching namespaces (pkg/reconciler/openshift/
				// namespacesync/reconciler.go), so there is no cleanup path that
				// retroactively removes resources from a namespace that was
				// synced before the selector excluded it. Creating nonMatching
				// beforehand would make AssertServiceAccountAbsent wait forever.
				operator.PatchNamespaceSync(sharedClients, `"namespaceSelector":{"matchLabels":{"`+labelKey+`":"`+labelValue+`"}}`)

				oc.CreateNewNamespace(matching)
				oc.LabelNamespace(matching, labelKey+"="+labelValue)
				oc.CreateNewNamespace(nonMatching)
				DeferCleanup(func() {
					oc.DeleteProjectIgnoreErrors(matching)
					oc.DeleteProjectIgnoreErrors(nonMatching)
					operator.RestoreNamespaceSyncDefaults(sharedClients)
				})

				operator.AssertServiceAccountPresent(sharedClients, matching, operator.NamespaceSyncPipelineSA)
				operator.AssertServiceAccountAbsent(sharedClients, nonMatching, operator.NamespaceSyncPipelineSA)
			})

			It("an explicit empty namespaceSelector ({}) opts out every namespace", func() {
				operator.RestoreNamespaceSyncDefaults(sharedClients)
				operator.PatchNamespaceSync(sharedClients, `"namespaceSelector":{}`)

				ns := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-sel-optout")
				oc.CreateNewNamespace(ns)
				DeferCleanup(func() {
					oc.DeleteProjectIgnoreErrors(ns)
					operator.RestoreNamespaceSyncDefaults(sharedClients)
				})

				operator.AssertServiceAccountAbsent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
			})
		})

		Context("System namespace exclusion", func() {
			// The ignore pattern (pkg/reconciler/common.NamespaceIgnorePattern)
			// is "^(openshift|kube)-|...|^kube-system$" — note it does NOT
			// include "default", so we only assert against namespaces that
			// actually match the real pattern.
			It("never touches namespaces matching the system-namespace ignore pattern", func() {
				operator.RestoreNamespaceSyncDefaults(sharedClients)
				Consistently(func() bool {
					_, err := sharedClients.KubeClient.Kube.CoreV1().ServiceAccounts("kube-public").
						Get(context.TODO(), operator.NamespaceSyncPipelineSA, metav1.GetOptions{})
					return err != nil
				}, "30s", "5s").Should(BeTrue(), "expected no pipeline SA to ever be created in kube-public")
			})
		})

		Context("Legacy spec.params migration", func() {
			It("maps createRbacResource/createCABundleConfigMaps/legacyPipelineRbac onto typed defaults", func() {
				operator.RemoveNamespaceSync(sharedClients)
				DeferCleanup(func() {
					operator.ClearLegacyNamespaceSyncParams(sharedClients)
					operator.RestoreNamespaceSyncDefaults(sharedClients)
				})

				By("createRbacResource=false overrides legacyPipelineRbac=true: SA, SCC and edit RoleBinding all disabled")
				operator.PatchLegacyNamespaceSyncParams(sharedClients, "false", "true", "true")

				ns := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-legacy")
				oc.CreateNewNamespace(ns)
				DeferCleanup(func() {
					oc.DeleteProjectIgnoreErrors(ns)
				})

				operator.AssertServiceAccountAbsent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
				operator.AssertRoleBindingNotPresent(sharedClients, ns, operator.NamespaceSyncEditRoleBinding)
				operator.AssertRoleBindingNotPresent(sharedClients, ns, operator.NamespaceSyncSCCRoleBinding)
				// createCABundleConfigMaps is independent of the master switch.
				operator.AssertConfigMapPresent(sharedClients, ns, operator.NamespaceSyncTrustedCABundleConfigMap)
			})

			// The previous spec only proves SetDefaults' in-memory behavior
			// (every webhook admission recomputes the typed fields from
			// spec.params, but never writes the result back). This spec
			// proves the separate, persisted migration: a one-time
			// pre-upgrade job that actually rewrites the stored CR so the
			// legacy params are gone for good. That job only re-runs when
			// the operator version changes, so it's forced here by
			// resetting the pre-upgrade-version status annotation, as if
			// the operator had just been upgraded.
			It("persists the migration onto the stored CR via the pre-upgrade job, not just in-memory defaulting", func() {
				operator.RemoveNamespaceSync(sharedClients)
				DeferCleanup(func() {
					operator.ClearLegacyNamespaceSyncParams(sharedClients)
					operator.RestoreNamespaceSyncDefaults(sharedClients)
				})

				By("setting a legacy param (createRbacResource=false) while namespaceSync is unset")
				operator.PatchLegacyNamespaceSyncParams(sharedClients, "false", "", "")

				By("forcing the pre-upgrade job to re-run, as if the operator had just been upgraded")
				operator.ForcePreUpgradeRerun(sharedClients)

				By("the pre-upgrade job rewrites the stored spec: legacy param removed, typed default persisted")
				operator.AssertLegacyNamespaceSyncParamsPersisted(sharedClients)

				By("a namespace created afterwards still honors the migrated, now-persisted typed default")
				ns := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-legacy-persist")
				oc.CreateNewNamespace(ns)
				DeferCleanup(func() {
					oc.DeleteProjectIgnoreErrors(ns)
				})
				operator.AssertServiceAccountAbsent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
			})
		})

		Context("Upgrade compatibility", func() {
			It("keeps syncing with documented defaults when namespaceSync is entirely absent", func() {
				operator.RemoveNamespaceSync(sharedClients)
				DeferCleanup(func() {
					operator.RestoreNamespaceSyncDefaults(sharedClients)
				})

				ns := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("nssync-upgrade")
				oc.CreateNewNamespace(ns)
				DeferCleanup(func() {
					oc.DeleteProjectIgnoreErrors(ns)
				})

				operator.AssertServiceAccountPresent(sharedClients, ns, operator.NamespaceSyncPipelineSA)
				operator.AssertRoleBindingPresent(sharedClients, ns, operator.NamespaceSyncEditRoleBinding)
				operator.AssertRoleBindingPresent(sharedClients, ns, operator.NamespaceSyncSCCRoleBinding)
				operator.AssertConfigMapPresent(sharedClients, ns, operator.NamespaceSyncTrustedCABundleConfigMap)
			})
		})
	})
