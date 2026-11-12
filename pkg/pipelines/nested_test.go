package pipelines

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/apis"
)

func TestNestedTaskRejectsDuplicateOrFailedRuns(t *testing.T) {
	for _, scenario := range []string{"success", "failed", "duplicate-after-success", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			parent := &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "parent", UID: "parent-uid"}}
			run := &v1.TaskRun{ObjectMeta: metav1.ObjectMeta{Name: "leaf", Namespace: "test",
				Labels:          map[string]string{"tekton.dev/pipelineTask": "leaf"},
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, v1.SchemeGroupVersion.WithKind("PipelineRun"))}}}
			run.Status.SetConditions([]apis.Condition{{Type: apis.ConditionSucceeded, Status: corev1.ConditionTrue}})
			run.Status.CompletionTime = &metav1.Time{}
			var objects []runtime.Object
			if scenario != "missing" {
				objects = append(objects, run)
			}
			if scenario == "failed" {
				run.Status.SetConditions([]apis.Condition{{Type: apis.ConditionSucceeded, Status: corev1.ConditionFalse}})
			}
			if scenario == "duplicate-after-success" {
				duplicate := run.DeepCopy()
				duplicate.Name = "second"
				duplicate.Status.SetConditions([]apis.Condition{{Type: apis.ConditionSucceeded, Status: corev1.ConditionFalse}})
				objects = append(objects, duplicate)
			}
			client := fake.NewSimpleClientset(objects...)
			got, err := NestedTask(t.Context(), client.TektonV1().TaskRuns("test"), parent, "leaf")
			if scenario == "success" {
				if err != nil || got == nil || got.Name != "leaf" {
					t.Fatalf("run=%v err=%v", got, err)
				}
			} else if err == nil {
				t.Fatalf("%s was accepted", scenario)
			}
		})
	}
}

func TestNestedChildValidatesParentMetadata(t *testing.T) {
	for _, scenario := range []string{"valid", "owner-name", "owner-kind", "parent-label", "reference-name", "reference-kind", "reference-task", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			parent := &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "parent", UID: types.UID("parent-uid")}}
			parent.Status.ChildReferences = []v1.ChildStatusReference{{TypeMeta: runtime.TypeMeta{APIVersion: "tekton.dev/v1", Kind: "PipelineRun"}, Name: "child", PipelineTaskName: "child"}}
			child := &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "test",
				Labels:          map[string]string{"tekton.dev/pipelineTask": "child", "tekton.dev/pipelineRun": parent.Name},
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, v1.SchemeGroupVersion.WithKind("PipelineRun"))}}}
			switch scenario {
			case "owner-name":
				child.OwnerReferences[0].Name = "wrong"
			case "owner-kind":
				child.OwnerReferences[0].Kind = "TaskRun"
			case "parent-label":
				child.Labels["tekton.dev/pipelineRun"] = "wrong"
			case "reference-name":
				parent.Status.ChildReferences[0].Name = "wrong"
			case "reference-kind":
				parent.Status.ChildReferences[0].Kind = "TaskRun"
			case "reference-task":
				parent.Status.ChildReferences[0].PipelineTaskName = "wrong"
			}
			objects := []runtime.Object{child}
			if scenario == "duplicate" {
				duplicate := child.DeepCopy()
				duplicate.Name = "second"
				objects = append(objects, duplicate)
			}
			client := fake.NewSimpleClientset(objects...)
			got, err := NestedChild(t.Context(), client.TektonV1().PipelineRuns("test"), parent, "child")
			if scenario == "valid" {
				if err != nil || got == nil || got.Name != "child" {
					t.Fatalf("child=%v err=%v", got, err)
				}
			} else if err == nil {
				t.Fatalf("%s was accepted", scenario)
			}
		})
	}
}

func TestNestedWatchTerminal(t *testing.T) {
	for _, status := range []corev1.ConditionStatus{corev1.ConditionTrue, corev1.ConditionFalse} {
		t.Run(string(status), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			events := make(chan watch.Event, 5)
			events <- watch.Event{Type: watch.Bookmark, Object: &metav1.PartialObjectMetadata{}}
			events <- watch.Event{Type: watch.Added, Object: &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "parent"}}}
			unrelated := &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "unrelated"}}
			unrelated.Status.SetConditions([]apis.Condition{{Type: apis.ConditionSucceeded, Status: corev1.ConditionTrue}})
			events <- watch.Event{Type: watch.Modified, Object: unrelated}
			for _, state := range []corev1.ConditionStatus{corev1.ConditionUnknown, status} {
				run := &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "parent"}}
				run.Status.SetConditions([]apis.Condition{{Type: apis.ConditionSucceeded, Status: state}})
				events <- watch.Event{Type: watch.Modified, Object: run}
			}
			got, err := ReadNestedWatch(ctx, events, "parent")
			if err != nil || got == nil || got.Name != "parent" || got.Status.GetCondition(apis.ConditionSucceeded).Status != status {
				t.Fatalf("terminal %s: run=%v error=%v", status, got, err)
			}
		})
	}
}

func TestNestedWatchFailures(t *testing.T) {
	for _, kind := range []string{"error", "deleted", "closed", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			events := make(chan watch.Event, 2)
			events <- watch.Event{Type: watch.Modified, Object: &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "unrelated"}}}
			switch kind {
			case "error":
				events <- watch.Event{Type: watch.Error, Object: &metav1.Status{Status: metav1.StatusFailure, Reason: metav1.StatusReasonExpired, Code: 410}}
			case "deleted":
				events <- watch.Event{Type: watch.Deleted, Object: &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "parent"}}}
			case "closed":
				close(events)
			case "canceled":
				cancel()
			}
			run, err := ReadNestedWatch(ctx, events, "parent")
			if err == nil || run != nil || errors.Is(err, context.DeadlineExceeded) || (kind == "canceled" && !errors.Is(err, context.Canceled)) || (kind == "error" && !apierrors.IsResourceExpired(err)) {
				t.Fatalf("%s: run=%v error=%v", kind, run, err)
			}
		})
	}
}
