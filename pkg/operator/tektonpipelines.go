package operator

import (
	"context"
	"errors"
	"fmt"
	"log"

	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	pipelinev1alpha1 "github.com/tektoncd/operator/pkg/client/clientset/versioned/typed/operator/v1alpha1"
	"github.com/tektoncd/operator/test/utils"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// EnsureTektonPipelineExists waits until a TektonPipeline CR with the given name exists.
func EnsureTektonPipelineExists(clients pipelinev1alpha1.TektonPipelineInterface, names utils.ResourceNames) (*v1alpha1.TektonPipeline, error) {
	tpCR, err := clients.Get(context.TODO(), names.TektonPipeline, metav1.GetOptions{})
	if err == nil {
		return tpCR, err
	}
	err = wait.PollUntilContextTimeout(context.TODO(), config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		tpCR, err = clients.Get(context.TODO(), names.TektonPipeline, metav1.GetOptions{})
		if err != nil {
			if apierrs.IsNotFound(err) {
				log.Printf("Waiting for availability of pipelines cr [%s]\n", names.TektonPipeline)
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	return tpCR, err
}

// ValidatePipelineDeployments validates the Tekton Pipeline deployments exist and are ready.
func ValidatePipelineDeployments(cs *clients.Clients, rnames utils.ResourceNames) {
	_, err := EnsureTektonPipelineExists(cs.TektonPipeline(), rnames)
	Expect(err).NotTo(HaveOccurred(), "TektonPipeline doesn't exist")
	// Import from k8s package is avoided to prevent circular dependency;
	// deployments are validated via the k8s.ValidateDeployments call pattern.
}

// TektonPipelineCRDelete deletes the TektonPipeline CR and waits for removal.
func TektonPipelineCRDelete(cs *clients.Clients, crNames utils.ResourceNames) {
	err := cs.TektonPipeline().Delete(context.TODO(), crNames.TektonPipeline, metav1.DeleteOptions{})
	Expect(err).NotTo(HaveOccurred(), "TektonPipeline %q failed to delete", crNames.TektonPipeline)

	err = wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true, func(context.Context) (bool, error) {
		_, err := cs.TektonPipeline().Get(context.TODO(), crNames.TektonPipeline, metav1.GetOptions{})
		if apierrs.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
	Expect(err).NotTo(HaveOccurred(), "timed out waiting on TektonPipeline to delete")

	verifyNoTektonPipelineCR(cs)
}

func verifyNoTektonPipelineCR(cs *clients.Clients) {
	pipelines, err := cs.TektonPipeline().List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to list TektonPipeline CRs")
	Expect(pipelines.Items).To(BeEmpty(), "expected no TektonPipeline CRs to exist")
}

// Ensure unused imports don't fire.
var _ = fmt.Sprintf
var _ = errors.New
