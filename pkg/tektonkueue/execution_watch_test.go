package tektonkueue

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	k8stesting "k8s.io/client-go/testing"
)

func TestExecutionDiscontinuityFailsClosedEvenWhenForbiddenHistoryVanished(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(map[bool]string{false: "close", true: "compaction"}[compact], func(t *testing.T) {
			f := executionEnvironment(t)
			broken := watch.NewRaceFreeFake()
			var calls atomic.Int32
			f.dynamic["hub"].PrependWatchReactor("pods", func(k8stesting.Action) (bool, watch.Interface, error) { calls.Add(1); return true, broken, nil })
			armed := armFixture(t, f, proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) }))
			f.start()
			if err := armed.watches.bind(f.run); err != nil {
				t.Fatal(err)
			}
			if err := armed.watches.drain(armed.ctx); err != nil {
				t.Fatal(err)
			}
			if compact {
				broken.Error(&metav1.Status{Reason: metav1.StatusReasonExpired, Code: 410})
			} else {
				broken.Stop()
			}
			// A forbidden hub Pod exists only during the watch gap; a relist would miss it.
			pod, err := f.dynamic["spoke-1"].Resource(podGVR).Namespace(wireRun.namespace).Get(armed.ctx, wireRun.pod, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.dynamic["hub"].Resource(podGVR).Namespace(wireRun.namespace).Create(armed.ctx, pod, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := f.dynamic["hub"].Resource(podGVR).Namespace(wireRun.namespace).Delete(armed.ctx, pod.GetName(), metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := armed.watches.wait(armed.ctx, func() (bool, error) { return true, nil }); err == nil {
				t.Fatal("accepted a history discontinuity")
			}
			if calls.Load() != 1 {
				t.Fatal("execution history was relisted/resubscribed")
			}
		})
	}
}

func TestExecutionCancellationAndPartialFailureStopStreams(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancellation", true: "partial failure"}[partial], func(t *testing.T) {
			f := executionEnvironment(t)
			var streams []*stoppedReadinessWatch
			f.dynamic["hub"].PrependWatchReactor("*", func(action k8stesting.Action) (bool, watch.Interface, error) {
				if partial && action.GetResource() == workloadGVR {
					return true, nil, errors.New("watch forbidden")
				}
				stream, err := f.dynamic["hub"].Tracker().Watch(action.GetResource(), action.GetNamespace())
				if err != nil {
					return true, nil, err
				}
				wrapped := &stoppedReadinessWatch{Interface: stream, stopped: make(chan struct{})}
				streams = append(streams, wrapped)
				return true, wrapped, nil
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			w, err := f.environment.watchExecution(ctx)
			if !partial {
				if err != nil {
					t.Fatal(err)
				}
				cancel()
				err = w.wait(ctx, func() (bool, error) { return false, nil })
				w.stop()
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation identity: %v", err)
				}
			} else if err == nil {
				w.stop()
				t.Fatal("missing subscription failure")
			}
			for _, stream := range streams {
				select {
				case <-stream.stopped:
				default:
					t.Fatal("execution watch leaked")
				}
			}
		})
	}
}

func TestCleanupContinuesAfterExecutionHistoryFailure(t *testing.T) {
	for _, historyErr := range []error{
		errors.New("execution history discontinuity"),
		context.DeadlineExceeded,
	} {
		t.Run(historyErr.Error(), func(t *testing.T) {
			f := executionEnvironment(t)
			continued := false
			f.environment.addCleanup(func(context.Context) error {
				continued = true
				return nil
			})
			armed := armFixture(t, f, proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) }))
			f.start()
			armed.root = f.run.DeepCopy()
			armed.watches.discontinuity = historyErr

			if err := f.environment.Cleanup(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !continued {
				t.Fatal("execution history failure blocked remaining cleanup")
			}
		})
	}
}
