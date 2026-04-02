package operator

import (
	"context"
	"fmt"
	"log"

	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	triggerv1alpha1 "github.com/tektoncd/operator/pkg/client/clientset/versioned/typed/operator/v1alpha1"
	"github.com/tektoncd/operator/test/utils"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// EnsureTektonTriggerExists waits until a TektonTrigger CR with the given name exists.
func EnsureTektonTriggerExists(clients triggerv1alpha1.TektonTriggerInterface, names utils.ResourceNames) (*v1alpha1.TektonTrigger, error) {
	ks, err := clients.Get(context.TODO(), names.TektonTrigger, metav1.GetOptions{})
	if err == nil {
		return ks, nil
	}
	err = wait.PollUntilContextTimeout(context.TODO(), config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		ks, err = clients.Get(context.TODO(), names.TektonTrigger, metav1.GetOptions{})
		if err != nil {
			if apierrs.IsNotFound(err) {
				log.Printf("Waiting for availability of triggers cr [%s]\n", names.TektonTrigger)
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	return ks, err
}

// TektonTriggerCRDelete deletes the TektonTrigger CR and waits for removal.
func TektonTriggerCRDelete(cs *clients.Clients, crNames utils.ResourceNames) {
	err := cs.TektonTrigger().Delete(context.TODO(), crNames.TektonTrigger, metav1.DeleteOptions{})
	Expect(err).NotTo(HaveOccurred(), "TektonTrigger %q failed to delete", crNames.TektonTrigger)

	err = wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true, func(context.Context) (bool, error) {
		_, err := cs.TektonTrigger().Get(context.TODO(), crNames.TektonTrigger, metav1.GetOptions{})
		if apierrs.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
	Expect(err).NotTo(HaveOccurred(), "timed out waiting on TektonTrigger to delete")

	verifyNoTektonTriggerCR(cs)
}

func verifyNoTektonTriggerCR(cs *clients.Clients) {
	triggers, err := cs.TektonTrigger().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to list TektonTrigger CRs")
	Expect(triggers.Items).To(BeEmpty(), "expected no TektonTrigger CRs to exist")
}

// Ensure unused imports don't fire.
var _ = fmt.Sprintf
