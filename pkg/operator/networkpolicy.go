package operator

import (
	"context"
	"fmt"
	"log"

	. "github.com/onsi/gomega" //nolint:revive,staticcheck // dot import is idiomatic for Gomega
	"github.com/tektoncd/operator/test/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// AssertNetworkPoliciesExist polls until every named NetworkPolicy exists
// in the given namespace. Pattern follows AssertRoleBindingPresent.
func AssertNetworkPoliciesExist(cs *clients.Clients, ns string, names ...string) {
	for _, name := range names {
		n := name // capture
		err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
			log.Printf("Verifying that NetworkPolicy %s exists in namespace %s\n", n, ns)
			_, err := cs.KubeClient.Kube.NetworkingV1().NetworkPolicies(ns).Get(context.TODO(), n, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			return true, nil
		})
		Expect(err).NotTo(HaveOccurred(),
			"expected NetworkPolicy %v present in namespace %v", n, ns)
	}
}

// AssertNetworkPoliciesAbsent polls until every named NetworkPolicy is
// gone from the given namespace. Pattern follows AssertRoleBindingNotPresent.
func AssertNetworkPoliciesAbsent(cs *clients.Clients, ns string, names ...string) {
	for _, name := range names {
		n := name // capture
		err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
			log.Printf("Verifying that NetworkPolicy %s doesn't exist in namespace %s\n", n, ns)
			npList, err := cs.KubeClient.Kube.NetworkingV1().NetworkPolicies(ns).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, item := range npList.Items {
				if item.Name == n {
					return false, nil
				}
			}
			return true, nil
		})
		Expect(err).NotTo(HaveOccurred(),
			"expected NetworkPolicy %v not present in namespace %v", n, ns)
	}
}

// PatchNetworkPolicyDisabled patches TektonConfig spec.networkPolicy.disabled
// and waits for the TektonConfig to reach installed status.
// Pattern follows patchTektonConfigParam in tests/operator/rbac_test.go.
func PatchNetworkPolicyDisabled(cs *clients.Clients, crNames utils.ResourceNames, disabled bool) {
	patchData := fmt.Sprintf(`{"spec":{"networkPolicy":{"disabled":%t}}}`, disabled)
	log.Printf("Patching TektonConfig networkPolicy.disabled=%t\n", disabled)
	cmd.MustSucceed("oc", "patch", "TektonConfig", crNames.TektonConfig, "--type=merge", "-p", patchData)
	EnsureTektonConfigStatusInstalled(cs.TektonConfig(), crNames)
}

// AssertPodsReady polls until at least one pod matching the label selector
// in the given namespace is Running and all its containers are Ready.
func AssertPodsReady(cs *clients.Clients, ns, labelSelector string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying pods with selector %q are Ready in namespace %s\n", labelSelector, ns)
		pods, err := cs.KubeClient.Kube.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return false, err
		}
		if len(pods.Items) == 0 {
			return false, nil
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != "Running" {
				return false, nil
			}
			for _, cs := range pod.Status.ContainerStatuses {
				if !cs.Ready {
					return false, nil
				}
			}
		}
		return true, nil
	})
	Expect(err).NotTo(HaveOccurred(),
		"expected pods with selector %q to be Running and Ready in namespace %v", labelSelector, ns)
}

// AssertTektonConfigReady waits for the TektonConfig CR to report Ready.
func AssertTektonConfigReady(cs *clients.Clients, crNames utils.ResourceNames) {
	AssertTektonConfigCRReadyStatus(cs, crNames)
}

// RestoreNetworkPolicyDisabled restores networkPolicy.disabled to false
// and waits for TektonConfig to be installed. Suitable for DeferCleanup.
func RestoreNetworkPolicyDisabled(cs *clients.Clients) {
	crNames := store.GetCRNames()
	log.Println("Restoring TektonConfig networkPolicy.disabled=false")
	PatchNetworkPolicyDisabled(cs, crNames, false)
}
