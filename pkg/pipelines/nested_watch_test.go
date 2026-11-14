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
	"k8s.io/apimachinery/pkg/watch"
	ktesting "k8s.io/client-go/testing"
	"knative.dev/pkg/apis"
)

func TestResumeNestedPipelineRun(t *testing.T) {
	for _, scenario := range []string{"terminal-success", "terminal-failure", "closed", "expired-event", "expired-start", "resumed-watch"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			parent := &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"}}
			status := corev1.ConditionTrue
			if scenario == "terminal-failure" {
				status = corev1.ConditionFalse
			}
			terminal := parent.DeepCopy()
			terminal.Status.SetConditions([]apis.Condition{{Type: apis.ConditionSucceeded, Status: status}})
			if scenario == "terminal-success" || scenario == "terminal-failure" {
				parent = terminal
			}
			client := fake.NewSimpleClientset(parent)
			client.PrependReactor("list", "pipelineruns", func(ktesting.Action) (bool, runtime.Object, error) {
				object, err := client.Tracker().List(v1.SchemeGroupVersion.WithResource("pipelineruns"),
					v1.SchemeGroupVersion.WithKind("PipelineRun"), "test")
				if err != nil {
					return true, nil, err
				}
				list := object.(*v1.PipelineRunList)
				list.ResourceVersion = "42"
				return true, list, nil
			})
			interrupted := false
			client.PrependWatchReactor("pipelineruns", func(action ktesting.Action) (bool, watch.Interface, error) {
				restrictions := action.(ktesting.WatchAction).GetWatchRestrictions()
				if restrictions.ResourceVersion != "42" || restrictions.Fields.String() != "metadata.name=parent" {
					t.Fatalf("watch must resume from the named list snapshot: %+v", restrictions)
				}
				stream := watch.NewRaceFreeFake()
				if interrupted {
					stream.Modify(terminal)
					return true, stream, nil
				}
				interrupted = true
				if scenario != "resumed-watch" {
					if err := client.Tracker().Update(v1.SchemeGroupVersion.WithResource("pipelineruns"), terminal, "test"); err != nil {
						t.Fatal(err)
					}
				}
				switch scenario {
				case "expired-start":
					return true, nil, apierrors.NewResourceExpired("snapshot expired")
				case "expired-event":
					stream.Error(&metav1.Status{Status: metav1.StatusFailure, Reason: metav1.StatusReasonExpired, Code: 410})
				default:
					stream.Stop()
				}
				return true, stream, nil
			})

			got, err := ResumeNestedPipelineRun(ctx, client.TektonV1().PipelineRuns("test"), "parent")
			if err != nil || got == nil {
				t.Fatalf("run=%v error=%v", got, err)
			}
			if condition := got.Status.GetCondition(apis.ConditionSucceeded); condition == nil || condition.Status != status {
				t.Fatalf("terminal state=%+v, want %s", condition, status)
			}
		})
	}
}

func TestResumeNestedPipelineRunRejectsFatalFailures(t *testing.T) {
	for _, scenario := range []string{"deleted", "missing", "forbidden-event", "forbidden-start", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			parent := &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "parent", Namespace: "test"}}
			client := fake.NewSimpleClientset(parent)
			if scenario == "missing" {
				if err := client.Tracker().Delete(v1.SchemeGroupVersion.WithResource("pipelineruns"), "test", "parent"); err != nil {
					t.Fatal(err)
				}
			}
			client.PrependWatchReactor("pipelineruns", func(ktesting.Action) (bool, watch.Interface, error) {
				stream := watch.NewRaceFreeFake()
				switch scenario {
				case "deleted":
					stream.Delete(parent)
				case "forbidden-event":
					stream.Error(&metav1.Status{Status: metav1.StatusFailure, Reason: metav1.StatusReasonForbidden, Code: 403})
				case "forbidden-start":
					return true, nil, apierrors.NewForbidden(v1.Resource("pipelineruns"), "parent", errors.New("denied"))
				case "canceled":
					cancel()
				}
				return true, stream, nil
			})

			got, err := ResumeNestedPipelineRun(ctx, client.TektonV1().PipelineRuns("test"), "parent")
			if got != nil || err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("run=%v error=%v", got, err)
			}
			if scenario == "missing" && !apierrors.IsNotFound(err) {
				t.Fatalf("missing run: %v", err)
			}
			if (scenario == "forbidden-event" || scenario == "forbidden-start") && !apierrors.IsForbidden(err) {
				t.Fatalf("authorization failure: %v", err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
		})
	}
}
