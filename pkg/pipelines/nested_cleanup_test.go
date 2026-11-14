package pipelines

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"knative.dev/pkg/apis"
)

func TestCancelNestedTaskRunsPreservesDiagnosticsAndForeignRuns(t *testing.T) {
	for _, scenario := range []string{"stopped", "closed", "forbidden", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			parent := &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "parent", UID: "parent-uid"}}
			leaf := &v1.TaskRun{ObjectMeta: metav1.ObjectMeta{Name: "leaf", Namespace: "test", UID: "leaf-uid",
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, v1.SchemeGroupVersion.WithKind("PipelineRun"))}}}
			foreign := leaf.DeepCopy()
			foreign.Name, foreign.UID = "foreign", "foreign-uid"
			foreign.OwnerReferences[0].UID = "other-parent-uid"
			client := fake.NewSimpleClientset(leaf, foreign)
			stream := watch.NewRaceFreeFake()
			subscribed, patched := false, false
			client.PrependWatchReactor("taskruns", func(ktesting.Action) (bool, watch.Interface, error) {
				subscribed = true
				return true, stream, nil
			})
			client.PrependReactor("patch", "taskruns", func(action ktesting.Action) (bool, runtime.Object, error) {
				if !subscribed || action.(ktesting.PatchAction).GetName() != leaf.Name {
					t.Fatal("cleanup must subscribe before canceling only the owned TaskRun")
				}
				var request v1.TaskRun
				if err := json.Unmarshal(action.(ktesting.PatchAction).GetPatch(), &request); err != nil {
					t.Fatal(err)
				}
				if request.Spec.Status != v1.TaskRunSpecStatusCancelled {
					t.Fatalf("cleanup status=%q", request.Spec.Status)
				}
				patched = true
				done := leaf.DeepCopy()
				done.Spec.Status = request.Spec.Status
				done.Status.SetConditions([]apis.Condition{{Type: apis.ConditionSucceeded, Status: corev1.ConditionFalse}})
				if err := client.Tracker().Update(v1.SchemeGroupVersion.WithResource("taskruns"), done, "test"); err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "stopped":
					stream.Modify(done)
				case "closed":
					stream.Stop()
				case "forbidden":
					stream.Error(&metav1.Status{Status: metav1.StatusFailure, Reason: metav1.StatusReasonForbidden, Code: 403})
				case "canceled":
					cancel()
				}
				return true, done, nil
			})

			uids, err := CancelNestedTaskRuns(ctx, client.TektonV1().TaskRuns("test"), []types.UID{parent.UID})
			if scenario == "stopped" {
				if err != nil || !patched || !slices.Contains(uids, leaf.UID) || slices.Contains(uids, foreign.UID) {
					t.Fatalf("uids=%v patched=%v error=%v", uids, patched, err)
				}
				retained, err := client.TektonV1().TaskRuns("test").Get(ctx, leaf.Name, metav1.GetOptions{})
				if err != nil || retained.Spec.Status != v1.TaskRunSpecStatusCancelled {
					t.Fatalf("diagnostic resource=%v error=%v", retained, err)
				}
				untouched, err := client.TektonV1().TaskRuns("test").Get(ctx, foreign.Name, metav1.GetOptions{})
				if err != nil || untouched.Spec.Status != "" {
					t.Fatalf("foreign resource=%v error=%v", untouched, err)
				}
			} else if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("cleanup must report %s without waiting for its deadline: %v", scenario, err)
			}
			if scenario == "forbidden" && !apierrors.IsForbidden(err) {
				t.Fatalf("authorization failure: %v", err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
		})
	}
}

func TestWaitNestedPodsStopped(t *testing.T) {
	for _, scenario := range []string{"succeeded", "failed", "deleted", "already-stopped", "closed", "forbidden", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			task := &v1.TaskRun{ObjectMeta: metav1.ObjectMeta{Name: "leaf", UID: "leaf-uid"}}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "leaf-pod", Namespace: "test", UID: "pod-uid",
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(task, v1.SchemeGroupVersion.WithKind("TaskRun"))}},
				Status: corev1.PodStatus{Phase: corev1.PodRunning}}
			foreign := pod.DeepCopy()
			foreign.Name, foreign.UID = "foreign", "foreign-pod-uid"
			foreign.OwnerReferences[0].UID = "foreign-task-uid"
			if scenario == "already-stopped" {
				pod.Status.Phase = corev1.PodFailed
			}
			client := kubefake.NewClientset(pod, foreign)
			client.PrependWatchReactor("pods", func(ktesting.Action) (bool, watch.Interface, error) {
				stream := watch.NewRaceFreeFake()
				done := pod.DeepCopy()
				switch scenario {
				case "succeeded":
					done.Status.Phase = corev1.PodSucceeded
					stream.Modify(done)
				case "failed":
					done.Status.Phase = corev1.PodFailed
					stream.Modify(done)
				case "deleted":
					stream.Delete(pod)
				case "closed":
					stream.Stop()
				case "forbidden":
					stream.Error(&metav1.Status{Status: metav1.StatusFailure, Reason: metav1.StatusReasonForbidden, Code: 403})
				case "canceled":
					cancel()
				}
				return true, stream, nil
			})

			err := WaitNestedPodsStopped(ctx, client.CoreV1().Pods("test"), []types.UID{task.UID})
			switch scenario {
			case "succeeded", "failed", "deleted", "already-stopped":
				if err != nil {
					t.Fatal(err)
				}
			default:
				if err == nil || errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("cleanup failure=%v", err)
				}
			}
			if scenario == "forbidden" && !apierrors.IsForbidden(err) {
				t.Fatalf("authorization failure: %v", err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
		})
	}
}
