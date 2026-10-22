package tektonkueue

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

func armFixture(t *testing.T, f *executionFixture, proxy *consoleProxy) *ArmedExecution {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	armed, err := f.environment.arm(ctx, proxy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(armed.watches.stop)
	return armed
}

func boundFixture(t *testing.T) (*executionFixture, *ArmedExecution) {
	t.Helper()
	f := executionEnvironment(t)
	armed := armFixture(t, f, proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) }))
	f.start()
	if err := armed.watches.bind(f.run); err != nil {
		t.Fatal(err)
	}
	if err := armed.watches.drain(armed.ctx); err != nil {
		t.Fatal(err)
	}
	return f, armed
}

func TestObservationProvesLiveLogsBeforeReturningStatusAndWorkerCleanup(t *testing.T) {
	f := executionEnvironment(t)
	challenge := make(chan string, 1)
	gates := make(chan string, 2)
	f.onGate = func(gate, value string) error {
		if f.subscriptions.Load() != 12 {
			return errors.New("producer released before all history watches existed")
		}
		gates <- gate
		if gate == "gate1" {
			challenge <- value
			return nil
		}
		if gate == "gate2" {
			return f.complete(t.Context())
		}
		return errors.New("unknown gate")
	}
	proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) {
		if proxyJSON(t, w, r) {
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		if err := conn.WriteMessage(websocket.TextMessage, []byte(workloadLogMarker+"\n")); err != nil {
			t.Error(err)
			return
		}
		select {
		case marker := <-challenge:
			if err := conn.WriteMessage(websocket.TextMessage, []byte(marker+"\n")); err != nil {
				t.Error(err)
				return
			}
		case <-r.Context().Done():
			return
		}
		_, _, _ = conn.ReadMessage()
	})
	armed := armFixture(t, f, proxy)
	// External creation occurs only after session identity and all watches are armed.
	f.start()
	result, err := armed.Observe(f.run)
	if err != nil {
		t.Fatal(err)
	}
	gotGates := []string{receiveReadinessEvent(t, armed.ctx, gates), receiveReadinessEvent(t, armed.ctx, gates)}
	if result.Spoke != "spoke-1" || !strings.Contains(result.Logs, "\nlive-") ||
		!reflect.DeepEqual(gotGates, []string{"gate1", "gate2"}) || !armed.watches.success["hub"] || !armed.watches.success["spoke-1"] {
		t.Fatalf("missing causal execution/status evidence: %+v", result)
	}
}

func TestNonterminalReadsRejectTerminalOrReplacedRun(t *testing.T) {
	for _, target := range []string{"hub", "spoke-1"} {
		for _, replacement := range []bool{false, true} {
			t.Run(target+map[bool]string{false: " terminal", true: " replaced"}[replacement], func(t *testing.T) {
				f, armed := boundFixture(t)
				runs := f.tekton[target].TektonV1().PipelineRuns(wireRun.namespace)
				run, err := runs.Get(t.Context(), wireRun.name, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if replacement {
					run.UID = "replacement"
				} else {
					run.Status.Conditions = duckv1.Conditions{{Type: "Succeeded", Status: corev1.ConditionTrue}}
				}
				if _, err := runs.Update(t.Context(), run, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := armed.assertNonterminal(armed.ctx, f.environment.Spokes[0]); err == nil {
					t.Fatal("accepted invalid nonterminal identity/state")
				}
			})
		}
	}
}

func TestCleanupWasRegisteredBeforeTriggerAndUsesTheOriginalWatches(t *testing.T) {
	f := executionEnvironment(t)
	armed := armFixture(t, f, proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) }))
	if len(f.environment.cleanups) != 1 {
		t.Fatal("cleanup was not registered before trigger")
	}
	f.start() // caller fails here, before Observe/binding
	deleted := make(chan struct{})
	f.tekton["hub"].PrependReactor("delete", "pipelineruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if f.subscriptions.Load() != 12 {
			return true, nil, errors.New("cleanup restarted execution histories")
		}
		uid := action.(k8stesting.DeleteAction).GetDeleteOptions().Preconditions.UID
		if uid == nil || *uid != f.run.UID {
			return true, nil, errors.New("missing hub UID precondition")
		}
		if err := f.dynamic["hub"].Resource(pipelineRunGVR).Namespace(wireRun.namespace).Delete(armed.ctx, wireRun.name, metav1.DeleteOptions{}); err != nil {
			return true, nil, err
		}
		close(deleted)
		return false, nil, nil
	})
	done := make(chan error, 1)
	cleanupCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	go func() { done <- f.environment.Cleanup(cleanupCtx) }()
	receiveReadinessEvent(t, armed.ctx, deleted)
	select {
	case err := <-done:
		t.Fatalf("cleanup crossed a live-worker barrier: %v", err)
	default:
	}
	if err := f.workerGC(armed.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.dynamic["hub"].Resource(workloadGVR).Namespace(wireRun.namespace).Delete(armed.ctx, "workload", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := receiveReadinessEvent(t, cleanupCtx, done); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupRefusesHubUIDReplacement(t *testing.T) {
	f, armed := boundFixture(t)
	armed.root = f.run.DeepCopy()
	runs := f.environment.Hub.Clients.PipelineRunClient
	current, err := runs.Get(armed.ctx, f.run.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current.UID = "replacement"
	if _, err := runs.Update(armed.ctx, current, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := f.environment.Cleanup(armed.ctx); err == nil {
		t.Fatal("cleanup accepted replacement")
	}
	current, err = runs.Get(t.Context(), f.run.Name, metav1.GetOptions{})
	if err != nil || current.UID != "replacement" {
		t.Fatal("replacement was deleted")
	}
	if len(f.environment.cleanups) != 1 {
		t.Fatal("cleanup barrier was discarded")
	}
}

func TestBindingRejectsLateArming(t *testing.T) {
	f := executionEnvironment(t)
	f.start()
	armed := armFixture(t, f, proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) }))
	if _, err := armed.Observe(f.run); err == nil {
		t.Fatal("accepted a run created before arming")
	}
}

func TestFailedBoundRunIsRejectedButStillHasUIDGuardedCleanup(t *testing.T) {
	f, armed := boundFixture(t)
	armed.root = f.run.DeepCopy()
	resource := f.dynamic["spoke-1"].Resource(pipelineRunGVR).Namespace(wireRun.namespace)
	run, err := resource.Get(armed.ctx, wireRun.name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	run.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "False"}}}
	if _, err := resource.Update(armed.ctx, run, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := armed.wait(armed.ctx, func() (bool, error) { return true, nil }); err == nil {
		t.Fatal("accepted failed spoke status")
	}
	f.tekton["hub"].PrependReactor("delete", "pipelineruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		uid := action.(k8stesting.DeleteAction).GetDeleteOptions().Preconditions.UID
		if uid == nil || *uid != f.run.UID {
			return true, nil, errors.New("missing UID guard")
		}
		if err := f.workerGC(armed.ctx); err != nil {
			return true, nil, err
		}
		for gvr, name := range map[schema.GroupVersionResource]string{pipelineRunGVR: wireRun.name, workloadGVR: wireRun.workload} {
			if err := f.dynamic["hub"].Resource(gvr).Namespace(wireRun.namespace).Delete(armed.ctx, name, metav1.DeleteOptions{}); err != nil {
				return true, nil, err
			}
		}
		return false, nil, nil
	})
	cleanupCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := f.environment.Cleanup(cleanupCtx); err != nil {
		t.Fatal(err)
	}
}
