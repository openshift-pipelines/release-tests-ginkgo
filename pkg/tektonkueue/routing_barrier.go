package tektonkueue

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
)

// A LIST followed by WATCH at that exact resourceVersion cannot miss a short
// create/finish/delete between the calls. Closure/expiration is an error, not a
// relist that silently discards history.
func watchRoutingResource(ctx context.Context, resource dynamic.ResourceInterface, ready func([]unstructured.Unstructured) (bool, error)) error {
	list, err := resource.List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	stream, err := resource.Watch(ctx, metav1.ListOptions{ResourceVersion: list.GetResourceVersion()})
	if err != nil {
		return err
	}
	defer stream.Stop()
	current := map[string]unstructured.Unstructured{}
	for _, item := range list.Items {
		current[item.GetName()] = item
	}
	for {
		items := make([]unstructured.Unstructured, 0, len(current))
		for _, item := range current {
			items = append(items, item)
		}
		if done, err := ready(items); done || err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, open := <-stream.ResultChan():
			if !open || event.Type == watch.Error {
				return fmt.Errorf("routing barrier watch closed or failed: %v", event.Object)
			}
			obj, ok := event.Object.(*unstructured.Unstructured)
			if !ok || event.Type == watch.Bookmark {
				continue
			}
			switch event.Type {
			case watch.Deleted:
				delete(current, obj.GetName())
			case watch.Added, watch.Modified:
				current[obj.GetName()] = *obj
			}
		}
	}
}

func (e *Environment) waitForRoutingDeletion(ctx context.Context, runName string) error {
	for _, cluster := range e.clusters() {
		for _, gvr := range routingResources {
			resource := cluster.Clients.Dynamic.Resource(gvr).Namespace(e.Namespace)
			if err := watchRoutingResource(ctx, resource, func(items []unstructured.Unstructured) (bool, error) {
				for _, item := range items {
					if item.GetName() == e.Prefix+"-observation-fence" {
						continue
					}
					switch gvr.Resource {
					case "pipelineruns":
						if item.GetName() == runName {
							return false, nil
						}
					case "taskruns":
						if item.GetLabels()["tekton.dev/pipelineRun"] == runName {
							return false, nil
						}
					case "workloads":
						// This isolated namespace has one execution. Include
						// copied Workloads with different owner references.
						return false, nil
					}
				}
				return true, nil
			}); err != nil {
				return fmt.Errorf("routing cleanup %s on %s: %w", gvr.Resource, cluster.Name, err)
			}
		}
	}
	return nil
}
