package tektonkueue

import (
	"context"
	"fmt"

	authorizationv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// EnablePACSecretSync grants only get/create/update Secrets in the test
// namespace. It is opt-in; ordinary worker setup still proves Secret denial.
func (e *Environment) EnablePACSecretSync(ctx context.Context) error {
	for _, spoke := range e.Spokes {
		name := e.workerName(spoke) + "-pac-secrets"
		if err := e.createRole(ctx, spoke, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: e.Namespace, Labels: e.ownedLabels()},
			Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get", "create", "update"}}},
		}); err != nil {
			return requestError(ctx, "create scoped PAC Secret role")
		}
		if err := e.createRoleBinding(ctx, spoke, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: e.Namespace, Labels: e.ownedLabels()},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: e.workerName(spoke), Namespace: e.Namespace}},
			RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: name},
		}); err != nil {
			return requestError(ctx, "bind scoped PAC Secret role")
		}
		credential, err := e.Hub.Clients.KubeClient.Kube.CoreV1().Secrets(kueueNamespace).Get(ctx, e.workerName(spoke), metav1.GetOptions{})
		if err != nil {
			return requestError(ctx, "read worker credential")
		}
		cfg, err := clientcmd.RESTConfigFromKubeConfig(credential.Data["kubeconfig"])
		if err != nil {
			return fmt.Errorf("invalid worker credential")
		}
		worker, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return fmt.Errorf("invalid worker client")
		}
		if err := verifyPACSecretScope(ctx, worker, e.Namespace); err != nil {
			return err
		}
	}
	e.pacSecretSync = true
	return nil
}

func verifyPACSecretScope(ctx context.Context, worker kubernetes.Interface, namespace string) error {
	for _, ns := range []string{namespace, "default"} {
		for _, verb := range []string{"get", "create", "update", "list", "watch", "delete", "patch"} {
			review, err := worker.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{
				Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Namespace: ns, Verb: verb, Resource: "secrets"}},
			}, metav1.CreateOptions{})
			if err != nil {
				return requestError(ctx, "verify PAC Secret permission")
			}
			want := ns == namespace && (verb == "get" || verb == "create" || verb == "update")
			if review.Status.Allowed != want {
				return fmt.Errorf("PAC Secret permission scope mismatch for %s in namespace %s", verb, ns)
			}
		}
	}
	return nil
}
