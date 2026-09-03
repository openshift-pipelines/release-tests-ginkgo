// Package operator (this file) provides helpers for exercising the
// NamespaceSyncController: spec.platforms.openshift.namespaceSync on
// TektonConfig (OpenShift only). See issue #76 for the full test plan.
//
// The Go module here pins an older tektoncd/operator release (see go.mod)
// that predates the NamespaceSync API types, so every TektonConfig mutation
// below goes through raw JSON merge patches via oc.UpdateTektonConfig
// instead of the typed v1alpha1.NamespaceSyncConfig struct. This mirrors the
// existing buildPrunerPatch / SetTektonPrunerGlobalConfig pattern in pkg/oc.
package operator

import (
	"context"
	"fmt"
	"log"

	. "github.com/onsi/gomega" //nolint:revive,staticcheck // dot import is idiomatic for Gomega
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// Resource names managed by the NamespaceSyncController. Kept in sync with
// tektoncd/operator's pkg/reconciler/openshift/namespacesync/reconciler.go.
const (
	// NamespaceSyncPipelineSA is the per-namespace ServiceAccount name.
	NamespaceSyncPipelineSA = "pipeline"
	// NamespaceSyncEditRoleBinding binds the pipeline SA to the built-in edit ClusterRole.
	NamespaceSyncEditRoleBinding = "openshift-pipelines-edit"
	// NamespaceSyncSCCRoleBinding grants the pipeline SA use of the default/custom SCC.
	NamespaceSyncSCCRoleBinding = "pipelines-scc-rolebinding"
	// NamespaceSyncTrustedCABundleConfigMap is one of the two injected CA bundle ConfigMaps.
	NamespaceSyncTrustedCABundleConfigMap = "config-trusted-cabundle"
	// NamespaceSyncServiceCABundleConfigMap is the other injected CA bundle ConfigMap.
	NamespaceSyncServiceCABundleConfigMap = "config-service-cabundle"
	// NamespaceSyncClusterInterceptorsCRB is the cluster-scoped ClusterRoleBinding
	// that grants every synced namespace's pipeline SA access to ClusterInterceptors.
	NamespaceSyncClusterInterceptorsCRB = "openshift-pipelines-clusterinterceptors"
)

// ---------------------------------------------------------------------------
// TektonConfig patching
// ---------------------------------------------------------------------------

// PatchNamespaceSync merges the given raw JSON fields into
// spec.platforms.openshift.namespaceSync and waits for TektonConfig to
// report Ready. fields must be one or more comma-separated `"key":value`
// JSON fragments, e.g. `"createPipelineSA":false`.
func PatchNamespaceSync(cs *clients.Clients, fields string) {
	patch := fmt.Sprintf(`{"spec":{"platforms":{"openshift":{"namespaceSync":{%s}}}}}`, fields)
	log.Printf("Patching TektonConfig namespaceSync: %s\n", patch)
	oc.UpdateTektonConfig(patch)
	AssertTektonConfigCRReadyStatus(cs, store.GetCRNames())
}

// RemoveNamespaceSync deletes spec.platforms.openshift.namespaceSync entirely
// (JSON merge-patch null), simulating a pre-upgrade TektonConfig that
// predates this feature. The operator must fall back to in-memory defaults
// on the very next reconcile without requiring any user edit.
func RemoveNamespaceSync(cs *clients.Clients) {
	log.Println("Removing spec.platforms.openshift.namespaceSync from TektonConfig")
	oc.UpdateTektonConfig(`{"spec":{"platforms":{"openshift":{"namespaceSync":null}}}}`)
	AssertTektonConfigCRReadyStatus(cs, store.GetCRNames())
}

// RestoreNamespaceSyncDefaults resets every NamespaceSync feature flag to its
// documented default (true), clears secretBindings and namespaceSelector.
// Safe to call from DeferCleanup/BeforeEach to guarantee each spec starts
// from a known baseline regardless of what a previous spec left behind.
func RestoreNamespaceSyncDefaults(cs *clients.Clients) {
	PatchNamespaceSync(cs,
		`"createPipelineSA":true,"createCABundles":true,"createEditRoleBinding":true,`+
			`"createSCCRoleBinding":true,"secretBindings":[],"namespaceSelector":null`)
}

// PatchLegacyNamespaceSyncParams sets the three legacy spec.params entries
// that migrateNamespaceSyncParams maps onto the typed namespaceSync fields
// (createRbacResource, createCABundleConfigMaps, legacyPipelineRbac). Callers
// must RemoveNamespaceSync first: migration only fills typed fields that are
// still nil, so a namespaceSync block with explicit values already set (e.g.
// from RestoreNamespaceSyncDefaults) would silently ignore these params.
func PatchLegacyNamespaceSyncParams(cs *clients.Clients, createRbacResource, createCABundleConfigMaps, legacyPipelineRbac string) {
	patch := fmt.Sprintf(
		`{"spec":{"params":[{"name":"createRbacResource","value":"%s"},{"name":"createCABundleConfigMaps","value":"%s"},{"name":"legacyPipelineRbac","value":"%s"}]}}`,
		createRbacResource, createCABundleConfigMaps, legacyPipelineRbac)
	log.Printf("Patching TektonConfig legacy namespaceSync params: %s\n", patch)
	oc.UpdateTektonConfig(patch)
	AssertTektonConfigCRReadyStatus(cs, store.GetCRNames())
}

// ClearLegacyNamespaceSyncParams empties spec.params, undoing PatchLegacyNamespaceSyncParams.
func ClearLegacyNamespaceSyncParams(cs *clients.Clients) {
	oc.UpdateTektonConfig(`{"spec":{"params":[]}}`)
	AssertTektonConfigCRReadyStatus(cs, store.GetCRNames())
}

// namespaceSyncPreUpgradeVersionAnnotation mirrors
// v1alpha1.PreUpgradeVersionKey (tektoncd/operator's
// pkg/apis/operator/v1alpha1/const.go). Kept as a local string constant
// instead of importing that package, consistent with this file's use of raw
// JSON patches to stay compatible with the older pinned operator release
// (see the package doc comment above).
const namespaceSyncPreUpgradeVersionAnnotation = "operator.tekton.dev/pre-upgrade-version"

// ForcePreUpgradeRerun clears the pre-upgrade-version status annotation on
// TektonConfig via a status-subresource update. Upgrade.RunPreUpgrade
// (tektoncd/operator's pkg/reconciler/shared/tektonconfig/upgrade) only
// re-executes its registered pre-upgrade functions — including the
// persisted legacy spec.params migration — when this annotation differs
// from the running operator version. Clearing it forces the very next
// TektonConfig reconcile to treat the CR as freshly upgraded and re-run
// every pre-upgrade function again, letting tests exercise one-time
// persisted upgrade migrations without needing an actual two-version
// operator upgrade in CI.
func ForcePreUpgradeRerun(cs *clients.Clients) {
	tc, err := cs.Operator.TektonConfigs().Get(context.TODO(), "config", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get TektonConfig to reset pre-upgrade-version")
	if tc.Status.Annotations == nil {
		return
	}
	delete(tc.Status.Annotations, namespaceSyncPreUpgradeVersionAnnotation)
	_, err = cs.Operator.TektonConfigs().UpdateStatus(context.TODO(), tc, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to reset pre-upgrade-version annotation")
}

// AssertLegacyNamespaceSyncParamsPersisted polls until spec.params no longer
// contains any of the three legacy NamespaceSync keys (createRbacResource,
// createCABundleConfigMaps, legacyPipelineRbac) and the pre-upgrade-version
// status annotation has been restored to a non-empty value. This complements
// the "maps ... onto typed defaults" spec, which only proves SetDefaults'
// in-memory behavior: this proves the pre-upgrade job actually rewrites the
// stored CR (see migrateLegacyNamespaceSyncParams in tektoncd/operator's
// pkg/reconciler/shared/tektonconfig/upgrade/pre_upgrade.go) — without it,
// the deprecated params would remain in spec.params forever.
func AssertLegacyNamespaceSyncParamsPersisted(cs *clients.Clients) {
	legacyKeys := map[string]bool{
		"createRbacResource":       true,
		"createCABundleConfigMaps": true,
		"legacyPipelineRbac":       true,
	}
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		tc, err := cs.Operator.TektonConfigs().Get(context.TODO(), "config", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, p := range tc.Spec.Params {
			if legacyKeys[p.Name] {
				log.Printf("Legacy param %s is still present in spec.params, waiting for the pre-upgrade job\n", p.Name)
				return false, nil
			}
		}
		if tc.Status.Annotations[namespaceSyncPreUpgradeVersionAnnotation] == "" {
			log.Println("pre-upgrade-version annotation not yet restored, waiting for the pre-upgrade job")
			return false, nil
		}
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected legacy spec.params to be removed and pre-upgrade-version restored after the pre-upgrade job re-ran")
}

// ---------------------------------------------------------------------------
// Assertions not already covered by pkg/operator/rbac.go
// ---------------------------------------------------------------------------

// AssertServiceAccountAbsent polls until the named ServiceAccount does not
// exist in the given namespace. Complements AssertServiceAccountPresent.
func AssertServiceAccountAbsent(cs *clients.Clients, ns, name string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that ServiceAccount %s doesn't exist in namespace %s\n", name, ns)
		_, err := cs.KubeClient.Kube.CoreV1().ServiceAccounts(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if apierrs.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected ServiceAccount %v not present in namespace %v", name, ns)
}

// AssertConfigMapNotPresent polls until the named ConfigMap does not exist
// in the given namespace. Complements AssertConfigMapPresent.
func AssertConfigMapNotPresent(cs *clients.Clients, ns, name string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that ConfigMap %s doesn't exist in namespace %s\n", name, ns)
		_, err := cs.KubeClient.Kube.CoreV1().ConfigMaps(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if apierrs.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected ConfigMap %v not present in namespace %v", name, ns)
}

// AssertServiceAccountImagePullSecret polls until the named ServiceAccount's
// imagePullSecrets does (present=true) or does not (present=false) reference secretName.
func AssertServiceAccountImagePullSecret(cs *clients.Clients, ns, saName, secretName string, present bool) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying imagePullSecrets of %s/%s contains %s: %v\n", ns, saName, secretName, present)
		sa, err := cs.KubeClient.Kube.CoreV1().ServiceAccounts(ns).Get(context.TODO(), saName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		found := false
		for _, ref := range sa.ImagePullSecrets {
			if ref.Name == secretName {
				found = true
				break
			}
		}
		return found == present, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected ServiceAccount %s/%s imagePullSecrets present=%v for %s", ns, saName, present, secretName)
}

// AssertServiceAccountSecretRef polls until the named ServiceAccount's
// secrets list does (present=true) or does not (present=false) reference secretName.
// The NamespaceSyncController binds secretBindings to both imagePullSecrets and secrets.
func AssertServiceAccountSecretRef(cs *clients.Clients, ns, saName, secretName string, present bool) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying secrets of %s/%s contains %s: %v\n", ns, saName, secretName, present)
		sa, err := cs.KubeClient.Kube.CoreV1().ServiceAccounts(ns).Get(context.TODO(), saName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		found := false
		for _, ref := range sa.Secrets {
			if ref.Name == secretName {
				found = true
				break
			}
		}
		return found == present, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected ServiceAccount %s/%s secrets present=%v for %s", ns, saName, present, secretName)
}

// ClearServiceAccountSecretRefs wipes imagePullSecrets and secrets from the
// named ServiceAccount, simulating an admin accidentally removing a managed
// secret reference. Used to exercise self-healing.
func ClearServiceAccountSecretRefs(cs *clients.Clients, ns, saName string) {
	sa, err := cs.KubeClient.Kube.CoreV1().ServiceAccounts(ns).Get(context.TODO(), saName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get ServiceAccount %s/%s", ns, saName)
	sa.ImagePullSecrets = nil
	sa.Secrets = nil
	_, err = cs.KubeClient.Kube.CoreV1().ServiceAccounts(ns).Update(context.TODO(), sa, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to clear secret refs on ServiceAccount %s/%s", ns, saName)
}

// AssertClusterRoleBindingSubject polls until the named ClusterRoleBinding
// does (present=true) or does not (present=false) contain a ServiceAccount
// subject matching subjectName/subjectNamespace. A missing ClusterRoleBinding
// counts as "no subjects present".
func AssertClusterRoleBindingSubject(cs *clients.Clients, crbName, subjectNamespace, subjectName string, present bool) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying ClusterRoleBinding %s subject %s/%s present=%v\n", crbName, subjectNamespace, subjectName, present)
		crb, err := cs.KubeClient.Kube.RbacV1().ClusterRoleBindings().Get(context.TODO(), crbName, metav1.GetOptions{})
		if err != nil {
			if apierrs.IsNotFound(err) {
				return !present, nil
			}
			return false, err
		}
		found := false
		for _, s := range crb.Subjects {
			if s.Kind == rbacv1.ServiceAccountKind && s.Name == subjectName && s.Namespace == subjectNamespace {
				found = true
				break
			}
		}
		return found == present, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected ClusterRoleBinding %s subject %s/%s present=%v", crbName, subjectNamespace, subjectName, present)
}

// ---------------------------------------------------------------------------
// Secret fixtures
// ---------------------------------------------------------------------------

// CreateDockerConfigSecret creates a minimal dockerconfigjson Secret in the
// given namespace, optionally with labels (for labelSelector binding tests).
func CreateDockerConfigSecret(cs *clients.Clients, ns, name string, labels map[string]string) {
	_, err := cs.KubeClient.Kube.CoreV1().Secrets(ns).Create(context.TODO(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte("{}")},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create Secret %s/%s", ns, name)
}

// DeleteSecretIgnoreNotFound deletes a Secret, tolerating it already being gone.
func DeleteSecretIgnoreNotFound(cs *clients.Clients, ns, name string) {
	err := cs.KubeClient.Kube.CoreV1().Secrets(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil && !apierrs.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred(), "failed to delete Secret %s/%s", ns, name)
	}
}

// RemoveSecretLabel deletes labelKey from the named Secret's labels, without
// touching the binding rule in TektonConfig — used to prove that label-based
// unbinding is driven by the live Secret state, not the rule itself.
func RemoveSecretLabel(cs *clients.Clients, ns, name, labelKey string) {
	secret, err := cs.KubeClient.Kube.CoreV1().Secrets(ns).Get(context.TODO(), name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get Secret %s/%s", ns, name)
	delete(secret.Labels, labelKey)
	_, err = cs.KubeClient.Kube.CoreV1().Secrets(ns).Update(context.TODO(), secret, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to update Secret %s/%s", ns, name)
}
