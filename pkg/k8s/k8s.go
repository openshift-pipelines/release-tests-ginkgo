package k8s

import (
	"context"
	"strings"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
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
