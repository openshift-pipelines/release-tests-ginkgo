package tektonkueue

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
)

func TestLiveLogsRejectsLoginHTMLAndCloseAfterReadiness(t *testing.T) {
	for _, scenario := range []string{"redirect", "html", "close after ready"} {
		t.Run(scenario, func(t *testing.T) {
			// Given valid discovery responses but an invalid/short-lived stream.
			proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) {
				if proxyJSON(t, w, r) {
					return
				}
				switch scenario {
				case "redirect":
					http.Redirect(w, r, "/auth/login", http.StatusFound)
				case "html":
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte("<html>login</html>"))
				case "close after ready":
					conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer func() { _ = conn.Close() }()
					if err := conn.WriteMessage(websocket.TextMessage, []byte(workloadLogMarker+"\n")); err != nil {
						t.Error(err)
					}
				}
			})
			var released atomic.Bool
			// When no new challenge can be received on the established socket.
			_, err := proxy.liveLogs(t.Context(), wireRun, func(context.Context, string) error { released.Store(true); return nil })
			// Then even a ready marker alone never satisfies live-log acceptance.
			if err == nil {
				t.Fatal("accepted an invalid or prematurely closed log stream")
			}
			if released.Load() != (scenario == "close after ready") {
				t.Fatal("gate release did not follow readiness")
			}
		})
	}
}
