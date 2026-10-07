// Package multicluster prepares a single cluster to act as a multi-cluster Hub
// so that NetworkPolicy behavior of the hub-only components (TektonScheduler,
// SyncerService, TektonMulticlusterProxyAAE) can be tested without a second
// cluster. The cluster is registered as its own MultiKueue worker because
// proxy-aae serves /ready only once a worker exists; until then its deployment
// never becomes available and the operator never reaches NetworkPolicy reconcile.
package multicluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

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

	operatorv1alpha1 "github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	olmpkg "github.com/openshift-pipelines/release-tests-ginkgo/pkg/olm"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

var (
	kueueCRGVR = schema.GroupVersionResource{
		Group: "kueue.openshift.io", Version: "v1", Resource: "kueues",
	}
	multiKueueClusterGVR = schema.GroupVersionResource{
		Group: "kueue.x-k8s.io", Version: "v1beta2", Resource: "multikueueclusters",
	}
	tektonConfigGVR = schema.GroupVersionResource{
		Group: "operator.tekton.dev", Version: "v1alpha1", Resource: "tektonconfigs",
	}
)

// ErrOLMMissing reports that OLM is not installed, so operators cannot be
// installed. Callers should skip rather than fail in that case.
var ErrOLMMissing = errors.New("OLM API not available")

// PoliciesByComponent lists the NetworkPolicies each multicluster component owns.
// It stays separate from the single-cluster map because these CRs only exist
// while the scheduler runs in multi-cluster mode.
var PoliciesByComponent = map[string][]string{
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

// HubOnlyComponents are deployed only while multi-cluster-role is Hub.
var HubOnlyComponents = []string{"proxy-aae", "syncer-service"}

// Environment owns every resource one Hub-mode test run creates. The run prefix
// makes names unique across runs, and cleanup re-reads each object and requires
// both the label and the recorded UID before deleting it.
type Environment struct {
	clients *clients.Clients

	prefix     string
	workerName string

	secretUID  types.UID
	clusterUID types.UID
	kueueUID   types.UID
	kueueOwned bool

	origScheduler   *operatorv1alpha1.Scheduler
	origNPJSON      string
	origMAGExists   bool
	origMAGDisabled string
}

// NewEnvironment returns an Environment with a unique identity for this run.
func NewEnvironment(cs *clients.Clients) *Environment {
	prefix := fmt.Sprintf("rtg-np-%x", time.Now().UnixNano())
	return &Environment{clients: cs, prefix: prefix, workerName: prefix + "-self"}
}

// labels marks resources this run created so cleanup never removes foreign ones.
func (e *Environment) labels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "release-tests-ginkgo",
		"app.kubernetes.io/instance":   e.prefix,
	}
}

func (e *Environment) owns(labels map[string]string) bool {
	return labels["app.kubernetes.io/managed-by"] == "release-tests-ginkgo" &&
		labels["app.kubernetes.io/instance"] == e.prefix && e.prefix != ""
}

// Setup installs the Kueue and cert-manager prerequisites, snapshots the exact
// TektonConfig state, then configures Kueue and registers the self worker.
func (e *Environment) Setup(ctx context.Context) error {
	if _, err := e.clients.KubeClient.Kube.Discovery().
		ServerResourcesForGroupVersion("operators.coreos.com/v1alpha1"); err != nil {
		return fmt.Errorf("install prerequisites: %w: %w", ErrOLMMissing, err)
	}

	controllerClient, err := e.clients.NewClientFromKubeconfig(
		config.Flags.Kubeconfig, config.Flags.Cluster, config.Flags.Context)
	if err != nil {
		return fmt.Errorf("create controller-runtime client: %w", err)
	}
	bootstrap := &olmpkg.ClusterBootstrap{Client: controllerClient}
	if err := bootstrap.EnsurePrerequisites(ctx,
		e.clients.KubeClient.Kube.Discovery(), olmpkg.MulticlusterPrerequisites()); err != nil {
		return fmt.Errorf("install multicluster prerequisites: %w", err)
	}

	if err := e.snapshotTektonConfig(ctx); err != nil {
		return err
	}
	if err := e.ensureKueueCR(ctx); err != nil {
		return err
	}
	return e.ensureSelfWorkerCluster(ctx)
}

// Cleanup deletes every resource this run created and restores the exact
// pre-test TektonConfig state.
func (e *Environment) Cleanup(ctx context.Context) error {
	return errors.Join(e.deleteOwnedResources(ctx), e.restoreTektonConfig(ctx))
}

// SetSchedulerMode patches the TektonConfig scheduler fields that gate which
// multicluster components, and therefore which NetworkPolicies, the operator
// maintains. It waits for the operator to settle before returning.
func (e *Environment) SetSchedulerMode(disabled, multiClusterDisabled bool, role string) error {
	patch := fmt.Sprintf(
		`{"spec":{"scheduler":{"disabled":%t,"multi-cluster-disabled":%t,"multi-cluster-role":"%s"}}}`,
		disabled, multiClusterDisabled, role)
	if result := cmd.Run("oc", "patch", "TektonConfig", "config", "--type=merge", "-p", patch); result.ExitCode != 0 {
		return fmt.Errorf("patch scheduler mode disabled=%t multi-cluster-disabled=%t role=%q: %s",
			disabled, multiClusterDisabled, role, result.Stderr())
	}
	log.Printf("Patched scheduler: disabled=%t multi-cluster-disabled=%t role=%q",
		disabled, multiClusterDisabled, role)
	operator.EnsureTektonConfigStatusInstalled(e.clients.TektonConfig(), store.GetCRNames())
	return nil
}

// AssertPolicies polls until every NetworkPolicy of the named components is
// present or absent as requested.
func (e *Environment) AssertPolicies(ctx context.Context, shouldBePresent bool, components ...string) error {
	for _, component := range components {
		policies, ok := PoliciesByComponent[component]
		if !ok {
			return fmt.Errorf("unknown multicluster component %q", component)
		}
		for _, name := range policies {
			if err := e.assertPolicy(ctx, name, shouldBePresent); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Environment) assertPolicy(ctx context.Context, name string, shouldBePresent bool) error {
	policies := e.clients.KubeClient.Kube.NetworkingV1().NetworkPolicies(config.TargetNamespace)
	return wait.PollUntilContextTimeout(ctx, config.APIRetry, config.APITimeout, true,
		func(ctx context.Context) (bool, error) {
			_, err := policies.Get(ctx, name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return !shouldBePresent, nil
			}
			if err != nil {
				return false, err
			}
			return shouldBePresent, nil
		})
}

// AssertCustomPolicyAbsent polls until the named NetworkPolicy is gone.
func (e *Environment) AssertCustomPolicyAbsent(ctx context.Context, name string) error {
	return e.assertPolicy(ctx, name, false)
}

// EnsureCustomPolicy adds a custom policy entry to TektonConfig and waits until
// the NetworkPolicy exists and TektonConfig has propagated the entry onto the
// proxy component. It returns the original spec.networkPolicy JSON so the caller
// can restore it exactly. It refuses when the name already exists.
func (e *Environment) EnsureCustomPolicy(ctx context.Context, name string) (string, error) {
	policies, err := e.policyMap(ctx)
	if err != nil {
		return "", err
	}
	if _, collision := policies[name]; collision {
		return "", fmt.Errorf("custom policy %q already exists, refusing to overwrite it", name)
	}
	origNP, err := e.networkPolicyJSON()
	if err != nil {
		return "", err
	}

	patch := fmt.Sprintf(
		`{"spec":{"networkPolicy":{"policies":{"%s":{"podSelector":{},"policyTypes":["Ingress","Egress"]}}}}}`,
		name)
	if result := cmd.Run("oc", "patch", "TektonConfig", "config", "--type=merge", "-p", patch); result.ExitCode != 0 {
		return "", fmt.Errorf("add custom policy %q: %s", name, result.Stderr())
	}
	operator.EnsureTektonConfigStatusInstalled(e.clients.TektonConfig(), store.GetCRNames())

	if err := e.assertPolicy(ctx, name, true); err != nil {
		return "", fmt.Errorf("custom policy %q never appeared: %w", name, err)
	}
	if err := e.AssertPolicies(ctx, true, "scheduler", "proxy-aae", "syncer-service"); err != nil {
		return "", err
	}

	// TektonConfig copies spec.networkPolicy onto the component CR on its next
	// reconcile, so propagation is eventually consistent rather than immediate.
	proxy := e.clients.Dynamic.Resource(schema.GroupVersionResource{
		Group: "operator.tekton.dev", Version: "v1alpha1", Resource: "tektonmulticlusterproxyaaes",
	})
	err = wait.PollUntilContextTimeout(ctx, config.APIRetry, config.APITimeout, true,
		func(ctx context.Context) (bool, error) {
			current, getErr := proxy.Get(ctx, "multicluster-proxy-aae", metav1.GetOptions{})
			if getErr != nil {
				return false, nil
			}
			entry, found, _ := unstructured.NestedMap(current.Object, "spec", "networkPolicy", "policies", name)
			return found && len(entry) != 0, nil
		})
	if err != nil {
		return "", fmt.Errorf("TektonConfig did not propagate custom policy %q onto TektonMulticlusterProxyAAE: %w", name, err)
	}
	return origNP, nil
}

// RemoveCustomPolicyAndRestore tears down the custom policy without losing
// pre-existing configuration. Dropping the policies key does not delete the
// NetworkPolicy object, so the custom set is disabled first and the removal is
// observed before the original configuration is restored exactly.
func (e *Environment) RemoveCustomPolicyAndRestore(ctx context.Context, name, origNP string) error {
	if result := cmd.Run("oc", "patch", "TektonConfig", "config", "--type=merge",
		"-p", `{"spec":{"networkPolicy":{"disabled":true}}}`); result.ExitCode != 0 {
		return fmt.Errorf("disable NetworkPolicy: %s", result.Stderr())
	}
	operator.EnsureTektonConfigStatusInstalled(e.clients.TektonConfig(), store.GetCRNames())
	if err := e.AssertCustomPolicyAbsent(ctx, name); err != nil {
		return err
	}
	if err := e.restoreNetworkPolicyJSON(origNP); err != nil {
		return err
	}
	operator.EnsureTektonConfigStatusInstalled(e.clients.TektonConfig(), store.GetCRNames())
	return e.AssertCustomPolicyAbsent(ctx, name)
}

// snapshotTektonConfig records the exact scheduler object and the raw
// spec.networkPolicy JSON. The pinned operator client predates NetworkPolicy,
// so the latter stays untyped while the scheduler uses the typed client.
func (e *Environment) snapshotTektonConfig(ctx context.Context) error {
	tc, err := e.clients.TektonConfig().Get(ctx, "config", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read TektonConfig for snapshot: %w", err)
	}
	e.origScheduler = tc.Spec.Scheduler.DeepCopy()
	e.origNPJSON, err = e.networkPolicyJSON()
	if err != nil {
		return err
	}

	result := cmd.Run("oc", "get", "manualapprovalgate", "manual-approval-gate",
		"-o", "jsonpath={.spec.networkPolicy.disabled}")
	if result.ExitCode == 0 {
		e.origMAGExists = true
		e.origMAGDisabled = result.Stdout()
	}
	return nil
}

func (e *Environment) networkPolicyJSON() (string, error) {
	obj, err := e.clients.Dynamic.Resource(tektonConfigGVR).Get(context.TODO(), "config", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("read TektonConfig: %w", err)
	}
	np, found, err := unstructured.NestedMap(obj.Object, "spec", "networkPolicy")
	if err != nil {
		return "", fmt.Errorf("read TektonConfig networkPolicy: %w", err)
	}
	if !found || len(np) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(np)
	if err != nil {
		return "", fmt.Errorf("encode TektonConfig networkPolicy: %w", err)
	}
	return string(raw), nil
}

// restoreNetworkPolicyJSON writes back a previously snapshotted spec.networkPolicy.
// An empty snapshot means the field was absent and is removed.
func (e *Environment) restoreNetworkPolicyJSON(snapshot string) error {
	if snapshot == "" {
		res := cmd.Run("oc", "patch", "TektonConfig", "config", "--type=json",
			"-p", `[{"op":"remove","path":"/spec/networkPolicy"}]`)
		if res.ExitCode != 0 {
			return fmt.Errorf("remove spec.networkPolicy: %s", res.Stderr())
		}
		return nil
	}
	res := cmd.Run("oc", "patch", "TektonConfig", "config", "--type=merge",
		"-p", fmt.Sprintf(`{"spec":{"networkPolicy":%s}}`, snapshot))
	if res.ExitCode != 0 {
		return fmt.Errorf("restore spec.networkPolicy: %s", res.Stderr())
	}
	return nil
}

// restoreTektonConfig writes back the exact snapshotted scheduler object and
// NetworkPolicy JSON and waits for the operator to settle.
func (e *Environment) restoreTektonConfig(ctx context.Context) error {
	if e.origScheduler == nil {
		return fmt.Errorf("no TektonConfig snapshot to restore")
	}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest, getErr := e.clients.TektonConfig().Get(ctx, "config", metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		latest.Spec.Scheduler = *e.origScheduler.DeepCopy()
		_, updateErr := e.clients.TektonConfig().Update(ctx, latest, metav1.UpdateOptions{})
		return updateErr
	})
	if err != nil {
		return fmt.Errorf("restore TektonConfig scheduler: %w", err)
	}
	if err := e.restoreNetworkPolicyJSON(e.origNPJSON); err != nil {
		return err
	}
	operator.EnsureTektonConfigStatusInstalled(e.clients.TektonConfig(), store.GetCRNames())

	if e.origMAGExists {
		disabled := e.origMAGDisabled
		if disabled == "" {
			disabled = "false"
		}
		if result := cmd.Run("oc", "patch", "manualapprovalgate", "manual-approval-gate",
			"--type=merge", "-p", fmt.Sprintf(`{"spec":{"networkPolicy":{"disabled":%s}}}`, disabled)); result.ExitCode != 0 {
			return fmt.Errorf("restore ManualApprovalGate networkPolicy: %s", result.Stderr())
		}
	}
	return nil
}

// policyMap returns the current spec.networkPolicy.policies entries.
func (e *Environment) policyMap(ctx context.Context) (map[string]any, error) {
	obj, err := e.clients.Dynamic.Resource(tektonConfigGVR).Get(ctx, "config", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read TektonConfig: %w", err)
	}
	policies, _, err := unstructured.NestedMap(obj.Object, "spec", "networkPolicy", "policies")
	if err != nil {
		return nil, fmt.Errorf("read TektonConfig policies: %w", err)
	}
	if policies == nil {
		return map[string]any{}, nil
	}
	return policies, nil
}

// ensureKueueCR creates a minimal Kueue CR only when none exists. This package
// never dispatches a PipelineRun, so no external framework is configured; an
// existing Kueue CR is left untouched.
func (e *Environment) ensureKueueCR(ctx context.Context) error {
	resource := e.clients.Dynamic.Resource(kueueCRGVR)
	if _, err := resource.Get(ctx, "cluster", metav1.GetOptions{}); err == nil {
		log.Println("Kueue CR already exists, leaving it untouched")
	} else {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("read Kueue CR: %w", err)
		}
		kueue := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "kueue.openshift.io/v1",
			"kind":       "Kueue",
			"metadata":   map[string]any{"name": "cluster", "labels": e.labels()},
			"spec": map[string]any{
				"config":          map[string]any{"integrations": map[string]any{"frameworks": []any{"Deployment"}}},
				"managementState": "Managed",
			},
		}}
		created, err := resource.Create(ctx, kueue, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create Kueue CR: %w", err)
		}
		e.kueueUID = created.GetUID()
		e.kueueOwned = true
	}

	if err := wait.PollUntilContextTimeout(ctx, config.APIRetry, config.APITimeout, true,
		func(ctx context.Context) (bool, error) {
			current, err := resource.Get(ctx, "cluster", metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			conditions, _, _ := unstructured.NestedSlice(current.Object, "status", "conditions")
			for _, item := range conditions {
				condition, ok := item.(map[string]any)
				if ok && condition["type"] == "Available" && condition["status"] == "True" {
					return true, nil
				}
			}
			return false, nil
		}); err != nil {
		return fmt.Errorf("kueue never became Available: %w", err)
	}

	// The kueue.x-k8s.io CRDs are installed by the Kueue operand and lag the CR
	// reaching Available, so wait for them before creating the MultiKueueCluster.
	return wait.PollUntilContextTimeout(ctx, config.APIRetry, config.APITimeout, true,
		func(ctx context.Context) (bool, error) {
			_, err := e.clients.Dynamic.Resource(multiKueueClusterGVR).List(ctx, metav1.ListOptions{})
			return err == nil, nil
		})
}

// ensureWorkerNamespace creates the run-owned namespace hosting the worker
// identity. It must not be an openshift-* namespace: ROSA's
// clusterrolebindings-validation webhook refuses to delete ClusterRoleBindings
// whose subjects live there, which would break cleanup.
func (e *Environment) ensureWorkerNamespace(ctx context.Context) (string, error) {
	ns := e.workerName
	kube := e.clients.KubeClient.Kube
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: e.labels()},
	}
	created, err := kube.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	if err == nil {
		_ = created
		return ns, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("create worker namespace: %w", err)
	}
	existing, getErr := kube.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if getErr != nil {
		return "", fmt.Errorf("read the existing worker namespace: %w", getErr)
	}
	if !e.owns(existing.Labels) {
		return "", fmt.Errorf("namespace %s already exists and was not created by this run", ns)
	}
	return ns, nil
}

// ensureWorkerServiceAccount creates a dedicated ServiceAccount with only the
// permissions a MultiKueue worker needs, and returns a bounded token for it.
func (e *Environment) ensureWorkerServiceAccount(ctx context.Context, ns string) (string, error) {
	kube := e.clients.KubeClient.Kube
	saName := e.workerName

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: ns, Labels: e.labels()},
	}
	if _, err := kube.CoreV1().ServiceAccounts(ns).Create(ctx, sa, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return "", fmt.Errorf("create worker ServiceAccount: %w", err)
		}
		existing, getErr := kube.CoreV1().ServiceAccounts(ns).Get(ctx, saName, metav1.GetOptions{})
		if getErr != nil {
			return "", fmt.Errorf("read existing worker ServiceAccount: %w", getErr)
		}
		if !e.owns(existing.Labels) {
			return "", fmt.Errorf("ServiceAccount %s already exists and was not created by this run", saName)
		}
	}

	// Kueue's Active check lists cluster-wide resources, so the worker needs
	// cluster-scoped read. This is deletable because the ServiceAccount lives in
	// the run-owned namespace rather than an openshift-* one.
	readRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Labels: e.labels()},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kueue.x-k8s.io"}, Resources: []string{"workloads"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"pipelineruns"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
	if _, err := kube.RbacV1().ClusterRoles().Create(ctx, readRole, metav1.CreateOptions{}); err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("create worker ClusterRole: %w", err)
	}
	readBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Labels: e.labels()},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: saName},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: saName, Namespace: ns}},
	}
	if _, err := kube.RbacV1().ClusterRoleBindings().Create(ctx, readBinding, metav1.CreateOptions{}); err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("create worker ClusterRoleBinding: %w", err)
	}

	writeRole := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: ns, Labels: e.labels()},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"kueue.x-k8s.io"}, Resources: []string{"workloads"}, Verbs: []string{"create", "delete", "get", "list", "patch", "update", "watch"}},
			{APIGroups: []string{"kueue.x-k8s.io"}, Resources: []string{"workloads/status"}, Verbs: []string{"get", "patch", "update"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"pipelineruns"}, Verbs: []string{"create", "delete", "get", "list", "patch", "update", "watch"}},
			{APIGroups: []string{"tekton.dev"}, Resources: []string{"pipelineruns/status"}, Verbs: []string{"get", "patch", "update"}},
		},
	}
	if _, err := kube.RbacV1().Roles(ns).Create(ctx, writeRole, metav1.CreateOptions{}); err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("create worker Role: %w", err)
	}
	writeBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: ns, Labels: e.labels()},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: saName},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: saName, Namespace: ns}},
	}
	if _, err := kube.RbacV1().RoleBindings(ns).Create(ctx, writeBinding, metav1.CreateOptions{}); err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("create worker RoleBinding: %w", err)
	}

	expiration := int64(3 * 60 * 60)
	token, err := kube.CoreV1().ServiceAccounts(ns).CreateToken(ctx, saName,
		&authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &expiration}},
		metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("mint worker token: %w", err)
	}
	if token.Status.Token == "" {
		return "", fmt.Errorf("worker token is empty")
	}
	return token.Status.Token, nil
}

func (e *Environment) deleteOwned(
	ctx context.Context,
	get func(context.Context) (metav1.Object, error),
	delete func(context.Context) error,
	wantUID types.UID,
	kind, name string,
) error {
	current, err := get(ctx)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s %s: %w", kind, name, err)
	}
	if !e.owns(current.GetLabels()) {
		return fmt.Errorf("refusing to delete %s %s not created by this run", kind, name)
	}
	if wantUID != "" && current.GetUID() != wantUID {
		return fmt.Errorf("refusing to delete replacement %s %s", kind, name)
	}
	if err := delete(ctx); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete %s %s: %w", kind, name, err)
	}
	return nil
}

// deleteOwnedRBAC removes cluster- and namespace-scoped worker RBAC by name,
// guarded by the ownership label. Names are unique per run, so no UID is needed.
func (e *Environment) deleteOwnedRBAC(ctx context.Context) error {
	kube := e.clients.KubeClient.Kube
	ns := e.workerName
	name := e.workerName

	// Dependents first: bindings before the roles and accounts they reference.
	var errs []error
	for _, binding := range []struct {
		get    func(context.Context) (metav1.Object, error)
		delete func(context.Context) error
		kind   string
	}{
		{
			get: func(ctx context.Context) (metav1.Object, error) {
				return kube.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
			},
			delete: func(ctx context.Context) error {
				return kube.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{})
			},
			kind: "ClusterRoleBinding",
		},
		{
			get: func(ctx context.Context) (metav1.Object, error) {
				return kube.RbacV1().RoleBindings(ns).Get(ctx, name, metav1.GetOptions{})
			},
			delete: func(ctx context.Context) error {
				return kube.RbacV1().RoleBindings(ns).Delete(ctx, name, metav1.DeleteOptions{})
			},
			kind: "RoleBinding",
		},
	} {
		if err := e.deleteOwned(ctx, binding.get, binding.delete, "", binding.kind, name); err != nil {
			errs = append(errs, err)
		}
	}
	for _, role := range []struct {
		get    func(context.Context) (metav1.Object, error)
		delete func(context.Context) error
		kind   string
	}{
		{
			get: func(ctx context.Context) (metav1.Object, error) {
				return kube.RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
			},
			delete: func(ctx context.Context) error {
				return kube.RbacV1().ClusterRoles().Delete(ctx, name, metav1.DeleteOptions{})
			},
			kind: "ClusterRole",
		},
		{
			get: func(ctx context.Context) (metav1.Object, error) {
				return kube.RbacV1().Roles(ns).Get(ctx, name, metav1.GetOptions{})
			},
			delete: func(ctx context.Context) error {
				return kube.RbacV1().Roles(ns).Delete(ctx, name, metav1.DeleteOptions{})
			},
			kind: "Role",
		},
		{
			get: func(ctx context.Context) (metav1.Object, error) {
				return kube.CoreV1().ServiceAccounts(ns).Get(ctx, name, metav1.GetOptions{})
			},
			delete: func(ctx context.Context) error {
				return kube.CoreV1().ServiceAccounts(ns).Delete(ctx, name, metav1.DeleteOptions{})
			},
			kind: "ServiceAccount",
		},
	} {
		if err := e.deleteOwned(ctx, role.get, role.delete, "", role.kind, name); err != nil {
			errs = append(errs, err)
		}
	}
	if err := e.deleteOwned(ctx,
		func(ctx context.Context) (metav1.Object, error) {
			return kube.CoreV1().Namespaces().Get(ctx, e.workerName, metav1.GetOptions{})
		},
		func(ctx context.Context) error {
			return kube.CoreV1().Namespaces().Delete(ctx, e.workerName, metav1.DeleteOptions{})
		},
		"", "Namespace", e.workerName,
	); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// deleteOwnedWorker removes the worker Secret, MultiKueueCluster, and Kueue CR
// created by this run, each guarded by label and UID.
func (e *Environment) deleteOwnedWorker(ctx context.Context) error {
	kueueNS := config.Flags.KueueOperatorNamespace
	secrets := e.clients.KubeClient.Kube.CoreV1().Secrets(kueueNS)
	errs := []error{e.deleteOwned(ctx,
		func(ctx context.Context) (metav1.Object, error) {
			return secrets.Get(ctx, e.workerName, metav1.GetOptions{})
		},
		func(ctx context.Context) error {
			return secrets.Delete(ctx, e.workerName, metav1.DeleteOptions{})
		},
		e.secretUID, "Secret", e.workerName,
	)}

	clusters := e.clients.Dynamic.Resource(multiKueueClusterGVR)
	errs = append(errs, e.deleteOwned(ctx,
		func(ctx context.Context) (metav1.Object, error) {
			return clusters.Get(ctx, e.workerName, metav1.GetOptions{})
		},
		func(ctx context.Context) error {
			return clusters.Delete(ctx, e.workerName, metav1.DeleteOptions{})
		},
		e.clusterUID, "MultiKueueCluster", e.workerName,
	))

	if e.kueueOwned {
		kueues := e.clients.Dynamic.Resource(kueueCRGVR)
		errs = append(errs, e.deleteOwned(ctx,
			func(ctx context.Context) (metav1.Object, error) {
				return kueues.Get(ctx, "cluster", metav1.GetOptions{})
			},
			func(ctx context.Context) error {
				return kueues.Delete(ctx, "cluster", metav1.DeleteOptions{})
			},
			e.kueueUID, "Kueue", "cluster",
		))
	}
	return errors.Join(errs...)
}

func (e *Environment) deleteOwnedResources(ctx context.Context) error {
	return errors.Join(e.deleteOwnedWorker(ctx), e.deleteOwnedRBAC(ctx))
}

// ensureSelfWorkerCluster registers the cluster as its own MultiKueue worker.
// proxy-aae reports ready only once a worker is registered, and the operator
// creates its NetworkPolicies only after the deployment is available.
func (e *Environment) ensureSelfWorkerCluster(ctx context.Context) error {
	// The worker is this same cluster, so Kueue connects to it from inside: the
	// in-cluster endpoint is routable from pods and is the endpoint kube-root-ca.crt
	// signs. The external API URL is neither on a managed cluster, where it resolves
	// to a public address pods cannot reach and is served by a public CA.
	const inClusterEndpoint = "https://kubernetes.default.svc:443"

	configMap, err := e.clients.KubeClient.Kube.CoreV1().
		ConfigMaps(config.Flags.KueueOperatorNamespace).
		Get(ctx, "kube-root-ca.crt", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read kube-root-ca.crt: %w", err)
	}
	ca := []byte(configMap.Data["ca.crt"])
	if len(ca) == 0 {
		return fmt.Errorf("kube-root-ca.crt has no ca.crt")
	}

	workerNS, err := e.ensureWorkerNamespace(ctx)
	if err != nil {
		return err
	}
	token, err := e.ensureWorkerServiceAccount(ctx, workerNS)
	if err != nil {
		return err
	}

	kubeconfig, err := clientcmd.Write(clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"self": {Server: inClusterEndpoint, CertificateAuthorityData: ca}},
		AuthInfos:      map[string]*clientcmdapi.AuthInfo{"self": {Token: token}},
		Contexts:       map[string]*clientcmdapi.Context{"self": {Cluster: "self", AuthInfo: "self"}},
		CurrentContext: "self",
	})
	if err != nil {
		return fmt.Errorf("serialize the self worker kubeconfig: %w", err)
	}

	secrets := e.clients.KubeClient.Kube.CoreV1().Secrets(config.Flags.KueueOperatorNamespace)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      e.workerName,
			Namespace: config.Flags.KueueOperatorNamespace,
			Labels:    e.labels(),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"kubeconfig": kubeconfig},
	}
	createdSecret, err := secrets.Create(ctx, secret, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := secrets.Get(ctx, e.workerName, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("read the existing worker secret: %w", getErr)
		}
		if !e.owns(existing.Labels) {
			return fmt.Errorf("secret %s already exists and was not created by this run", e.workerName)
		}
		// A leftover Secret from an interrupted run holds a stale kubeconfig, which
		// would leave the worker inactive. Refresh it rather than trusting it.
		existing.Data = secret.Data
		updated, err := secrets.Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("refresh the worker secret: %w", err)
		}
		e.secretUID = updated.GetUID()
	} else if err != nil {
		return fmt.Errorf("create the self worker secret: %w", err)
	} else {
		e.secretUID = createdSecret.GetUID()
	}

	clusters := e.clients.Dynamic.Resource(multiKueueClusterGVR)
	worker := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta2",
		"kind":       "MultiKueueCluster",
		"metadata":   map[string]any{"name": e.workerName, "labels": e.labels()},
		"spec": map[string]any{
			"clusterSource": map[string]any{
				"kubeConfig": map[string]any{"locationType": "Secret", "location": e.workerName},
			},
		},
	}}
	createdCluster, err := clusters.Create(ctx, worker, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := clusters.Get(ctx, e.workerName, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("read the existing MultiKueueCluster: %w", getErr)
		}
		if !e.owns(existing.GetLabels()) {
			return fmt.Errorf("MultiKueueCluster %s already exists and was not created by this run", e.workerName)
		}
		e.clusterUID = existing.GetUID()
	} else if err != nil {
		return fmt.Errorf("create the self MultiKueueCluster: %w", err)
	} else {
		e.clusterUID = createdCluster.GetUID()
	}

	return wait.PollUntilContextTimeout(ctx, config.APIRetry, config.APITimeout, true,
		func(ctx context.Context) (bool, error) {
			current, getErr := clusters.Get(ctx, e.workerName, metav1.GetOptions{})
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
}
