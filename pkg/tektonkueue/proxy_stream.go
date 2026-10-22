package tektonkueue

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func (p *consoleProxy) liveLogs(ctx context.Context, run proxyRun, release func(context.Context, string) error) (result string, resultErr error) {
	pod, err := p.pod(ctx, run)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithDeadline(ctx, p.expires)
	defer cancel()
	dialer := websocket.Dialer{
		Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 30 * time.Second,
		Jar: p.client.Jar,
	}
	// Reuse verified TLS roots, including httptest's private CA in wire tests.
	if transport, ok := p.client.Transport.(*http.Transport); ok {
		dialer.TLSClientConfig = transport.TLSClientConfig
	}
	endpoint := "wss" + strings.TrimPrefix(p.base, "https") + run.logPath(pod.Name)
	conn, response, err := dialer.DialContext(ctx, endpoint, nil)
	if response != nil && response.Body != nil {
		defer func() {
			if err := response.Body.Close(); err != nil && resultErr == nil {
				resultErr = requestError(ctx, "close console upgrade response")
			}
		}()
	}
	if err != nil {
		// Never echo a response body or credential-bearing headers.
		return "", requestError(ctx, "console WebSocket upgrade")
	}
	defer func() {
		if err := conn.Close(); err != nil && resultErr == nil {
			resultErr = requestError(ctx, "close console stream")
		}
	}()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	conn.SetReadLimit(1 << 20)
	deadline, _ := ctx.Deadline()
	if err := conn.SetReadDeadline(deadline); err != nil {
		return "", requestError(ctx, "set console stream deadline")
	}
	var logs strings.Builder
	await := func(marker string) error {
		for !strings.HasPrefix(logs.String(), marker+"\n") && !strings.Contains(logs.String(), "\n"+marker+"\n") {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if !time.Now().Before(deadline) {
					return context.DeadlineExceeded
				}
				var closed *websocket.CloseError
				if errors.As(err, &closed) && closed.Code >= 1000 && closed.Code <= 4999 {
					return fmt.Errorf("console stream closed before marker (code %d)", closed.Code)
				}
				return fmt.Errorf("console stream closed before marker")
			}
			if kind != websocket.TextMessage && kind != websocket.BinaryMessage {
				return fmt.Errorf("unexpected console proxy WebSocket message type %d", kind)
			}
			if logs.Len()+len(data) > 1<<20 {
				return fmt.Errorf("console proxy log stream exceeds one MiB")
			}
			logs.Write(data)
		}
		return nil
	}
	if err := await(workloadLogMarker); err != nil {
		return "", err
	}
	// Generate only after receiving the first marker. Buffered historical logs
	// cannot satisfy this challenge on the same, already-established socket.
	marker := "live-" + rand.Text()
	if err := release(ctx, marker); err != nil {
		return "", err
	}
	if err := await(marker); err != nil {
		return "", err
	}
	return logs.String(), nil
}
