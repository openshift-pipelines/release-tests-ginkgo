package operator

import (
	"context"
	"fmt"
	"log"

	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	scc "github.com/openshift/client-go/security/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// AssertServiceAccountPresent verifies a service account exists in the given namespace.
func AssertServiceAccountPresent(cs *clients.Clients, ns, targetSA string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that service account %s exists\n", targetSA)
		saList, err := cs.KubeClient.Kube.CoreV1().ServiceAccounts(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, item := range saList.Items {
			if item.Name == targetSA {
				return true, nil
			}
		}
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected Service account %v present in namespace %v", targetSA, ns)
}

// AssertRoleBindingPresent verifies a role binding exists in the given namespace.
func AssertRoleBindingPresent(cs *clients.Clients, ns, roleBindingName string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that role binding %s exists\n", roleBindingName)
		rbList, err := cs.KubeClient.Kube.RbacV1().RoleBindings(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, item := range rbList.Items {
			if item.Name == roleBindingName {
				return true, nil
			}
		}
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected RoleBinding %v present in namespace %v", roleBindingName, ns)
}

// AssertRoleBindingNotPresent verifies a role binding does not exist in the given namespace.
func AssertRoleBindingNotPresent(cs *clients.Clients, ns, roleBindingName string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that role binding %s doesn't exist\n", roleBindingName)
		rbList, err := cs.KubeClient.Kube.RbacV1().RoleBindings(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, item := range rbList.Items {
			if item.Name == roleBindingName {
				return false, nil
			}
		}
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected RoleBinding %v not present in namespace %v", roleBindingName, ns)
}

// AssertConfigMapPresent verifies a config map exists in the given namespace.
func AssertConfigMapPresent(cs *clients.Clients, ns, configMapName string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that config map %s exists\n", configMapName)
		cmList, err := cs.KubeClient.Kube.CoreV1().ConfigMaps(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, item := range cmList.Items {
			if item.Name == configMapName {
				return true, nil
			}
		}
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected ConfigMap %v present in namespace %v", configMapName, ns)
}

// AssertClusterRolePresent verifies a cluster role exists.
func AssertClusterRolePresent(cs *clients.Clients, clusterRoleName string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that cluster role %s exists\n", clusterRoleName)
		crList, err := cs.KubeClient.Kube.RbacV1().ClusterRoles().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, item := range crList.Items {
			if item.Name == clusterRoleName {
				return true, nil
			}
		}
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected ClusterRole %v present", clusterRoleName)
}

// AssertClusterRoleNotPresent verifies a cluster role does not exist.
func AssertClusterRoleNotPresent(cs *clients.Clients, clusterRoleName string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that cluster role %s doesn't exist\n", clusterRoleName)
		crList, err := cs.KubeClient.Kube.RbacV1().ClusterRoles().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, item := range crList.Items {
			if item.Name == clusterRoleName {
				return false, nil
			}
		}
		return true, err
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected ClusterRole %v not present", clusterRoleName)
}

// AssertSCCPresent verifies a security context constraint exists.
func AssertSCCPresent(cs *clients.Clients, sccName string) {
	s := scc.NewForConfigOrDie(cs.KubeConfig)
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that security context constraint %s exists\n", sccName)
		sccList, err := s.SecurityV1().SecurityContextConstraints().List(cs.Ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, item := range sccList.Items {
			if item.Name == sccName {
				return true, nil
			}
		}
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected SCC %q present", sccName)
}

// AssertSCCNotPresent verifies a security context constraint does not exist.
func AssertSCCNotPresent(cs *clients.Clients, sccName string) {
	s := scc.NewForConfigOrDie(cs.KubeConfig)
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that security context constraint %s doesn't exist\n", sccName)
		sccList, err := s.SecurityV1().SecurityContextConstraints().List(cs.Ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, item := range sccList.Items {
			if item.Name == sccName {
				return false, nil
			}
		}
		return true, err
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected SCC %q not present", sccName)
}

// VerifyRolesArePresent verifies a role exists in the given namespace.
func VerifyRolesArePresent(cs *clients.Clients, role, namespace string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that role %s exists in namespace %s\n", role, namespace)
		_, err := cs.KubeClient.Kube.RbacV1().Roles(namespace).Get(context.TODO(), role, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"failed to verify role %q present in namespace %q", role, namespace)
}

// Ensure unused imports don't fire.
var _ = fmt.Sprintf
