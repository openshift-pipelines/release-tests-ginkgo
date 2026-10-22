package tektonkueue

import (
	"context"
	"fmt"
	"sync"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
)

var routingResources = []schema.GroupVersionResource{
	{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"},
	{Group: "tekton.dev", Version: "v1", Resource: "taskruns"},
	workloadGVR,
}

type routingStream struct {
	cluster  int
	resource dynamic.ResourceInterface
	marker   *unstructured.Unstructured
	final    *unstructured.Unstructured
	watcher  watch.Interface
	live     map[string]bool
	fenced   bool
}

type routingObserver struct {
	mu       sync.Mutex
	state    routingState
	streams  []*routingStream
	runName  string
	runUID   types.UID
	finished bool
	err      error
	changed  chan struct{}
	cancel   context.CancelFunc
	workers  sync.WaitGroup
	env      *Environment
}

func (e *Environment) observeRouting(ctx context.Context, runName string) (*routingObserver, error) {
	ctx, cancel := context.WithCancel(ctx)
	o := &routingObserver{state: newRoutingState(len(e.clusters())), runName: runName,
		changed: make(chan struct{}, 1), cancel: cancel, env: e}
	for i, cluster := range e.clusters() {
		namespace := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "Namespace",
			"metadata": map[string]any{"name": e.Namespace + "-observation"},
		}}
		if err := e.createDynamic(ctx, cluster, routingNamespaceGVR, namespace); err != nil {
			o.stop()
			return nil, err
		}
		for _, gvr := range routingResources {
			// One collection-wide watch orders execution-namespace events before
			// the final fence in the surviving observation namespace.
			resource := cluster.Clients.Dynamic.Resource(gvr)
			marker, err := e.routingMarker(gvr)
			if err != nil {
				o.stop()
				return nil, err
			}
			final := marker.DeepCopy()
			final.SetNamespace(namespace.GetName())
			marker, err = resource.Namespace(e.Namespace).Create(ctx, marker, metav1.CreateOptions{})
			if err != nil {
				o.stop()
				return nil, fmt.Errorf("create %s observation fence on %s: %w", gvr.Resource, cluster.Name, err)
			}
			stream := &routingStream{cluster: i, resource: resource.Namespace(final.GetNamespace()), marker: marker, final: final, live: map[string]bool{}}
			o.streams = append(o.streams, stream)
			list, err := resource.List(ctx, metav1.ListOptions{})
			if err != nil {
				o.stop()
				return nil, err
			}
			stream.watcher, err = resource.Watch(ctx, metav1.ListOptions{ResourceVersion: list.GetResourceVersion()})
			if err != nil {
				o.stop()
				return nil, err
			}
			for j := range list.Items {
				o.record(stream, watch.Event{Type: watch.Added, Object: &list.Items[j]})
			}
			o.workers.Add(1)
			go func() {
				defer o.workers.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case event, open := <-stream.watcher.ResultChan():
						if !open {
							if ctx.Err() == nil {
								o.fail(fmt.Errorf("routing watch on cluster %d closed before its fence", i))
							}
							return
						}
						o.record(stream, event)
					}
				}
			}()
		}
	}
	return o, nil
}

func (o *routingObserver) record(stream *routingStream, event watch.Event) {
	o.mu.Lock()
	defer o.mu.Unlock()
	defer func() {
		select {
		case o.changed <- struct{}{}:
		default:
		}
	}()
	if event.Type == watch.Error {
		o.err = fmt.Errorf("routing watch error: %v", event.Object)
		return
	}
	obj, ok := event.Object.(*unstructured.Unstructured)
	if !ok || event.Type == watch.Bookmark {
		return
	}
	if obj.GetNamespace() == stream.final.GetNamespace() && obj.GetName() == stream.final.GetName() {
		if stream.final.GetUID() != "" && obj.GetUID() != stream.final.GetUID() {
			o.err = fmt.Errorf("observation fence was replaced")
		}
		stream.fenced = event.Type == watch.Deleted
		return
	}
	if obj.GetNamespace() != o.env.Namespace {
		return
	}
	if obj.GetName() == stream.marker.GetName() {
		if obj.GetUID() != stream.marker.GetUID() {
			o.err = fmt.Errorf("namespace observation fence was replaced")
		}
		return
	}
	relevant := false
	switch obj.GetKind() {
	case "PipelineRun":
		relevant = obj.GetName() == o.runName
		if relevant {
			run := &pipelinev1.PipelineRun{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, run); err != nil {
				o.err = err
			} else if err := o.state.record(routingEvent{stream.cluster, run}); err != nil {
				o.err = err
			}
		}
	case "TaskRun":
		relevant = obj.GetLabels()["tekton.dev/pipelineRun"] == o.runName
		if relevant {
			task := &pipelinev1.TaskRun{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, task); err != nil {
				o.err = err
			} else if err := o.state.record(routingEvent{stream.cluster, task}); err != nil {
				o.err = err
			}
		}
	case "Workload":
		for _, owner := range obj.GetOwnerReferences() {
			if owner.Name == o.runName && owner.Kind == "PipelineRun" {
				if stream.cluster == 0 {
					if o.runUID != "" && o.runUID != owner.UID {
						o.err = fmt.Errorf("workload belongs to a replacement PipelineRun")
					}
					finished, _ := conditionTrue(obj, "Finished")
					o.finished = o.finished || finished
				}
			}
		}
		// MultiKueue copies may have different owner references. This namespace
		// contains only one execution and the inert observation fences.
		relevant = true
	}
	if relevant {
		if event.Type == watch.Deleted {
			delete(stream.live, obj.GetName())
		} else {
			stream.live[obj.GetName()] = true
		}
	}
}

func (o *routingObserver) fail(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err == nil {
		o.err = err
	}
	select {
	case o.changed <- struct{}{}:
	default:
	}
}

func (o *routingObserver) wait(ctx context.Context, ready func() bool) error {
	for {
		o.mu.Lock()
		done := ready()
		err := o.err
		o.mu.Unlock()
		if err != nil || done {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-o.changed:
		}
	}
}

func (o *routingObserver) stop() {
	o.cancel()
	for _, stream := range o.streams {
		if stream.watcher != nil {
			stream.watcher.Stop()
		}
	}
	o.workers.Wait()
}
