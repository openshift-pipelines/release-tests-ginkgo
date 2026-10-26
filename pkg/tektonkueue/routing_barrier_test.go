package tektonkueue

import (
	"context"
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestRoutingBarrierReplaysImmediateWorkloadLifecycleFromListVersion(t *testing.T) {
	// Given: a lifecycle occurring in the LIST/WATCH gap must be replayed by
	// the versioned watch, not recovered from a later empty snapshot.
	api := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{workloadGVR: "WorkloadList"})
	api.PrependReactor("list", "workloads", func(clienttesting.Action) (bool, runtime.Object, error) {
		list := &unstructured.UnstructuredList{}
		list.SetResourceVersion("17")
		return true, list, nil
	})
	stream := watch.NewRaceFreeFake()
	api.PrependWatchReactor("workloads", func(action clienttesting.Action) (bool, watch.Interface, error) {
		if version := action.(clienttesting.WatchAction).GetWatchRestrictions().ResourceVersion; version != "17" {
			t.Errorf("watch resourceVersion = %q, want LIST version 17", version)
		}
		workload := object("Workload", "immediate", "test", nil)
		stream.Add(workload.DeepCopy())
		workload.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Finished", "status": "True"}}}
		stream.Modify(workload.DeepCopy())
		stream.Delete(workload.DeepCopy())
		return true, stream, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	finished := false
	// When
	err := watchRoutingResource(ctx, api.Resource(workloadGVR).Namespace("test"), func(items []unstructured.Unstructured) (bool, error) {
		for _, item := range items {
			done, _ := conditionTrue(&item, "Finished")
			finished = finished || done
		}
		return finished && len(items) == 0, nil
	})
	// Then
	if err != nil || !finished || !stream.IsStopped() {
		t.Fatalf("immediate lifecycle: finished=%t, stopped=%t, error=%v", finished, stream.IsStopped(), err)
	}
}

func TestRoutingBarrierCancellationClosesItsWatch(t *testing.T) {
	// Given
	api := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{workloadGVR: "WorkloadList"})
	stream := watch.NewRaceFreeFake()
	subscribed := make(chan struct{})
	api.PrependWatchReactor("workloads", func(clienttesting.Action) (bool, watch.Interface, error) {
		close(subscribed)
		return true, stream, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	bounded, finish := context.WithTimeout(t.Context(), 10*time.Second)
	defer finish()
	result := make(chan error, 1)
	go func() {
		result <- watchRoutingResource(ctx, api.Resource(workloadGVR).Namespace("test"), func([]unstructured.Unstructured) (bool, error) {
			return false, nil
		})
	}()
	if err := awaitRoutingSignal(bounded, subscribed); err != nil {
		t.Fatal(err)
	}
	// When
	cancel()
	// Then
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !stream.IsStopped() {
			t.Fatalf("cancellation = %v, watch stopped=%t", err, stream.IsStopped())
		}
	case <-bounded.Done():
		t.Fatal(bounded.Err())
	}
}
