package tektonkueue

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
)

func TestPACUsesParentLiveLogsStatusAndUIDGuardedSecretCleanup(t *testing.T) {
	challenge := make(chan string, 1)
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
	f, armed := preparePACFixture(t, proxy)
	publishSecret(t, f, "hub", generatedPACSecret("hub"))
	publishSecret(t, f, "spoke-1", generatedPACSecret("spoke-1"))
	f.onGate = func(gate, value string) error {
		if f.subscriptions.Load() != 15 {
			return errors.New("trigger/gates preceded Secret subscriptions")
		}
		if gate == "gate1" {
			challenge <- value
			return nil
		}
		if err := f.complete(armed.ctx); err != nil {
			return err
		}
		return f.dynamic["spoke-1"].Resource(secretGVR).Namespace(wireRun.namespace).Delete(armed.ctx, generatedSecret, metav1.DeleteOptions{})
	}
	result, err := armed.ObservePAC(pacEvent)
	if err != nil {
		t.Fatal(err)
	}
	if armed.pac.reference == nil || !strings.Contains(result.Logs, "\nlive-") || strings.Contains(result.Logs, payloadSentinel) {
		t.Fatal("missing verification or unsafe accepted logs")
	}
	f.tekton["hub"].PrependReactor("delete", "pipelineruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		uid := action.(k8stesting.DeleteAction).GetDeleteOptions().Preconditions.UID
		if uid == nil || *uid != f.run.UID {
			return true, nil, errors.New("cleanup lost hub UID")
		}
		for gvr, name := range map[schema.GroupVersionResource]string{pipelineRunGVR: wireRun.name, workloadGVR: wireRun.workload, secretGVR: generatedSecret} {
			if err := f.dynamic["hub"].Resource(gvr).Namespace(wireRun.namespace).Delete(t.Context(), name, metav1.DeleteOptions{}); err != nil {
				return true, nil, err
			}
		}
		return false, nil, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := f.environment.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPACCorrelationHasNoLatestRunFallbackAndRejectsReplacement(t *testing.T) {
	for _, scenario := range []string{"no match", "replacement"} {
		t.Run(scenario, func(t *testing.T) {
			f, armed := preparePACFixture(t, nil)
			expected := pacEvent
			if scenario == "no match" {
				expected.Correlation = "different"
				armed.pac.correlation = expected.Correlation
			}
			armed.pac.expected = &expected
			if err := armed.watches.drain(armed.ctx); err != nil {
				t.Fatal(err)
			}
			if scenario == "replacement" {
				client := f.dynamic["hub"].Resource(pipelineRunGVR).Namespace(wireRun.namespace)
				run, err := client.Get(armed.ctx, wireRun.name, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if err := client.Delete(armed.ctx, wireRun.name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				run.SetUID("replacement")
				if _, err := client.Create(armed.ctx, run, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := armed.watches.drain(armed.ctx); err != nil {
					t.Fatal(err)
				}
			}
			run, err := armed.findPACRun()
			if scenario == "no match" {
				if err != nil || run != nil {
					t.Fatal("fell back to an uncorrelated/latest run")
				}
			} else if err == nil {
				t.Fatal("accepted replaced correlated run")
			}
		})
	}
}
