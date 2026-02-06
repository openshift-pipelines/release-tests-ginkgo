package k8s

import (
	"context"
	"fmt"
	"log"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/clients"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func ValidateDeployments(clients *clients.Clients, namespace, name string) {
	err := wait.PollUntilContextTimeout(context.Background(), 10*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		deployment, err := clients.KubeClient.Kube.AppsV1().Deployments(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				log.Printf("deployment %s not found, waiting...", name)
				return false, nil
			}
			return false, err
		}
		if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		Fail(fmt.Sprintf("Deployment %s in namespace %s is not ready: %v", name, namespace, err))
	}
}

func DeleteDeployment(c *clients.Clients, namespace, name string) error {
	if c == nil || c.KubeClient == nil || c.KubeClient.Kube == nil {
		return fmt.Errorf("kubernetes client is not initialized")
	}
	deploymentsClient := c.KubeClient.Kube.AppsV1().Deployments(namespace)
	err := deploymentsClient.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	return nil
}
