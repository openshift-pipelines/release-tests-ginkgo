package tektonkueue

import (
	"fmt"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func installRoutingNamespaceHarness(t *testing.T, h *routingHarness, cluster int, api *dynamicfake.FakeDynamicClient) {
	t.Helper()
	namespace := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"metadata": map[string]any{"name": "test", "uid": fmt.Sprintf("namespace-%d", cluster)},
	}}
	namespace.SetLabels((&Environment{Prefix: "test"}).ownedLabels())
	if err := api.Tracker().Create(routingNamespaceGVR, namespace, ""); err != nil {
		t.Fatal(err)
	}
	api.PrependReactor("delete", "namespaces", func(action clienttesting.Action) (bool, runtime.Object, error) {
		name := action.(clienttesting.DeleteAction).GetName()
		current, err := api.Tracker().Get(routingNamespaceGVR, "", name)
		if err != nil {
			return true, nil, err
		}
		uid := action.(clienttesting.DeleteAction).GetDeleteOptions().Preconditions
		if uid == nil || uid.UID == nil || *uid.UID != current.(*unstructured.Unstructured).GetUID() {
			return true, nil, fmt.Errorf("namespace deletion lacked the owned UID precondition")
		}
		if name == "test" && cluster == 2 && h.namespaceBlocked != nil {
			close(h.namespaceBlocked)
			<-h.namespaceRelease
		}
		for index, gvr := range routingResources {
			list, err := api.Tracker().List(gvr, gvr.GroupVersion().WithKind([]string{"PipelineRun", "TaskRun", "Workload"}[index]), name)
			if err != nil {
				return true, nil, err
			}
			items, err := meta.ExtractList(list)
			if err != nil {
				return true, nil, err
			}
			for _, item := range items {
				if err := api.Tracker().Delete(gvr, name, item.(*unstructured.Unstructured).GetName()); err != nil {
					return true, nil, err
				}
			}
		}
		return true, nil, api.Tracker().Delete(routingNamespaceGVR, "", name)
	})
	api.PrependReactor("create", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetNamespace() != "test-observation" {
			return false, nil, nil
		}
		// A final fence must be issued only after execution writes are impossible.
		if _, err := api.Tracker().Get(routingNamespaceGVR, "", "test"); err == nil {
			return true, nil, fmt.Errorf("final fence preceded namespace quiescence")
		}
		return false, nil, nil
	})
}

// Pause a producer after delivering its namespace-local marker deletion. Later
// create/delete events queue on the real fake watch until all final fences issue.
type routingWatchPause struct {
	source  watch.Interface
	output  chan watch.Event
	fenced  chan struct{}
	release chan struct{}
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func (p *routingWatchPause) start(source watch.Interface) watch.Interface {
	p.source = source
	go func() {
		defer close(p.stopped)
		defer close(p.output)
		for {
			select {
			case <-p.done:
				return
			case event, open := <-source.ResultChan():
				if !open {
					return
				}
				select {
				case p.output <- event:
				case <-p.done:
					return
				}
				obj, ok := event.Object.(*unstructured.Unstructured)
				if ok && event.Type == watch.Deleted && obj.GetNamespace() == "test" && obj.GetName() == "test-observation-fence" {
					close(p.fenced)
					select {
					case <-p.release:
					case <-p.done:
						return
					}
				}
			}
		}
	}()
	return p
}

func (p *routingWatchPause) ResultChan() <-chan watch.Event { return p.output }

func (p *routingWatchPause) Stop() {
	p.once.Do(func() { close(p.done) })
	p.source.Stop()
	<-p.stopped
}
