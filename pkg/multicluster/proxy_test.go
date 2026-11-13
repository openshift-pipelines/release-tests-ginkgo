package multicluster

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestProxyClosePreservesUnexpectedErrorsButAcceptsClosedPeer(t *testing.T) {
	unexpected := errors.New("unexpected close failure")
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"normal", nil, nil},
		{"broken-pipe", fmt.Errorf("TLS closeNotify: %w", syscall.EPIPE), nil},
		{"reset-peer", syscall.ECONNRESET, nil},
		{"already-closed", net.ErrClosed, nil},
		{"unexpected", unexpected, unexpected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a real WebSocket handshake and a controlled close syscall
			// result. Inject only that result: kernel peer-close timing varies.
			server := httptest.NewServer(websocket.Handler(func(conn *websocket.Conn) {
				var data string
				if err := websocket.Message.Receive(conn, &data); err == nil {
					t.Error("client unexpectedly sent a message")
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			config, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http"), server.URL)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", config.Location.Host)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := raw.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
					t.Error(err)
				}
			}()
			deadline, _ := ctx.Deadline()
			if err := raw.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			conn, err := websocket.NewClient(config, &closeResultConn{Conn: raw, result: tc.err})
			if err != nil {
				t.Fatal(err)
			}
			stream := &proxyStream{conn: conn, stop: func() bool { return true }}

			// When teardown follows a completed stream.
			err = stream.close()

			// Then expected peer closure is harmless, but other errors survive.
			if !errors.Is(err, tc.want) {
				t.Fatalf("close=%v want=%v", err, tc.want)
			}
		})
	}
}

type closeResultConn struct {
	net.Conn
	result error
}

func (c *closeResultConn) Close() error {
	return errors.Join(c.Conn.Close(), c.result)
}
