package kindmulticluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

func TestTenRunsRetainIndependentMixedWorkerResults(t *testing.T) {
	// Given ten API creates and a mixed placement schedule.
	server, _ := runAPIServer(t, 0)
	defer server.Close()
	batch := apiBatch(t, server)
	defer func() {
		if err := batch.cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	workers := []int{0, 1, 1, 0, 1, 0, 0, 0, 1, 1}
	for range 10 {
		if _, err := batch.create(t.Context(), func(*scaleRun) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}

	// When observations arrive out of creation order, including worker deletion.
	for index := len(batch.runs) - 1; index >= 0; index-- {
		run := batch.runs[index]
		for _, event := range successfulExecution(run, workers[index]) {
			deliver(t, run, event)
		}
		worker := run.workers[workers[index]]
		err := run.record(runSource{worker: workers[index], resource: "pipelineruns"},
			watch.Event{Type: watch.Deleted, Object: unstructuredObject(t, worker)})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Then no run consumes another run's placement, pod, workload, or success.
	for index, run := range batch.runs {
		placement, ready, err := run.progress(runFinished)
		if err != nil || !ready || placement.worker != workers[index] || placement.pod != run.name+"-pod" {
			t.Fatalf("run %s: placement=%+v ready=%t error=%v", run.name, placement, ready, err)
		}
	}
}

func TestScaleRejectsDuplicatePlacement(t *testing.T) {
	// Given a run observed on spoke A.
	run := newScaleRun("duplicate")
	run.uid = "hub-uid"
	events := successfulExecution(run, 0)
	for _, event := range events {
		deliver(t, run, event)
	}

	// When a copy appears on B, even if A is already complete.
	copy := run.workers[0].DeepCopy()
	copy.UID = "other-worker-uid"
	err := run.record(runSource{worker: 1, resource: "pipelineruns"},
		watch.Event{Type: watch.Added, Object: unstructuredObject(t, copy)})

	// Then the duplicate cannot pass completion.
	if err == nil {
		t.Fatal("duplicate execution accepted")
	}
	if _, _, err := run.progress(runFinished); err == nil {
		t.Fatal("duplicate placement lost from per-run state")
	}
}

func TestScaleRejectsFailuresAndInvalidReturnedStatus(t *testing.T) {
	for _, scenario := range []string{"worker-failed", "hub-failed", "status-mismatch", "wrong-target", "hub-execution"} {
		t.Run(scenario, func(t *testing.T) {
			// Given a fully observed run, altered at one observable boundary.
			run := newScaleRun(scenario)
			run.uid = "hub-uid"
			for _, event := range successfulExecution(run, 0) {
				deliver(t, run, event)
			}
			var err error

			// When an invalid event or completion arrives.
			switch scenario {
			case "worker-failed", "hub-failed":
				worker := 0
				failed := run.workers[0].DeepCopy()
				if scenario == "hub-failed" {
					worker, failed = -1, run.hub.DeepCopy()
				}
				failed.Status.Conditions[0].Status = corev1.ConditionFalse
				err = run.record(runSource{worker: worker, resource: "pipelineruns"},
					watch.Event{Type: watch.Modified, Object: unstructuredObject(t, failed)})
			case "status-mismatch":
				run.hub.Status.ChildReferences[0].Name = "a-different-task"
				_, _, err = run.progress(runFinished)
			case "wrong-target":
				run.target = 1
				_, _, err = run.progress(runDispatched)
			case "hub-execution":
				err = run.record(runSource{worker: -1, resource: "pods"},
					watch.Event{Type: watch.Added, Object: unstructuredObject(t, run.pods[0])})
			default:
				t.Fatal("unhandled test scenario")
			}

			// Then it is rejected rather than counted as a successful run.
			if err == nil {
				t.Fatal("invalid execution accepted")
			}
		})
	}
}

func TestRunWaitRespondsToObservedSuccess(t *testing.T) {
	// Given an observer with a subscription established before the event.
	run := newScaleRun("subscribed")
	run.uid = "hub-uid"
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := run.wait(ctx, runFinished)
		result <- err
	}()

	// When completion events arrive (no sleeps or polling).
	for _, event := range successfulExecution(run, 1) {
		deliver(t, run, event)
	}

	// Then the waiter observes that run's success.
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestScaleWaitsForFinishedWorkloadBeforeChangingEligibility(t *testing.T) {
	// Given successful PipelineRuns but a Workload still holding quota.
	run := newScaleRun("finishing")
	run.uid = "hub-uid"
	for _, event := range successfulExecution(run, 0) {
		deliver(t, run, event)
	}
	delete(run.workload.Object["status"].(map[string]any), "conditions")
	if _, ready, err := run.progress(runFinished); err != nil || ready {
		t.Fatalf("unfinished Workload accepted: ready=%t error=%v", ready, err)
	}

	// When Kueue reports the Workload finished.
	deliver(t, run, successfulExecution(run, 0)[0])
	_, ready, err := run.progress(runFinished)

	// Then the next deterministic worker phase may proceed.
	if err != nil || !ready {
		t.Fatalf("finished Workload rejected: ready=%t error=%v", ready, err)
	}
}

type observedEvent struct {
	worker   int
	resource string
	object   runtime.Object
}

func successfulExecution(run *scaleRun, worker int) []observedEvent {
	hub := &pipelinev1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: run.name, UID: run.uid}}
	hub.Status.Conditions = duckv1.Conditions{{Type: apis.ConditionSucceeded, Status: corev1.ConditionTrue}}
	hub.Status.ChildReferences = []pipelinev1.ChildStatusReference{{Name: run.name + "-task"}}
	remote := hub.DeepCopy()
	remote.UID = types.UID(run.name + "-worker")
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: run.name + "-pod", UID: types.UID(run.name + "-pod")}}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: container, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
	workload := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta2", "kind": "Workload",
		"metadata": map[string]any{"name": run.name + "-workload", "uid": run.name + "-workload"},
		"status": map[string]any{
			"clusterName": fmt.Sprintf("release-spoke-%d", worker+1),
			"conditions":  []any{map[string]any{"type": "Finished", "status": "True"}},
		},
	}}
	workload.SetOwnerReferences([]metav1.OwnerReference{{Kind: "PipelineRun", Name: run.name, UID: run.uid}})
	return []observedEvent{{-1, "workloads", workload}, {worker, "pods", pod}, {worker, "pipelineruns", remote}, {-1, "pipelineruns", hub}}
}

func deliver(t *testing.T, run *scaleRun, event observedEvent) {
	t.Helper()
	if err := run.record(runSource{worker: event.worker, resource: event.resource},
		watch.Event{Type: watch.Added, Object: unstructuredObject(t, event.object)}); err != nil {
		t.Fatal(err)
	}
}

func unstructuredObject(t *testing.T, object runtime.Object) *unstructured.Unstructured {
	t.Helper()
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
	if err != nil {
		t.Fatal(err)
	}
	return &unstructured.Unstructured{Object: data}
}
