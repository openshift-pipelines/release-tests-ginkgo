package tektonkueue

import (
	"context"
	"sync"
	"testing"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	pipelinefake "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/fake"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

type routingInjection struct {
	cluster  int
	resource string
}

type trackedRoutingWatch struct {
	watch.Interface
	stopped chan struct{}
	once    sync.Once
}

func (w *trackedRoutingWatch) Stop() {
	w.Interface.Stop()
	w.once.Do(func() { close(w.stopped) })
}

type routingHarness struct {
	env              *Environment
	apis             []*dynamicfake.FakeDynamicClient
	watches          []*trackedRoutingWatch
	created          chan struct{}
	fence            chan struct{}
	release          chan struct{}
	run              *pipelinev1.PipelineRun
	immediate        bool
	pause            *routingWatchPause
	namespaceBlocked chan struct{}
	namespaceRelease chan struct{}
	finalFences      chan struct{}
}

func newRoutingHarness(t *testing.T, injection routingInjection) *routingHarness {
	t.Helper()
	h := &routingHarness{created: make(chan struct{}), fence: make(chan struct{}),
		release: make(chan struct{}), immediate: true}
	kinds := map[schema.GroupVersionResource]string{
		routingResources[0]: "PipelineRunList", routingResources[1]: "TaskRunList", workloadGVR: "WorkloadList",
		routingNamespaceGVR: "NamespaceList",
	}
	clusters := make([]Cluster, 0, 3)
	for i := range 3 {
		api := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), kinds)
		installRoutingNamespaceHarness(t, h, i, api)
		api.PrependWatchReactor("*", func(action clienttesting.Action) (bool, watch.Interface, error) {
			w, err := api.Tracker().Watch(action.GetResource(), action.GetNamespace())
			if h.pause != nil && i == 0 && action.GetResource().Resource == "taskruns" {
				w = h.pause.start(w)
			}
			tracked := &trackedRoutingWatch{Interface: w, stopped: make(chan struct{})}
			h.watches = append(h.watches, tracked)
			return true, tracked, err
		})
		api.PrependReactor("delete", injection.resource, func(action clienttesting.Action) (bool, runtime.Object, error) {
			if i == injection.cluster && action.(clienttesting.DeleteAction).GetName() == "test-observation-fence" {
				close(h.fence)
				<-h.release
			}
			return false, nil, nil
		})
		api.PrependReactor("delete", "workloads", func(action clienttesting.Action) (bool, runtime.Object, error) {
			if h.finalFences != nil && i == 2 && action.GetNamespace() == "test-observation" {
				if err := api.Tracker().Delete(action.GetResource(), action.GetNamespace(), action.(clienttesting.DeleteAction).GetName()); err != nil {
					return true, nil, err
				}
				close(h.finalFences)
				return true, nil, nil
			}
			return false, nil, nil
		})
		h.apis = append(h.apis, api)
		clusters = append(clusters, Cluster{Name: "hub", Clients: &clients.Clients{Dynamic: api}})
		if i > 0 {
			clusters[i].Name = "spoke-" + string(rune('0'+i))
		}
	}
	h.env = NewEnvironment(clusters[0], clusters[1:], "test")
	h.env.routingWaits = true
	h.env.routingNamespaceUIDs = map[string]types.UID{"hub": "namespace-0", "spoke-1": "namespace-1", "spoke-2": "namespace-2"}
	hub := pipelinefake.NewSimpleClientset()
	h.env.Hub.Clients.PipelineRunClient = hub.TektonV1().PipelineRuns("test")
	hub.PrependReactor("create", "pipelineruns", func(action clienttesting.Action) (bool, runtime.Object, error) {
		run := action.(clienttesting.CreateAction).GetObject().(*pipelinev1.PipelineRun).DeepCopy()
		_, expected, err := h.env.newRoutingRun(run.Labels[routeLabel])
		if run.Labels[routeLabel] == "" {
			_, expected, err = h.env.newRoutingRun("missing")
		}
		if err != nil {
			return true, nil, err
		}
		run.UID = "run-uid"
		run.Labels[queueLabel] = expected.queue
		run.Spec.Status = pipelinev1.PipelineRunSpecStatusPending
		if expected.remote {
			run.Spec.ManagedBy = ptr(multiKueueController)
		}
		h.run = run.DeepCopy()
		if h.immediate {
			if err := h.execute(expected.remote); err != nil {
				return true, nil, err
			}
		}
		close(h.created)
		return true, run, nil
	})
	hub.PrependReactor("get", "pipelineruns", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, h.run.DeepCopy(), nil
	})
	hub.PrependReactor("delete", "pipelineruns", func(clienttesting.Action) (bool, runtime.Object, error) {
		for _, item := range []struct{ resource, name string }{
			{"pipelineruns", h.run.Name}, {"taskruns", "task"}, {"workloads", "execution"},
		} {
			if err := h.drop(0, item.resource, item.name); err != nil {
				return true, nil, err
			}
		}
		return true, nil, nil
	})
	return h
}

func (h *routingHarness) put(cluster int, obj runtime.Object) error {
	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return err
	}
	u := &unstructured.Unstructured{Object: value}
	resource := ""
	switch obj.(type) {
	case *pipelinev1.PipelineRun:
		u.SetAPIVersion("tekton.dev/v1")
		u.SetKind("PipelineRun")
		resource = "pipelineruns"
	case *pipelinev1.TaskRun:
		u.SetAPIVersion("tekton.dev/v1")
		u.SetKind("TaskRun")
		resource = "taskruns"
	case *unstructured.Unstructured:
		resource = "workloads"
	}
	for _, gvr := range routingResources {
		if gvr.Resource == resource {
			return h.apis[cluster].Tracker().Create(gvr, u, "test")
		}
	}
	return nil
}

func (h *routingHarness) drop(cluster int, resource, name string) error {
	for _, gvr := range routingResources {
		if gvr.Resource == resource {
			err := h.apis[cluster].Tracker().Delete(gvr, "test", name)
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

func (h *routingHarness) execute(remote bool) error {
	run := h.run.DeepCopy()
	run.Status = successfulRoutingRun().Status
	if err := h.put(0, run); err != nil {
		return err
	}
	selected := 0
	if remote {
		selected = 1
		run.Spec.ManagedBy = nil
		if err := h.put(1, run); err != nil {
			return err
		}
	}
	task := successfulRoutingTask()
	task.Labels = map[string]string{"tekton.dev/pipelineRun": run.Name}
	if err := h.put(selected, task); err != nil {
		return err
	}
	workload := object("Workload", "execution", "test", nil)
	workload.SetOwnerReferences([]metav1.OwnerReference{{Name: run.Name, Kind: "PipelineRun", UID: run.UID}})
	workload.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Finished", "status": "True"}}}
	if err := h.put(0, workload); err != nil {
		return err
	}
	// Both the terminal Workload and worker can disappear before Create returns.
	if err := h.drop(0, "workloads", "execution"); err != nil {
		return err
	}
	if remote {
		if err := h.drop(1, "taskruns", "task"); err != nil {
			return err
		}
		return h.drop(1, "pipelineruns", run.Name)
	}
	return nil
}

func awaitRoutingSignal(ctx context.Context, signal <-chan struct{}) error {
	select {
	case <-signal:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
