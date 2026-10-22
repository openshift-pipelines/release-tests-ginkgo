package tektonkueue

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var routingNamespaceGVR = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}

func (e *Environment) quiesceRoutingNamespaces(ctx context.Context) error {
	for _, cluster := range e.clusters() {
		resource := cluster.Clients.Dynamic.Resource(routingNamespaceGVR)
		current, err := resource.Get(ctx, e.Namespace, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		uid, owned := e.routingNamespaceUIDs[cluster.Name]
		if !owned || !e.owns(current.GetLabels()) || current.GetUID() != uid {
			return fmt.Errorf("refusing to delete replacement routing namespace %s on %s", e.Namespace, cluster.Name)
		}
		if err := ignoreNotFound(resource.Delete(ctx, e.Namespace, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: &uid},
		})); err != nil {
			return err
		}
	}
	for _, cluster := range e.clusters() {
		if err := watchRoutingResource(ctx, cluster.Clients.Dynamic.Resource(routingNamespaceGVR), func(items []unstructured.Unstructured) (bool, error) {
			for _, item := range items {
				if item.GetName() == e.Namespace {
					if item.GetUID() != e.routingNamespaceUIDs[cluster.Name] {
						return false, fmt.Errorf("routing namespace replaced during teardown on %s", cluster.Name)
					}
					return false, nil
				}
			}
			return true, nil
		}); err != nil {
			return err
		}
	}
	return nil
}
