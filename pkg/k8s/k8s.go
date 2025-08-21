package k8s

import (
	"context"
	"fmt"

	"github.com/srivickynesh/release-tests-ginkgo/pkg/clients"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ValidateDeployments(clients *clients.Clients, namespace, name string) {
	// dummy function
}

func DeleteDeployment(c *clients.Clients, namespace, name string) error {
	if c == nil || c.KubeClient == nil || c.KubeClient.Kube == nil {
		return fmt.Errorf("Kubernetes client is not initialized")
	}
	deploymentsClient := c.KubeClient.Kube.AppsV1().Deployments(namespace)
	err := deploymentsClient.Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	return nil
}
