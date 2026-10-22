package multicluster

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"

	"golang.org/x/net/websocket"
)

// proxyStream is deliberately transport-only: no cluster, port-forward, or UI
// lifecycle. OpenShift callers can supply a trusted route instead of a tunnel.
type proxyStream struct {
	conn *websocket.Conn
	stop func() bool
}

func openProxyStream(ctx context.Context, request LogRequest) (*proxyStream, error) {
	endpoint, err := url.Parse(request.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse proxy endpoint: %w", err)
	}
	if endpoint.Scheme != "wss" || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return nil, errors.New("proxy endpoint must be a wss origin without credentials, path, query, or fragment")
	}
	if request.Token == "" || strings.ContainsAny(request.Token, "\r\n") {
		return nil, errors.New("a bearer token without line breaks is required")
	}
	for _, part := range []string{request.Namespace, request.PipelineRun, request.Pod, request.Container} {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/?#\r\n") {
			return nil, errors.New("namespace, PipelineRun, pod, and container must be nonempty resource names")
		}
	}
	origin := "https://" + endpoint.Host
	endpoint.Path = "/api/v1/namespaces/" + request.Namespace + "/logs/stream"
	endpoint.RawQuery = url.Values{
		"pipelineRun": {request.PipelineRun}, "pod": {request.Pod}, "container": {request.Container},
	}.Encode()
	config, err := websocket.NewConfig(endpoint.String(), origin)
	if err != nil {
		return nil, fmt.Errorf("configure proxy WebSocket: %w", err)
	}
	config.Header = http.Header{"Authorization": {"Bearer " + request.Token}}
	config.TlsConfig = request.TLS
	conn, err := config.DialContext(ctx)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("connect proxy WebSocket: %w", err), ctx.Err())
	}
	conn.MaxPayloadBytes = 64 * 1024
	// Closing the transport interrupts a blocked receive on cancellation.
	stop := context.AfterFunc(ctx, func() {
		// The peer may have closed first. The receive reports the useful error.
		if closeErr := conn.Close(); closeErr != nil {
			return
		}
	})
	return &proxyStream{conn: conn, stop: stop}, nil
}

func (s *proxyStream) close() error {
	s.stop()
	err := s.conn.Close()
	// The proxy can finish its stream before the client sends a TLS closeNotify.
	// These peer-close results still release the socket and cannot invalidate
	// bytes already received. Receive errors and other close errors still fail.
	if errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
		return nil
	}
	return err
}

func (s *proxyStream) receive() (string, error) {
	var data string
	if err := websocket.Message.Receive(s.conn, &data); err != nil {
		return "", fmt.Errorf("receive proxy log frame: %w", err)
	}
	return data, nil
}
