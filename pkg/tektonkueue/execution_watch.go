package tektonkueue

import (
	"context"
	"fmt"
	"reflect"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

var (
	pipelineRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
	taskRunGVR     = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "taskruns"}
)

type executionSubscription struct {
	readinessSubscription
	cluster Cluster
	gvr     schema.GroupVersionResource
}

type executionEvent struct {
	cluster   string
	gvr       schema.GroupVersionResource
	kind      watch.EventType
	object    *unstructured.Unstructured
	beforeArm bool
}

type executionWatches struct {
	subscriptions []*executionSubscription
	hub           string
	namespace     string
	runName       string
	hubUID        types.UID
	events        []executionEvent
	retained      map[string]*clusterExecution
	seen          map[string]bool
	success       map[string]bool
	failed        map[string]bool
	discontinuity error
	armed         bool
	pac           *pacExecution
}

func (e *Environment) watchExecution(ctx context.Context) (*executionWatches, error) {
	w := &executionWatches{hub: e.Hub.Name, namespace: e.Namespace}
	for _, cluster := range e.clusters() {
		for _, gvr := range []schema.GroupVersionResource{pipelineRunGVR, taskRunGVR, workloadGVR, podGVR} {
			s := &executionSubscription{
				readinessSubscription: readinessSubscription{
					client: cluster.Clients.Dynamic.Resource(gvr).Namespace(e.Namespace),
					name:   cluster.Name + "/" + gvr.Resource,
				},
				cluster: cluster, gvr: gvr,
			}
			w.subscriptions = append(w.subscriptions, s)
			if err := s.subscribe(ctx); err != nil {
				w.stop()
				return nil, requestError(ctx, "subscribe execution history")
			}
			for i := range s.snapshot.Items {
				if err := w.record(s, watch.Added, &s.snapshot.Items[i]); err != nil {
					w.stop()
					return nil, err
				}
			}
		}
	}
	return w, nil
}

func (w *executionWatches) bind(run *pipelinev1.PipelineRun) error {
	if run == nil || run.Name == "" || run.UID == "" || run.Namespace != w.namespace {
		return fmt.Errorf("binding requires the actual hub PipelineRun identity")
	}
	if w.runName != "" && (w.runName != run.Name || w.hubUID != run.UID) {
		return fmt.Errorf("execution is already bound to a different hub identity")
	}
	for _, event := range w.events {
		if event.beforeArm && event.cluster == w.hub && event.gvr == pipelineRunGVR && event.object.GetName() == run.Name {
			return fmt.Errorf("execution was not armed before hub creation")
		}
	}
	w.runName, w.hubUID = run.Name, run.UID
	return w.validateHistory()
}

func (w *executionWatches) record(s *executionSubscription, kind watch.EventType, object *unstructured.Unstructured) error {
	if s.gvr == secretGVR {
		return w.pac.record(s.cluster.Name, kind, object)
	}
	if len(w.events) >= 4096 {
		w.discontinuity = fmt.Errorf("execution history exceeded its bounded capacity")
		return w.discontinuity
	}
	w.events = append(w.events, executionEvent{s.cluster.Name, s.gvr, kind, object.DeepCopy(), !w.armed})
	if w.runName != "" {
		return w.validateHistory()
	}
	return nil
}

func (w *executionWatches) stop() {
	for _, s := range w.subscriptions {
		if s.stream != nil {
			s.stream.Stop()
		}
	}
}

func (w *executionWatches) drain(ctx context.Context) error {
	for {
		consumed, err := w.next(ctx, false)
		if err != nil {
			return err
		}
		if !consumed {
			return w.validateHistory()
		}
	}
}

func (w *executionWatches) wait(ctx context.Context, ready func() (bool, error)) error {
	for {
		if err := w.drain(ctx); err != nil {
			return err
		}
		if ok, err := ready(); err != nil || ok {
			return err
		}
		if _, err := w.next(ctx, true); err != nil {
			return err
		}
	}
}

// Unlike readiness, execution cannot reconstruct lost history from a snapshot.
// Every close, Error/compaction, or malformed event permanently fails closed.
func (w *executionWatches) next(ctx context.Context, block bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if w.discontinuity != nil {
		return false, w.discontinuity
	}
	cases := []reflect.SelectCase{{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())}}
	for _, s := range w.subscriptions {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(s.stream.ResultChan())})
	}
	if !block {
		cases = append(cases, reflect.SelectCase{Dir: reflect.SelectDefault})
	}
	index, value, open := reflect.Select(cases)
	if index == 0 {
		return false, ctx.Err()
	}
	if index > len(w.subscriptions) {
		return false, nil
	}
	s := w.subscriptions[index-1]
	if !open {
		w.discontinuity = fmt.Errorf("execution history discontinuity: closed %s", s.name)
		return false, w.discontinuity
	}
	event := value.Interface().(watch.Event)
	switch event.Type {
	case watch.Bookmark:
		return true, nil
	case watch.Added, watch.Modified, watch.Deleted:
		object, ok := event.Object.(*unstructured.Unstructured)
		if ok {
			return true, w.record(s, event.Type, object)
		}
	}
	w.discontinuity = fmt.Errorf("execution history discontinuity: invalid or compacted %s", s.name)
	return false, w.discontinuity
}
