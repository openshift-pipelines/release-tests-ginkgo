package operator_test

import (
	"context"
	"fmt"
	"log"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega
	operatorv1alpha1 "github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/retry"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	olmpkg "github.com/openshift-pipelines/release-tests-ginkgo/pkg/olm"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

const multiclusterCustomNP = "multicluster-custom-test"

// Unique per-run identity so two suite runs can never claim each other's resources.
var (
	mcRunPrefix  string
	mcWorkerName string

	mcSecretUID  types.UID
	mcClusterUID types.UID
	mcKueueUID   types.UID
	mcKueueOwned bool

	mcOrigScheduler *operatorv1alpha1.Scheduler
	// mcOrigNPJSON is the snapshotted spec.networkPolicy as raw JSON. Empty means
	// the field was absent. The pinned operator client predates NetworkPolicy, so
	// this stays untyped while Scheduler uses the typed client.
	mcOrigNPJSON      string
	mcOrigMAGExists   bool
	mcOrigMAGDisabled string
)

// mcWorkerLabels marks resources this run created. Cleanup refuses to delete
// anything without these exact labels and, where recorded, a matching UID.
func mcWorkerLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "release-tests-ginkgo",
		"app.kubernetes.io/instance":   mcRunPrefix,
	}
}

func mcOwnsWorker(labels map[string]string) bool {
	return labels["app.kubernetes.io/managed-by"] == "release-tests-ginkgo" &&
		labels["app.kubernetes.io/instance"] == mcRunPrefix && mcRunPrefix != ""
}

var (
	kueueCRGVR           = schema.GroupVersionResource{Group: "kueue.openshift.io", Version: "v1", Resource: "kueues"}
	multiKueueClusterGVR = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta2", Resource: "multikueueclusters"}
	tektonConfigGVR      = schema.GroupVersionResource{Group: "operator.tekton.dev", Version: "v1alpha1", Resource: "tektonconfigs"}
)

// multiclusterNPByComponent lists the NetworkPolicies each multicluster component
// owns. It is intentionally separate from networkPoliciesByComponent: these CRs only
// exist while the scheduler runs in multi-cluster mode, and assertNetworkPoliciesForComponent
// calls Skip when a CR is missing, which would abort TC01 to TC03 part way through.
var multiclusterNPByComponent = map[string][]string{
	"scheduler": {
		"scheduler-controller",
		"scheduler-controller-default-deny",
		"scheduler-webhook",
		"scheduler-webhook-default-deny",
	},
	"proxy-aae": {
		"proxy-aae",
		"proxy-aae-default-deny",
	},
	"syncer-service": {
		"syncer-service-controller",
		"syncer-service-default-deny",
	},
}

// hubOnlyNPComponents are deployed only while multi-cluster-role is Hub.
var hubOnlyNPComponents = []string{"proxy-aae", "syncer-service"}

// assertMulticlusterNPs verifies every NetworkPolicy of the named components.
func assertMulticlusterNPs(shouldBePresent bool, components ...string) {
	for _, component := range components {
		policies, ok := multiclusterNPByComponent[component]
		Expect(ok).To(BeTrue(), "unknown multicluster component %q", component)
		for _, name := range policies {
			assertNetworkPolicyPresence(name, config.TargetNamespace, shouldBePresent)
		}
	}
}

// setSchedulerMode patches the TektonConfig scheduler fields that gate which
// multicluster components, and therefore which NetworkPolicies, the operator maintains.
func setSchedulerMode(disabled, multiClusterDisabled bool, role string) {
	patch := fmt.Sprintf(
		`{"spec":{"scheduler":{"disabled":%t,"multi-cluster-disabled":%t,"multi-cluster-role":"%s"}}}`,
		disabled, multiClusterDisabled, role)
	cmd.MustSucceed("oc", "patch", "TektonConfig", "config", "--type=merge", "-p", patch)
	log.Printf("Patched scheduler: disabled=%t multi-cluster-disabled=%t role=%q",
		disabled, multiClusterDisabled, role)
	operator.EnsureTektonConfigStatusInstalled(sharedClients.TektonConfig(), store.GetCRNames())
}

// snapshotTektonConfig records the exact scheduler object and the raw
// spec.networkPolicy JSON so cleanup can restore the originals rather than
// assumed defaults.
func snapshotTektonConfig() {
	tc, err := sharedClients.TektonConfig().Get(context.TODO(), "config", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to read TektonConfig for snapshot")
	mcOrigScheduler = tc.Spec.Scheduler.DeepCopy()
	mcOrigNPJSON = originalNetworkPolicyJSON()

	result := cmd.Run("oc", "get", "manualapprovalgate", "manual-approval-gate",
		"-o", "jsonpath={.spec.networkPolicy.disabled}")
	if result.ExitCode == 0 {
		mcOrigMAGExists = true
		mcOrigMAGDisabled = result.Stdout()
	}
}

// restoreNetworkPolicyJSON writes back a previously snapshotted spec.networkPolicy.
// An empty snapshot means the field was absent and is removed.
func restoreNetworkPolicyJSON(snapshot string) {
	if snapshot == "" {
		cmd.MustSucceed("oc", "patch", "TektonConfig", "config", "--type=json",
			"-p", `[{"op":"remove","path":"/spec/networkPolicy"}]`)
		return
	}
	cmd.MustSucceed("oc", "patch", "TektonConfig", "config", "--type=merge",
		"-p", fmt.Sprintf(`{"spec":{"networkPolicy":%s}}`, snapshot))
}

// restoreTektonConfig writes back the exact snapshotted scheduler object and
// NetworkPolicy JSON and waits for the operator to settle.
func restoreTektonConfig() {
	Expect(mcOrigScheduler).NotTo(BeNil(), "no TektonConfig snapshot to restore")
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest, getErr := sharedClients.TektonConfig().Get(context.TODO(), "config", metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		latest.Spec.Scheduler = *mcOrigScheduler.DeepCopy()
		_, updateErr := sharedClients.TektonConfig().Update(context.TODO(), latest, metav1.UpdateOptions{})
		return updateErr
	})
	Expect(err).NotTo(HaveOccurred(), "failed to restore TektonConfig scheduler")
	restoreNetworkPolicyJSON(mcOrigNPJSON)
	operator.EnsureTektonConfigStatusInstalled(sharedClients.TektonConfig(), store.GetCRNames())

	if mcOrigMAGExists {
		disabled := mcOrigMAGDisabled
		if disabled == "" {
			disabled = "false"
		}
		cmd.MustSucceed("oc", "patch", "manualapprovalgate", "manual-approval-gate",
			"--type=merge", "-p", fmt.Sprintf(`{"spec":{"networkPolicy":{"disabled":%s}}}`, disabled))
	}
}

// ensureKueueCR creates a minimal Kueue CR only when none exists. This spec never
// dispatches a PipelineRun, so no external framework is configured; an existing
// Kueue CR is left untouched.
func ensureKueueCR() {
	resource := sharedClients.Dynamic.Resource(kueueCRGVR)
	existing, err := resource.Get(context.TODO(), "cluster", metav1.GetOptions{})
	if err == nil {
		log.Println("Kueue CR already exists, leaving it untouched")
		_ = existing
	} else {
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "failed to read Kueue CR: %v", err)
		kueue := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "kueue.openshift.io/v1",
			"kind":       "Kueue",
			"metadata":   map[string]any{"name": "cluster", "labels": mcWorkerLabels()},
			"spec": map[string]any{
				"config":          map[string]any{"integrations": map[string]any{"frameworks": []any{"Deployment"}}},
				"managementState": "Managed",
			},
		}}
		created, err := resource.Create(context.TODO(), kueue, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create Kueue CR")
		mcKueueUID = created.GetUID()
		mcKueueOwned = true
		DeferCleanup(func() {
			current, err := resource.Get(context.TODO(), "cluster", metav1.GetOptions{})
			if err == nil && mcOwnsWorker(current.GetLabels()) && current.GetUID() == mcKueueUID {
				_ = resource.Delete(context.TODO(), "cluster", metav1.DeleteOptions{})
			}
		})
	}

	Eventually(func(g Gomega) {
		current, err := resource.Get(context.TODO(), "cluster", metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		conditions, _, _ := unstructured.NestedSlice(current.Object, "status", "conditions")
		available := false
		for _, item := range conditions {
			condition, ok := item.(map[string]any)
			if ok && condition["type"] == "Available" && condition["status"] == "True" {
				available = true
			}
		}
		g.Expect(available).To(BeTrue(), "Kueue is not Available")
	}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Succeed())

	// The kueue.x-k8s.io CRDs are installed by the Kueue operand and lag the CR
	// reaching Available, so wait for them before creating the MultiKueueCluster.
	Eventually(func(g Gomega) {
		_, err := sharedClients.Dynamic.Resource(multiKueueClusterGVR).
			List(context.TODO(), metav1.ListOptions{})
		g.Expect(err).NotTo(HaveOccurred())
	}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Succeed())
}

// ensureWorkerServiceAccount creates a dedicated ServiceAccount with only the
// permissions a MultiKueue worker needs, and returns a bounded token for it.
func ensureWorkerServiceAccount() string {
	ns := config.TargetNamespace
	kube := sharedClients.KubeClient.Kube
	saName := mcWorkerName

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: ns, Labels: mcWorkerLabels()},
	}
	if _, err := kube.CoreV1().ServiceAccounts(ns).Create(context.TODO(), sa, metav1.CreateOptions{}); err != nil {
		Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), "failed to create worker ServiceAccount: %v", err)
		existing, getErr := kube.CoreV1().ServiceAccounts(ns).Get(context.TODO(), saName, metav1.GetOptions{})
		Expect(getErr).NotTo(HaveOccurred(), "failed to read existing worker ServiceAccount")
		Expect(mcOwnsWorker(existing.Labels)).To(BeTrue(),
			"ServiceAccount %s already exists and was not created by this run", saName)
	}
	DeferCleanup(func() {
		current, err := kube.CoreV1().ServiceAccounts(ns).Get(context.TODO(), saName, metav1.GetOptions{})
		if err == nil && mcOwnsWorker(current.Labels) {
			_ = kube.CoreV1().ServiceAccounts(ns).Delete(context.TODO(), saName, metav1.DeleteOptions{})
		}
	})

	readRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Labels: mcWorkerLabels()},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kueue.x-k8s.io"}, Resources: []string{"workloads"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"pipelineruns"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
	if _, err := kube.RbacV1().ClusterRoles().Create(context.TODO(), readRole, metav1.CreateOptions{}); err != nil {
		Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), "failed to create worker ClusterRole: %v", err)
	}
	DeferCleanup(func() {
		current, err := kube.RbacV1().ClusterRoles().Get(context.TODO(), saName, metav1.GetOptions{})
		if err == nil && mcOwnsWorker(current.Labels) {
			_ = kube.RbacV1().ClusterRoles().Delete(context.TODO(), saName, metav1.DeleteOptions{})
		}
	})
	readBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Labels: mcWorkerLabels()},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: saName},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: saName, Namespace: ns}},
	}
	if _, err := kube.RbacV1().ClusterRoleBindings().Create(context.TODO(), readBinding, metav1.CreateOptions{}); err != nil {
		Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), "failed to create worker ClusterRoleBinding: %v", err)
	}
	DeferCleanup(func() {
		current, err := kube.RbacV1().ClusterRoleBindings().Get(context.TODO(), saName, metav1.GetOptions{})
		if err == nil && mcOwnsWorker(current.Labels) {
			_ = kube.RbacV1().ClusterRoleBindings().Delete(context.TODO(), saName, metav1.DeleteOptions{})
		}
	})

	writeRole := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: ns, Labels: mcWorkerLabels()},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kueue.x-k8s.io"}, Resources: []string{"workloads"}, Verbs: []string{"create", "delete", "get", "list", "patch", "update", "watch"}},
			{APIGroups: []string{"kueue.x-k8s.io"}, Resources: []string{"workloads/status"}, Verbs: []string{"get", "patch", "update"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"pipelineruns"}, Verbs: []string{"create", "delete", "get", "list", "patch", "update", "watch"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"pipelineruns/status"}, Verbs: []string{"get", "patch", "update"}},
		},
	}
	if _, err := kube.RbacV1().Roles(ns).Create(context.TODO(), writeRole, metav1.CreateOptions{}); err != nil {
		Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), "failed to create worker Role: %v", err)
	}
	DeferCleanup(func() {
		current, err := kube.RbacV1().Roles(ns).Get(context.TODO(), saName, metav1.GetOptions{})
		if err == nil && mcOwnsWorker(current.Labels) {
			_ = kube.RbacV1().Roles(ns).Delete(context.TODO(), saName, metav1.DeleteOptions{})
		}
	})
	writeBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: ns, Labels: mcWorkerLabels()},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: saName},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: saName, Namespace: ns}},
	}
	if _, err := kube.RbacV1().RoleBindings(ns).Create(context.TODO(), writeBinding, metav1.CreateOptions{}); err != nil {
		Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), "failed to create worker RoleBinding: %v", err)
	}
	DeferCleanup(func() {
		current, err := kube.RbacV1().RoleBindings(ns).Get(context.TODO(), saName, metav1.GetOptions{})
		if err == nil && mcOwnsWorker(current.Labels) {
			_ = kube.RbacV1().RoleBindings(ns).Delete(context.TODO(), saName, metav1.DeleteOptions{})
		}
	})

	expiration := int64(3 * 60 * 60)
	token, err := kube.CoreV1().ServiceAccounts(ns).CreateToken(context.TODO(), saName,
		&authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &expiration}},
		metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to mint worker token")
	Expect(token.Status.Token).NotTo(BeEmpty(), "worker token is empty")
	return token.Status.Token
}

// ensureSelfWorkerCluster registers the cluster as its own MultiKueue worker. proxy-aae
// reports ready only once a worker is registered, and the operator creates its
// NetworkPolicies only after the deployment is available.
func ensureSelfWorkerCluster() {
	// The worker is this same cluster, so Kueue connects to it from inside: the
	// in-cluster endpoint is routable from pods and is the endpoint kube-root-ca.crt
	// signs. The external API URL is neither on a managed cluster, where it resolves
	// to a public address pods cannot reach and is served by a public CA.
	const inClusterEndpoint = "https://kubernetes.default.svc:443"

	configMap, err := sharedClients.KubeClient.Kube.CoreV1().
		ConfigMaps(config.Flags.KueueOperatorNamespace).
		Get(context.TODO(), "kube-root-ca.crt", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to read kube-root-ca.crt")
	ca := []byte(configMap.Data["ca.crt"])
	Expect(ca).NotTo(BeEmpty(), "kube-root-ca.crt has no ca.crt")

	token := ensureWorkerServiceAccount()

	kubeconfig, err := clientcmd.Write(clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"self": {Server: inClusterEndpoint, CertificateAuthorityData: ca}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"self": {Token: token}},
		Contexts:       map[string]*clientcmdapi.Context{"self": {Cluster: "self", AuthInfo: "self"}},
		CurrentContext: "self",
	})
	Expect(err).NotTo(HaveOccurred(), "failed to serialize the self worker kubeconfig")

	secrets := sharedClients.KubeClient.Kube.CoreV1().Secrets(config.Flags.KueueOperatorNamespace)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcWorkerName,
			Namespace: config.Flags.KueueOperatorNamespace,
			Labels:    mcWorkerLabels(),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"kubeconfig": kubeconfig},
	}
	createdSecret, err := secrets.Create(context.TODO(), secret, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := secrets.Get(context.TODO(), mcWorkerName, metav1.GetOptions{})
		Expect(getErr).NotTo(HaveOccurred(), "failed to read the existing worker secret")
		Expect(mcOwnsWorker(existing.Labels)).To(BeTrue(),
			"secret %s already exists and was not created by this run", mcWorkerName)
		existing.Data = secret.Data
		updated, err := secrets.Update(context.TODO(), existing, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to refresh the worker secret")
		mcSecretUID = updated.GetUID()
	} else {
		Expect(err).NotTo(HaveOccurred(), "failed to create the self worker secret")
		mcSecretUID = createdSecret.GetUID()
	}
	DeferCleanup(func() {
		current, err := secrets.Get(context.TODO(), mcWorkerName, metav1.GetOptions{})
		if err == nil && mcOwnsWorker(current.Labels) && current.GetUID() == mcSecretUID {
			_ = secrets.Delete(context.TODO(), mcWorkerName, metav1.DeleteOptions{})
		}
	})

	clusters := sharedClients.Dynamic.Resource(multiKueueClusterGVR)
	worker := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta2",
		"kind":       "MultiKueueCluster",
		"metadata":   map[string]any{"name": mcWorkerName, "labels": mcWorkerLabels()},
		"spec": map[string]any{
			"clusterSource": map[string]any{
				"kubeConfig": map[string]any{"locationType": "Secret", "location": mcWorkerName},
			},
		},
	}}
	createdCluster, err := clusters.Create(context.TODO(), worker, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := clusters.Get(context.TODO(), mcWorkerName, metav1.GetOptions{})
		Expect(getErr).NotTo(HaveOccurred(), "failed to read the existing MultiKueueCluster")
		Expect(mcOwnsWorker(existing.GetLabels())).To(BeTrue(),
			"MultiKueueCluster %s already exists and was not created by this run", mcWorkerName)
		mcClusterUID = existing.GetUID()
	} else {
		Expect(err).NotTo(HaveOccurred(), "failed to create the self MultiKueueCluster")
		mcClusterUID = createdCluster.GetUID()
	}
	DeferCleanup(func() {
		current, err := clusters.Get(context.TODO(), mcWorkerName, metav1.GetOptions{})
		if err == nil && mcOwnsWorker(current.GetLabels()) && current.GetUID() == mcClusterUID {
			_ = clusters.Delete(context.TODO(), mcWorkerName, metav1.DeleteOptions{})
		}
	})

	err = wait.PollUntilContextTimeout(context.TODO(), config.APIRetry, config.APITimeout, true,
		func(ctx context.Context) (bool, error) {
			current, getErr := clusters.Get(ctx, mcWorkerName, metav1.GetOptions{})
			if getErr != nil {
				return false, nil
			}
			conditions, _, _ := unstructured.NestedSlice(current.Object, "status", "conditions")
			for _, item := range conditions {
				condition, ok := item.(map[string]any)
				if ok && condition["type"] == "Active" && condition["status"] == "True" {
					return true, nil
				}
			}
			return false, nil
		})
	Expect(err).NotTo(HaveOccurred(), "the self MultiKueue worker never became active")
}

// originalNetworkPolicyJSON returns the current spec.networkPolicy as JSON for
// exact restoration.
func originalNetworkPolicyJSON() string {
	result := cmd.Run("oc", "get", "TektonConfig", "config", "-o", "jsonpath={.spec.networkPolicy}")
	Expect(result.ExitCode).To(Equal(0), "failed to read TektonConfig networkPolicy")
	return result.Stdout()
}

var _ = Describe("Verify multicluster NetworkPolicy", Serial, Ordered, ContinueOnFailure,
	Label("e2e", "operator", "admin", "networkpolicy", "multicluster"), func() {
		BeforeEach(func() {
			lastNamespace = config.TargetNamespace
			operator.ValidateOperatorInstallStatus(sharedClients, store.GetCRNames())
		})

		// Suite-level setup: configure Kueue and put the cluster into Hub mode.
		BeforeAll(func() {
			mcRunPrefix = fmt.Sprintf("rtg-np-%x", time.Now().UnixNano())
			mcWorkerName = mcRunPrefix + "-self"

			controllerClient, err := sharedClients.NewClientFromKubeconfig(
				config.Flags.Kubeconfig, config.Flags.Cluster, config.Flags.Context)
			Expect(err).NotTo(HaveOccurred(), "failed to create controller-runtime client")

			if _, err := sharedClients.KubeClient.Kube.Discovery().
				ServerResourcesForGroupVersion("operators.coreos.com/v1alpha1"); err != nil {
				Skip("OLM API not available, cannot install multicluster prerequisites")
			}

			bootstrap := &olmpkg.ClusterBootstrap{Client: controllerClient}
			Expect(bootstrap.EnsurePrerequisites(context.TODO(),
				sharedClients.KubeClient.Kube.Discovery(), olmpkg.MulticlusterPrerequisites())).To(Succeed(),
				"failed to install multicluster prerequisites")

			snapshotTektonConfig()

			DeferCleanup(func() {
				log.Println("Restoring TektonConfig to its exact pre-test state")
				restoreTektonConfig()
			})

			ensureKueueCR()
			ensureSelfWorkerCluster()
		})

		// TC07: Hub deploys the scheduler, proxy, and syncer policies.
		Context("Hub maintains every multicluster NetworkPolicy: PIPELINES-38-TC07", Ordered, func() {
			It("should create the scheduler, proxy, and syncer policies", func() {
				setSchedulerMode(false, false, "Hub")
				assertMulticlusterNPs(true, "scheduler", "proxy-aae", "syncer-service")
			})
		})

		// TC08: Hub to Spoke must tear the hub-only policies down.
		Context("Switching the hub to a spoke removes hub-only policies: PIPELINES-38-TC08", Ordered, func() {
			It("should keep the scheduler policies and remove the proxy and syncer policies", func() {
				setSchedulerMode(false, false, "Spoke")
				assertMulticlusterNPs(true, "scheduler")
				assertMulticlusterNPs(false, hubOnlyNPComponents...)
			})
		})

		// TC09: Spoke to Hub must restore them.
		Context("Switching back to a hub restores hub-only policies: PIPELINES-38-TC09", Ordered, func() {
			It("should recreate the proxy and syncer policies", func() {
				setSchedulerMode(false, false, "Hub")
				assertMulticlusterNPs(true, "scheduler", "proxy-aae", "syncer-service")
			})
		})

		// TC10: Disabling multi-cluster collapses the hub-only components.
		Context("Disabling multi-cluster removes hub-only policies: PIPELINES-38-TC10", Ordered, func() {
			It("should keep the scheduler policies and remove the proxy and syncer policies", func() {
				setSchedulerMode(false, true, "")
				assertMulticlusterNPs(true, "scheduler")
				assertMulticlusterNPs(false, hubOnlyNPComponents...)
			})
		})

		// TC11: networkPolicy.disabled wins over the scheduler mode.
		Context("Disabling NetworkPolicy removes every multicluster policy: PIPELINES-38-TC11", Ordered, func() {
			It("should remove all policies when disabled and restore them when re-enabled", func() {
				setSchedulerMode(false, false, "Hub")

				By("Disabling NetworkPolicy on TektonConfig")
				setNetworkPolicyOnTektonConfig(true)
				operator.EnsureTektonConfigStatusInstalled(sharedClients.TektonConfig(), store.GetCRNames())
				assertMulticlusterNPs(false, "scheduler", "proxy-aae", "syncer-service")

				By("Re-enabling NetworkPolicy on TektonConfig")
				ensureNetworkPolicyEnabled()
				assertMulticlusterNPs(true, "scheduler", "proxy-aae", "syncer-service")

				DeferCleanup(ensureNetworkPolicyEnabled)
			})
		})

		// TC12: a custom policy merges with the multicluster defaults.
		//
		// The custom policy is declared on TektonConfig rather than on the proxy CR.
		// TektonConfig owns spec.networkPolicy for every component it creates and copies
		// its own value over the component's on each reconcile, so patching
		// TektonMulticlusterProxyAAE directly is not a supported way to configure it.
		Context("Custom NetworkPolicy alongside the multicluster defaults: PIPELINES-38-TC12", Ordered, func() {
			It("should add the custom policy alongside the defaults", func() {
				setSchedulerMode(false, false, "Hub")

				obj, err := sharedClients.Dynamic.Resource(tektonConfigGVR).
					Get(context.TODO(), "config", metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred(), "failed to read TektonConfig")
				policies, _, err := unstructured.NestedMap(obj.Object,
					"spec", "networkPolicy", "policies")
				Expect(err).NotTo(HaveOccurred(), "failed to read TektonConfig policies")
				_, collision := policies[multiclusterCustomNP]
				Expect(collision).To(BeFalse(),
					"custom policy %q already exists, refusing to overwrite it", multiclusterCustomNP)
				origNP := originalNetworkPolicyJSON()

				By("Adding a custom NetworkPolicy")
				patchData := fmt.Sprintf(
					`{"spec":{"networkPolicy":{"policies":{"%s":{"podSelector":{},"policyTypes":["Ingress","Egress"]}}}}}`,
					multiclusterCustomNP)
				cmd.MustSucceed("oc", "patch", "TektonConfig", "config", "--type=merge", "-p", patchData)
				// Dropping the policies key does not delete the NetworkPolicy, so the custom
				// set has to be torn down with networkPolicy.disabled before the key goes.
				DeferCleanup(func() {
					setNetworkPolicyOnTektonConfig(true)
					assertNetworkPolicyPresence(multiclusterCustomNP, config.TargetNamespace, false)
					restoreNetworkPolicyJSON(origNP)
					operator.EnsureTektonConfigStatusInstalled(sharedClients.TektonConfig(), store.GetCRNames())
					assertNetworkPolicyPresence(multiclusterCustomNP, config.TargetNamespace, false)
				})
				operator.EnsureTektonConfigStatusInstalled(sharedClients.TektonConfig(), store.GetCRNames())

				assertNetworkPolicyPresence(multiclusterCustomNP, config.TargetNamespace, true)
				assertMulticlusterNPs(true, "scheduler", "proxy-aae", "syncer-service")

				By("Confirming TektonConfig propagated the policy onto the proxy component")
				// TektonConfig copies spec.networkPolicy onto the component CR on its next
				// reconcile, so this is eventually consistent rather than immediate.
				Eventually(func(g Gomega) {
					propagated := cmd.MustSucceed("oc", "get",
						"tektonmulticlusterproxyaae.operator.tekton.dev/multicluster-proxy-aae",
						"-o", "jsonpath={.spec.networkPolicy.policies."+multiclusterCustomNP+"}").Stdout()
					g.Expect(propagated).NotTo(BeEmpty())
				}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).
					Should(Succeed(), "TektonConfig did not propagate the custom policy onto TektonMulticlusterProxyAAE")

			})
		})
	})
