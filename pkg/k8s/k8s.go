package k8s

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

// DeleteDeployment deletes a deployment in the specified namespace
func DeleteDeployment(c *clients.Clients, namespace, deploymentName string) error {
	kc := c.KubeClient.Kube
	deploymentsClient := kc.AppsV1().Deployments(namespace)
	return deploymentsClient.Delete(context.TODO(), deploymentName, metav1.DeleteOptions{})
}

// ValidateDeployments validates that a deployment exists and is ready
func ValidateDeployments(c *clients.Clients, namespace, deploymentName string) error {
	kc := c.KubeClient.Kube
	deploymentsClient := kc.AppsV1().Deployments(namespace)
	_, err := deploymentsClient.Get(context.TODO(), deploymentName, metav1.GetOptions{})
	return err
}

// Watch creates a watch interface for the specified resource
func Watch(ctx context.Context, gvr schema.GroupVersionResource, c *clients.Clients, namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	return c.Dynamic.Resource(gvr).Namespace(namespace).Watch(ctx, opts)
}

// GetWarningEvents retrieves warning events from the namespace
func GetWarningEvents(c *clients.Clients, namespace string) (string, error) {
	kc := c.KubeClient.Kube
	events, err := kc.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	var warnings []string
	for _, event := range events.Items {
		if event.Type == corev1.EventTypeWarning {
			warnings = append(warnings, fmt.Sprintf("%s: %s", event.Reason, event.Message))
		}
	}
	return strings.Join(warnings, "\n"), nil
}
