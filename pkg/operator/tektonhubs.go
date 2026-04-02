package operator

import (
	"context"
	"log"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	hubv1alpha "github.com/tektoncd/operator/pkg/client/clientset/versioned/typed/operator/v1alpha1"
	"github.com/tektoncd/operator/test/utils"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// EnsureTektonHubsExists waits until a TektonHub CR with the given name exists.
func EnsureTektonHubsExists(clients hubv1alpha.TektonHubInterface, names utils.ResourceNames) (*v1alpha1.TektonHub, error) {
	ks, err := clients.Get(context.TODO(), names.TektonHub, metav1.GetOptions{})
	if err == nil {
		return ks, nil
	}
	err = wait.PollUntilContextTimeout(context.TODO(), config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		ks, err = clients.Get(context.TODO(), names.TektonHub, metav1.GetOptions{})
		if err != nil {
			if apierrs.IsNotFound(err) {
				log.Printf("Waiting for availability of hub cr [%s]\n", names.TektonHub)
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	return ks, err
}
