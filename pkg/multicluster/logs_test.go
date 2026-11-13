package multicluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestVerifyLiveLogsWhenFramesSplitLines(t *testing.T) {
	// Given a real TLS WebSocket endpoint, gated on the release callback.
	released := make(chan struct{})
	stop := make(chan struct{})
	requests := make(chan *http.Request, 1)
	server := httptest.NewTLSServer(websocket.Handler(func(conn *websocket.Conn) {
		requests <- conn.Request()
		for _, frame := range []string{"wait", "ing\n"} {
			if err := websocket.Message.Send(conn, frame); err != nil {
				t.Error(err)
				return
			}
		}
		select {
		case <-released:
		case <-stop:
			return
		}
		for _, frame := range []string{"li", "ve\n"} {
			if err := websocket.Message.Send(conn, frame); err != nil {
				t.Error(err)
				return
			}
		}
	}))
	defer server.Close()
	defer close(stop)
	request := testLogRequest(server)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// When the verifier opens and releases the stream.
	err := VerifyLiveLogs(ctx, request, func(context.Context) error { close(released); return nil })

	// Then the live line is observed with the expected wire contract.
	if err != nil {
		t.Fatal(err)
	}
	wire := <-requests
	if wire.Header.Get("Authorization") != "Bearer test-token" ||
		wire.URL.Path != "/api/v1/namespaces/test/logs/stream" ||
		wire.URL.Query().Get("pipelineRun") != "run" ||
		wire.URL.Query().Get("pod") != "run-pod" ||
		wire.URL.Query().Get("container") != "step-main" {
		t.Fatalf("unexpected request: %s %v", wire.URL, wire.Header)
	}
}

func TestVerifyLiveLogsRejectsHistoricalMarker(t *testing.T) {
	// Given a stream that already contains the supposed live line.
	server := httptest.NewTLSServer(websocket.Handler(func(conn *websocket.Conn) {
		if err := websocket.Message.Send(conn, "live\n"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	called := false

	// When replayed logs are offered as live evidence.
	err := VerifyLiveLogs(ctx, testLogRequest(server), func(context.Context) error { called = true; return nil })

	// Then the proof fails without releasing the workload.
	if err == nil || called {
		t.Fatalf("historical marker accepted: error=%v, released=%t", err, called)
	}
}

func TestVerifyLiveLogsCancellationInterruptsReceive(t *testing.T) {
	// Given a connected peer that sends no completion line.
	finished := make(chan struct{})
	server := httptest.NewTLSServer(websocket.Handler(func(conn *websocket.Conn) {
		defer close(finished)
		if err := websocket.Message.Send(conn, "waiting\n"); err != nil {
			t.Error(err)
			return
		}
		var message string
		if err := websocket.Message.Receive(conn, &message); err == nil {
			t.Error("client unexpectedly sent a message")
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	// When cancellation arrives after the client has received the ready line.
	err := VerifyLiveLogs(ctx, testLogRequest(server), func(context.Context) error {
		cancel()
		return nil
	})

	// Then receive and the peer both terminate, without a timing delay.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("peer did not observe transport close")
	}
}

func TestVerifyLiveLogsRejectsUntrustedTLS(t *testing.T) {
	// Given a self-signed peer without an explicitly supplied CA.
	server := httptest.NewTLSServer(websocket.Handler(func(*websocket.Conn) { t.Error("untrusted peer connected") }))
	defer server.Close()
	request := testLogRequest(server)
	request.TLS = nil
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// When the verifier connects.
	err := VerifyLiveLogs(ctx, request, func(context.Context) error { return nil })

	// Then TLS verification prevents the connection.
	if err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
}

func TestVerifyLiveLogsRejectsAuthorizationFailure(t *testing.T) {
	// Given an authentic TLS endpoint that denies the bearer token.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	called := false

	// When the handshake is denied.
	err := VerifyLiveLogs(ctx, testLogRequest(server), func(context.Context) error { called = true; return nil })

	// Then neither historical HTTP output nor a rejected handshake passes.
	if err == nil || called {
		t.Fatalf("denied request accepted: error=%v, released=%t", err, called)
	}
}

func testLogRequest(server *httptest.Server) LogRequest {
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	return LogRequest{
		Endpoint:  "wss" + strings.TrimPrefix(server.URL, "https"),
		Token:     "test-token",
		TLS:       &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		Namespace: "test", PipelineRun: "run", Pod: "run-pod", Container: "step-main",
		ReadyMarker: "waiting", LiveMarker: "live",
	}
}
