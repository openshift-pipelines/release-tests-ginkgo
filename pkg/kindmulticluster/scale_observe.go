package kindmulticluster

import (
	"context"
	"fmt"
	"sync"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
)

type runSource struct {
	worker   int // -1 is the hub.
	resource string
	client   dynamic.ResourceInterface
	options  metav1.ListOptions
}

func (r *scaleRun) subscribe(ctx context.Context, clusters []scaleCluster) error {
	r.ctx, r.cancel = context.WithCancel(ctx)
	for index, cluster := range clusters {
		resources := []schema.GroupVersionResource{{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}, pods}
		if index == 0 {
			resources = append(resources, workloads, schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "taskruns"})
		}
		for _, resource := range resources {
			source := runSource{worker: index - 1, resource: resource.Resource,
				client: cluster.clients.Dynamic.Resource(resource).Namespace(namespace)}
			switch resource.Resource {
			case "pipelineruns":
				source.options.FieldSelector = "metadata.name=" + r.name
			case "pods", "taskruns":
				source.options.LabelSelector = "tekton.dev/pipelineRun=" + r.name
			case "workloads":
				// Workloads are identified by owner UID, not an assumed name.
			}
			if err := r.subscribeSource(source); err != nil {
				return fmt.Errorf("watch %s on %s: %w", resource.Resource, cluster.name, err)
			}
		}
	}
	return nil
}

func (r *scaleRun) subscribeSource(source runSource) error {
	list, err := source.client.List(r.ctx, source.options)
	if err != nil {
		return err
	}
	for i := range list.Items {
		if err := r.record(source, watch.Event{Type: watch.Added, Object: &list.Items[i]}); err != nil {
			return err
		}
	}
	ready := make(chan error, 1)
	var once sync.Once
	events, err := watchtools.NewRetryWatcherWithContext(r.ctx, list.GetResourceVersion(), &cache.ListWatch{
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = source.options.FieldSelector
			options.LabelSelector = source.options.LabelSelector
			stream, err := source.client.Watch(ctx, options)
			once.Do(func() { ready <- err })
			return stream, err
		},
	})
	if err != nil {
		return err
	}
	r.watches = append(r.watches, events)
	r.group.Go(func() error {
		for {
			select {
			case <-r.ctx.Done():
				return nil
			case event, open := <-events.ResultChan():
				if !open {
					if r.ctx.Err() == nil {
						r.fail(fmt.Errorf("%s watch ended before verification", source.resource))
					}
					return nil
				}
				if err := r.record(source, event); err != nil {
					r.fail(err)
					return nil
				}
			}
		}
	})
	select {
	case err := <-ready:
		return err
	case <-r.ctx.Done():
		return r.ctx.Err()
	}
}

func (r *scaleRun) record(source runSource, event watch.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		select {
		case r.changed <- struct{}{}:
		default:
		}
	}()
	switch event.Type {
	case watch.Added, watch.Modified, watch.Deleted:
	case watch.Bookmark:
		return nil
	case watch.Error:
		return apierrors.FromObject(event.Object)
	default:
		return fmt.Errorf("unknown event %q", event.Type)
	}
	object, ok := event.Object.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("unexpected %s watch object %T", source.resource, event.Object)
	}
	switch source.resource {
	case "pipelineruns":
		run := &pipelinev1.PipelineRun{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, run); err != nil {
			return err
		}
		previous := r.hub
		if source.worker >= 0 {
			previous = r.workers[source.worker]
		}
		if previous != nil && previous.UID != run.UID {
			return fmt.Errorf("%s was replaced on worker index %d", r.name, source.worker)
		}
		success, err := runSucceeded(run)
		if err != nil {
			return err
		}
		if event.Type == watch.Deleted && !success {
			return fmt.Errorf("%s disappeared before success", r.name)
		}
		if source.worker == -1 {
			r.hub = run
		} else {
			r.workers[source.worker] = run
			if r.workers[1-source.worker] != nil || r.pods[1-source.worker] != nil {
				return fmt.Errorf("%s appeared on multiple spokes", r.name)
			}
		}
	case "pods", "taskruns":
		if source.worker == -1 {
			return fmt.Errorf("%s created a hub %s", r.name, source.resource)
		}
		pod := &corev1.Pod{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, pod); err != nil {
			return err
		}
		if previous := r.pods[source.worker]; previous != nil && previous.UID != pod.UID {
			return fmt.Errorf("%s created multiple execution pods", r.name)
		}
		r.pods[source.worker] = pod
		if r.workers[1-source.worker] != nil || r.pods[1-source.worker] != nil {
			return fmt.Errorf("%s created pods on multiple spokes", r.name)
		}
	case "workloads":
		for _, owner := range object.GetOwnerReferences() {
			if owner.Kind != "PipelineRun" || owner.Name != r.name {
				continue
			}
			if r.workload != nil && r.workload.GetUID() != object.GetUID() {
				return fmt.Errorf("%s's Workload was replaced", r.name)
			}
			selected, _, err := unstructured.NestedString(object.Object, "status", "clusterName")
			if err != nil {
				return err
			}
			if selected != "" {
				if r.selected != "" && r.selected != selected {
					return fmt.Errorf("%s's selected worker changed from %s to %s", r.name, r.selected, selected)
				}
				r.selected = selected
			}
			r.workload = object
		}
	default:
		return fmt.Errorf("unknown resource %s", source.resource)
	}
	return nil
}
