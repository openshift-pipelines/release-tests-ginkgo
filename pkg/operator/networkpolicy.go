package operator

import (
	"context"
	"fmt"
	"log"

	. "github.com/onsi/gomega" //nolint:revive,staticcheck // dot import is idiomatic for Gomega

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/tektoncd/operator/test/utils"
)

// AssertNetworkPoliciesExist polls until every named NetworkPolicy exists in ns.
func AssertNetworkPoliciesExist(cs *clients.Clients, ns string, names ...string) {
	for _, name := range names {
		policyName := name
		err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true,
			func(context.Context) (bool, error) {
				log.Printf("Checking NetworkPolicy %s/%s exists\n", ns, policyName)
				_, getErr := cs.KubeClient.Kube.NetworkingV1().NetworkPolicies(ns).Get(
					context.TODO(), policyName, metav1.GetOptions{},
				)
				if getErr != nil {
					log.Printf("NetworkPolicy %s/%s not found yet: %v\n", ns, policyName, getErr)
					return false, nil
				}
				return true, nil
			},
		)
		Expect(err).NotTo(HaveOccurred(),
			"expected NetworkPolicy %q to exist in namespace %q", policyName, ns)
	}
}

// AssertNetworkPoliciesAbsent polls until none of the named NetworkPolicies exist in ns.
func AssertNetworkPoliciesAbsent(cs *clients.Clients, ns string, names ...string) {
	for _, name := range names {
		policyName := name
		err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true,
			func(context.Context) (bool, error) {
				log.Printf("Checking NetworkPolicy %s/%s is absent\n", ns, policyName)
				_, getErr := cs.KubeClient.Kube.NetworkingV1().NetworkPolicies(ns).Get(
					context.TODO(), policyName, metav1.GetOptions{},
				)
				if getErr != nil {
					return true, nil
				}
				log.Printf("NetworkPolicy %s/%s still present, waiting for deletion\n", ns, policyName)
				return false, nil
			},
		)
		Expect(err).NotTo(HaveOccurred(),
			"expected NetworkPolicy %q to be absent from namespace %q", policyName, ns)
	}
}

// PatchNetworkPolicyDisabled sets spec.networkPolicy.disabled on TektonConfig and waits for Ready.
func PatchNetworkPolicyDisabled(cs *clients.Clients, crNames utils.ResourceNames, disabled bool) {
	disabledStr := "false"
	if disabled {
		disabledStr = "true"
	}
	patch := fmt.Sprintf(`{"spec":{"networkPolicy":{"disabled":%s}}}`, disabledStr)
	log.Printf("Patching TektonConfig networkPolicy.disabled=%s\n", disabledStr)
	cmd.MustSucceed("oc", "patch", "TektonConfig", crNames.TektonConfig, "--type=merge", "-p", patch)
	EnsureTektonConfigStatusInstalled(cs.TektonConfig(), crNames)
}

// AssertPodsReady verifies that all pods with the given label selector are Running and Ready in ns.
func AssertPodsReady(cs *clients.Clients, ns, labelSelector string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true,
		func(context.Context) (bool, error) {
			pods, listErr := cs.KubeClient.Kube.CoreV1().Pods(ns).List(context.TODO(), metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			if listErr != nil {
				return false, listErr
			}
			if len(pods.Items) == 0 {
				log.Printf("No pods found for selector %q in %s, waiting\n", labelSelector, ns)
				return false, nil
			}
			for i := range pods.Items {
				pod := &pods.Items[i]
				if pod.Status.Phase != "Running" {
					log.Printf("Pod %s is in phase %s, waiting\n", pod.Name, pod.Status.Phase)
					return false, nil
				}
				for _, cond := range pod.Status.Conditions {
					if cond.Type == "Ready" && cond.Status != "True" {
						log.Printf("Pod %s not Ready yet\n", pod.Name)
						return false, nil
					}
				}
			}
			return true, nil
		},
	)
	Expect(err).NotTo(HaveOccurred(),
		"expected pods with selector %q to be Running and Ready in namespace %q", labelSelector, ns)
}
