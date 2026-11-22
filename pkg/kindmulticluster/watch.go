// Package kindmulticluster drives the workload proof on script-owned clusters.
package kindmulticluster

import (
	"context"
	"fmt"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
)

// observe uses a list/watch so already-reconciled resources and later updates
// follow the same path. UntilWithSync recovers an expired resource version.
func observe(ctx context.Context, resource dynamic.ResourceInterface, accept func(*unstructured.Unstructured) (bool, error)) (*unstructured.Unstructured, error) {
	lw := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			return resource.List(ctx, options)
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			return resource.Watch(ctx, options)
		},
	}
	event, err := watchtools.UntilWithSync(ctx, lw, &unstructured.Unstructured{}, nil, func(event watch.Event) (bool, error) {
		switch event.Type {
		case watch.Added, watch.Modified:
			object, ok := event.Object.(*unstructured.Unstructured)
			if !ok {
				return false, fmt.Errorf("unexpected watched object %T", event.Object)
			}
			return accept(object)
		case watch.Deleted, watch.Bookmark:
			return false, nil
		case watch.Error:
			return false, fmt.Errorf("API watch error: %v", event.Object)
		default:
			return false, fmt.Errorf("unknown watch event %q", event.Type)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("observe cluster resource: %w", err)
	}
	object, ok := event.Object.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("unexpected accepted object %T", event.Object)
	}
	return object, nil
}

func completed(ctx context.Context, events watch.Interface) (*pipelinev1.PipelineRun, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event, open := <-events.ResultChan():
			if !open {
				return nil, fmt.Errorf("completion watch ended before success")
			}
			switch event.Type {
			case watch.Added, watch.Modified:
			case watch.Error, watch.Deleted:
				return nil, fmt.Errorf("completion watch received %s before success", event.Type)
			case watch.Bookmark:
				continue
			default:
				return nil, fmt.Errorf("unknown completion watch event %q", event.Type)
			}
			run, ok := event.Object.(*pipelinev1.PipelineRun)
			if !ok {
				return nil, fmt.Errorf("unexpected completion object %T", event.Object)
			}
			for _, condition := range run.Status.Conditions {
				if condition.Type != "Succeeded" {
					continue
				}
				switch condition.Status {
				case "True":
					return run, nil
				case "False":
					return nil, fmt.Errorf("PipelineRun failed: %s: %s", condition.Reason, condition.Message)
				case "Unknown":
					continue
				default:
					return nil, fmt.Errorf("invalid completion status %q", condition.Status)
				}
			}
		}
	}
}
