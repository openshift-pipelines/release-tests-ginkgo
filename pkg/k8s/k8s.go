package k8s

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
)

// GetWarningEvents returns warning events from the specified namespace as a single string.
func GetWarningEvents(c *clients.Clients, namespace string) (string, error) {
	var eventSlice []string
	events, err := c.KubeClient.Kube.CoreV1().Events(namespace).List(c.Ctx, metav1.ListOptions{FieldSelector: "type=Warning"})
	if err != nil {
		return "", err
	}
	for _, item := range events.Items {
		eventSlice = append(eventSlice, item.Message)
	}
	return strings.Join(eventSlice, "\n"), nil
}

// Watch func helps you to watch on dynamic resources
func Watch(ctx context.Context, gr schema.GroupVersionResource, clients *clients.Clients, ns string, op metav1.ListOptions) (watch.Interface, error) {
	gvr, err := GetGroupVersionResource(gr, clients.Tekton.Discovery())
	if err != nil {
		return nil, err
	}
	w, err := clients.Dynamic.Resource(*gvr).Namespace(ns).Watch(ctx, op)
	if err != nil {
		return nil, err
	}
	return w, nil
}

// Get retrieves a dynamic resource by name.
func Get(ctx context.Context, gr schema.GroupVersionResource, clients *clients.Clients, objname, ns string, op metav1.GetOptions) (*unstructured.Unstructured, error) {
	gvr, err := GetGroupVersionResource(gr, clients.Tekton.Discovery())
	if err != nil {
		return nil, err
	}

	obj, err := clients.Dynamic.Resource(*gvr).Namespace(ns).Get(ctx, objname, op)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

// GetGroupVersionResource resolves a GroupVersionResource using the discovery API.
func GetGroupVersionResource(gr schema.GroupVersionResource, discovery discovery.DiscoveryInterface) (*schema.GroupVersionResource, error) {
	apiGroupRes, err := restmapper.GetAPIGroupResources(discovery)
	if err != nil {
		return nil, err
	}
	rm := restmapper.NewDiscoveryRESTMapper(apiGroupRes)
	gvr, err := rm.ResourceFor(gr)
	if err != nil {
		return nil, err
	}
	return &gvr, nil
}

// WaitForDeployment waits until a deployment has the expected number of available replicas.
func WaitForDeployment(ctx context.Context, kc kubernetes.Interface, namespace, name string, replicas int, retryInterval, timeout time.Duration) error {
	err := wait.PollUntilContextTimeout(ctx, retryInterval, timeout, false, func(context.Context) (bool, error) {
		deployment, err := kc.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				log.Printf("Waiting for availability of %s deployment\n", name)
				return false, nil
			}
			return false, err
		}

		if int(deployment.Status.AvailableReplicas) == replicas && int(deployment.Status.UnavailableReplicas) == 0 {
			return true, nil
		}
		log.Printf("Waiting for full availability of deployment %s (%d/%d)\n", name, deployment.Status.AvailableReplicas, replicas)
		return false, nil
	})
	return err
}

// WaitForDeploymentDeletion checks to see if a given deployment is deleted.
// The function returns an error if the given deployment is not deleted within the timeout.
func WaitForDeploymentDeletion(cs *clients.Clients, namespace, name string) error {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		kc := cs.KubeClient.Kube
		_, err := kc.AppsV1().Deployments(namespace).Get(cs.Ctx, name, metav1.GetOptions{})
		if err != nil {
			if errors.IsGone(err) || errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		log.Printf("Waiting for deletion of %s deployment\n", name)
		return false, nil
	})
	return err
}

// ValidateDeployments waits for each named deployment to have at least 1 available replica.
func ValidateDeployments(cs *clients.Clients, namespace string, deployments ...string) {
	kc := cs.KubeClient.Kube
	for _, d := range deployments {
		err := WaitForDeployment(cs.Ctx, kc, namespace,
			d,
			1,
			config.APIRetry,
			config.APITimeout,
		)
		if err != nil {
			log.Printf("failed to validate deployment %s in namespace %s: %v", d, namespace, err)
		}
	}
}

// DeleteDeployment deletes a deployment by name and waits for it to be removed.
func DeleteDeployment(cs *clients.Clients, namespace, deploymentName string) error {
	kc := cs.KubeClient.Kube
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := kc.AppsV1().Deployments(namespace).Delete(ctx, deploymentName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			log.Printf("Deployment %s already deleted in namespace %s", deploymentName, namespace)
			return nil
		}
		return fmt.Errorf("failed to delete deployment %s in namespace %s: %v", deploymentName, namespace, err)
	}

	if delErr := WaitForDeploymentDeletion(cs, namespace, deploymentName); delErr != nil {
		return fmt.Errorf("deployment %s not fully deleted: %v", deploymentName, delErr)
	}
	return nil
}
