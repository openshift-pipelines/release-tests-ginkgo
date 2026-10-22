package tektonkueue

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestProxyTypedTimeErrorsAndSocketCloseReasonsNeverExposePayload(t *testing.T) {
	const sentinel = "SENTINEL-sensitive-payload-explicit-oauth-token"
	for _, scenario := range []string{"typed time", "close reason"} {
		t.Run(scenario, func(t *testing.T) {
			proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) {
				if scenario == "typed time" && strings.HasSuffix(r.URL.Path, "/pods") {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("X-Worker-Cluster", wireRun.worker)
					_, _ = w.Write([]byte(`{"items":[{"metadata":{"creationTimestamp":"` + sentinel + `"}}]}`))
					return
				}
				if proxyJSON(t, w, r) {
					return
				}
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.Close() }()
				if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, sentinel)); err != nil {
					t.Error(err)
				}
			})
			_, err := proxy.liveLogs(t.Context(), wireRun, func(_ context.Context, _ string) error { t.Error("released gate before ready"); return nil })
			if err == nil {
				t.Fatal("accepted invalid JSON or close-before-marker")
			}
			if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), "explicit-oauth-token") || len(err.Error()) > 160 {
				t.Fatal("error exposes payload or exceeds its bounded category")
			}
		})
	}
}

func TestJSONLimitRejectsWhitespaceBoundaryFollowedByHiddenData(t *testing.T) {
	proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}` + strings.Repeat(" ", (4<<20)-2) + `{"hidden":"payload"}`))
	})
	var value map[string]any
	if _, err := proxy.get(t.Context(), wireRun.path("resolve"), &value); err == nil {
		t.Fatal("accepted hidden data beyond the JSON limit")
	}
}

func TestProxyRequiresBoundTaskPodAndWorkloadIdentities(t *testing.T) {
	for _, scenario := range []string{"task UID", "pod UID", "workload name", "owner UID"} {
		t.Run(scenario, func(t *testing.T) {
			proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) })
			expected := wireRun
			switch scenario {
			case "task UID":
				expected.taskUID = "replacement"
			case "pod UID":
				expected.podUID = "replacement"
			case "workload name":
				expected.workload = "replacement"
			case "owner UID":
				expected.runUID = "replacement"
			}
			if _, err := proxy.pod(t.Context(), expected); err == nil {
				t.Fatal("accepted mismatched proxy identity")
			}
		})
	}
}

func TestConsoleTransportPreservesAlreadyExpiredContextIdentity(t *testing.T) {
	proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) { t.Error("expired request reached console") })
	ctx, cancel := context.WithDeadline(t.Context(), time.Unix(1, 0))
	defer cancel()
	if _, err := proxy.liveLogs(ctx, wireRun, func(context.Context, string) error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline identity was lost: %v", err)
	}
}
