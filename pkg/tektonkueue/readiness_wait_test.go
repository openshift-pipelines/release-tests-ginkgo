package tektonkueue

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	k8stesting "k8s.io/client-go/testing"
)

func TestWaitReadyConvergesOnResourceEventsBeforeOneShotAssertion(t *testing.T) {
	for _, scenario := range []struct {
		name, cluster, namespace, resourceName string
		gvr                                    schema.GroupVersionResource
	}{
		{"hub local queue generation", "hub", readinessPrefix, readinessPrefix, localQueueGVR},
		{"spoke local queue generation", "spoke-1", readinessPrefix, readinessPrefix, localQueueGVR},
		{"component deployment rollout", "hub", "openshift-pipelines", "scheduler", deploymentGVR},
		{"worker active generation", "hub", "", readinessPrefix + "-spoke-1", multiKueueClusterGVR},
		{"admission active generation", "hub", "", readinessPrefix, admissionCheckGVR},
		{"proxy pod readiness", "hub", "openshift-pipelines", "proxy-pod", podGVR},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Given a subscribed environment with exactly one pending readiness condition.
			f := watchEnvironment(t, func(name string, fixture *readinessFixture) {
				if name != scenario.cluster {
					return
				}
				if scenario.gvr == podGVR {
					fixture.pods[0].Status.Conditions[0].Status = corev1.ConditionFalse
					fixture.pods[0].Status.ContainerStatuses[0].Ready = false
					return
				}
				path := "/apis/" + scenario.gvr.GroupVersion().String() + "/"
				if scenario.namespace != "" {
					path += "namespaces/" + scenario.namespace + "/"
				}
				object := fixture.objects[path+scenario.gvr.Resource+"/"+scenario.resourceName]
				if scenario.gvr == deploymentGVR {
					object.Object["status"].(map[string]any)["observedGeneration"] = int64(1)
				} else {
					object.SetGeneration(3)
					if scenario.gvr == multiKueueClusterGVR || scenario.gvr == admissionCheckGVR {
						object.Object["status"].(map[string]any)["conditions"].([]any)[0].(map[string]any)["status"] = "False"
					}
				}
			})
			read := make(chan struct{}, 1)
			events := make(chan watch.Event)
			client := f.clients[scenario.cluster]
			client.PrependWatchReactor(scenario.gvr.Resource, func(action k8stesting.Action) (bool, watch.Interface, error) {
				if rv := action.(k8stesting.WatchAction).GetWatchRestrictions().ResourceVersion; rv != "42" {
					return true, nil, errors.New("watch did not resume from the list snapshot")
				}
				stream := &stoppedReadinessWatch{Interface: watch.NewProxyWatcher(events), stopped: make(chan struct{})}
				f.subscribed <- readinessWatchEvent{name: scenario.cluster + "/" + scenario.gvr.Resource, stream: stream}
				return true, stream, nil
			})
			client.PrependReactor("*", scenario.gvr.Resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
				isRead := action.GetVerb() == "get"
				if list, ok := action.(k8stesting.ListAction); ok {
					isRead = !list.GetListRestrictions().Labels.Empty()
				}
				if isRead {
					select {
					case read <- struct{}{}:
					default:
					}
				}
				return false, nil, nil
			})
			ctx, _, done := startReadinessWait(t, f.environment)
			subscribedReadinessWatches(t, ctx, f)
			receiveReadinessEvent(t, ctx, read)
			resource := client.Resource(scenario.gvr).Namespace(scenario.namespace)
			current, err := resource.Get(ctx, scenario.resourceName, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			status := current.Object["status"].(map[string]any)
			switch scenario.gvr {
			case deploymentGVR:
				status["observedGeneration"] = current.GetGeneration()
			case podGVR:
				status["conditions"].([]any)[0].(map[string]any)["status"] = "True"
				status["containerStatuses"].([]any)[0].(map[string]any)["ready"] = true
			default:
				status["conditions"].([]any)[0].(map[string]any)["observedGeneration"] = current.GetGeneration()
				status["conditions"].([]any)[0].(map[string]any)["status"] = "True"
			}
			// When the controller update produces the watch event (no timed retry).
			if _, err := resource.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			select {
			case events <- watch.Event{Type: watch.Modified, Object: current}:
			case err := <-done:
				t.Fatalf("barrier finished before receiving the controller event: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// Then the barrier completes without HTTP, followed by exactly one real assertion.
			if err := receiveReadinessEvent(t, ctx, done); err != nil {
				t.Fatal(err)
			}
			select {
			case <-f.probed:
				t.Fatal("barrier requested HTTP /ready instead of waiting for pod readiness")
			default:
			}
			if err := f.environment.AssertReady(ctx); err != nil {
				t.Fatal(err)
			}
			receiveReadinessEvent(t, ctx, f.probed)
		})
	}
}

func TestWaitReadySeesChangeBetweenListAndWatch(t *testing.T) {
	// Given a LocalQueue that changes after list but before its watch is returned.
	f := watchEnvironment(t, func(name string, fixture *readinessFixture) {
		if name == "hub" {
			fixture.objects["/apis/kueue.x-k8s.io/v1beta2/namespaces/"+readinessPrefix+"/localqueues/"+readinessPrefix].SetGeneration(3)
		}
	})
	client := f.clients["hub"]
	changed := make(chan struct{}, 1)
	client.PrependWatchReactor("localqueues", func(action k8stesting.Action) (bool, watch.Interface, error) {
		object, err := client.Tracker().Get(localQueueGVR, readinessPrefix, readinessPrefix)
		if err != nil {
			return true, nil, err
		}
		current := object.(*unstructured.Unstructured)
		current.Object["status"].(map[string]any)["conditions"].([]any)[0].(map[string]any)["observedGeneration"] = int64(3)
		if err := client.Tracker().Update(localQueueGVR, current, readinessPrefix); err != nil {
			return true, nil, err
		}
		changed <- struct{}{}
		return false, nil, nil
	})
	// When watches are established and the post-subscription snapshot is evaluated.
	ctx, _, done := startReadinessWait(t, f.environment)
	receiveReadinessEvent(t, ctx, changed)
	// Then no future event is required to notice the already completed update.
	if err := receiveReadinessEvent(t, ctx, done); err != nil {
		t.Fatal(err)
	}
}

func TestWaitReadyCancellationStopsEverySubscription(t *testing.T) {
	// Given a subscribed environment with a LocalQueue that remains stale.
	f := watchEnvironment(t, func(name string, fixture *readinessFixture) {
		if name == "hub" {
			fixture.objects["/apis/kueue.x-k8s.io/v1beta2/namespaces/"+readinessPrefix+"/localqueues/"+readinessPrefix].SetGeneration(3)
		}
	})
	ctx, cancel, done := startReadinessWait(t, f.environment)
	streams := subscribedReadinessWatches(t, ctx, f)
	// When
	cancel()
	// Then cancellation is returned and every established stream is stopped.
	err := receiveReadinessEvent(t, t.Context(), done)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitReady() = %v, want context cancellation", err)
	}
	for _, stream := range streams {
		select {
		case <-stream.stopped:
		default:
			t.Fatal("readiness watch leaked after cancellation")
		}
	}
}

func TestWaitReadyPropagatesWatchFailure(t *testing.T) {
	// Given an API that refuses one of the required watch subscriptions.
	f := watchEnvironment(t, nil)
	want := errors.New("watch forbidden")
	f.clients["hub"].PrependWatchReactor("localqueues", func(k8stesting.Action) (bool, watch.Interface, error) {
		return true, nil, want
	})
	// When
	err := f.environment.WaitReady(t.Context())
	// Then watch errors fail the barrier rather than falling back to polling.
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "localqueues") {
		t.Fatalf("WaitReady() = %v, want localqueue watch failure", err)
	}
}

func TestWaitReadyRelistsAfterExpiredWatch(t *testing.T) {
	// Given a stale LocalQueue and an API watch whose resourceVersion is compacted.
	f := watchEnvironment(t, func(name string, fixture *readinessFixture) {
		if name == "hub" {
			fixture.objects["/apis/kueue.x-k8s.io/v1beta2/namespaces/"+readinessPrefix+"/localqueues/"+readinessPrefix].SetGeneration(3)
		}
	})
	client := f.clients["hub"]
	events := make(chan watch.Event)
	first := true
	client.PrependWatchReactor("localqueues", func(k8stesting.Action) (bool, watch.Interface, error) {
		if !first {
			return false, nil, nil
		}
		first = false
		stream := &stoppedReadinessWatch{Interface: watch.NewProxyWatcher(events), stopped: make(chan struct{})}
		f.subscribed <- readinessWatchEvent{name: "hub/localqueues", stream: stream}
		return true, stream, nil
	})
	ctx, _, done := startReadinessWait(t, f.environment)
	subscribedReadinessWatches(t, ctx, f)
	// When compaction requires a new list/watch snapshot and the controller catches up.
	select {
	case events <- watch.Event{Type: watch.Error, Object: &metav1.Status{Reason: metav1.StatusReasonExpired, Code: 410}}:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	resource := client.Resource(localQueueGVR).Namespace(readinessPrefix)
	current, err := resource.Get(ctx, readinessPrefix, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current.Object["status"].(map[string]any)["conditions"].([]any)[0].(map[string]any)["observedGeneration"] = int64(3)
	if _, err := resource.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	// Then a new subscription is established without a timer or lost update.
	if event := receiveReadinessEvent(t, ctx, f.subscribed); event.name != "hub/localqueues" {
		t.Fatalf("unexpected replacement subscription: %s", event.name)
	}
	if err := receiveReadinessEvent(t, ctx, done); err != nil {
		t.Fatal(err)
	}
}
