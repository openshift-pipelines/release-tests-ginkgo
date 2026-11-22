package multicluster

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
)

// LogRequest identifies one authenticated hub-proxy log stream. TLS must verify
// the endpoint; a port-forward can use ServerName for the service certificate.
// Callers own token issuance, authorization, and endpoint lifecycle.
type LogRequest struct {
	Endpoint    string
	Token       string
	TLS         *tls.Config
	Namespace   string
	PipelineRun string
	Pod         string
	Container   string
	ReadyMarker string
	LiveMarker  string
}

// VerifyLiveLogs connects to the hub WebSocket, observes the workload's ready
// line, then invokes release to make the running workload emit LiveMarker.
// Only a subsequent complete log line satisfies the proof. Frames may split
// lines arbitrarily. A deadline is mandatory and cancellation interrupts reads.
func VerifyLiveLogs(ctx context.Context, request LogRequest, release func(context.Context) error) (err error) {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("live log verification requires a context deadline")
	}
	if request.ReadyMarker == "" || request.LiveMarker == "" ||
		request.ReadyMarker == request.LiveMarker ||
		strings.ContainsAny(request.ReadyMarker+request.LiveMarker, "\r\n") || release == nil {
		return errors.New("distinct single-line markers and a release action are required")
	}
	if request.TLS != nil && request.TLS.InsecureSkipVerify {
		return errors.New("proxy TLS verification cannot be disabled")
	}
	stream, err := openProxyStream(ctx, request)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := stream.close()
		// Cancellation already closes the socket to unblock reads.
		if ctx.Err() == nil {
			err = errors.Join(err, closeErr)
		}
	}()

	var buffered string
	released := false
	for {
		frame, readErr := stream.receive()
		if readErr != nil {
			return errors.Join(readErr, ctx.Err())
		}
		buffered += frame
		if len(buffered) > 1024*1024 {
			return errors.New("proxy log line exceeded one MiB")
		}
		for {
			line, remaining, found := strings.Cut(buffered, "\n")
			if !found {
				break
			}
			buffered = remaining
			line = strings.TrimSuffix(line, "\r")
			switch line {
			case request.LiveMarker:
				if !released {
					return errors.New("live marker arrived before workload release")
				}
				return nil
			case request.ReadyMarker:
				if released {
					continue
				}
				// Reject a complete marker already buffered before release.
				// A unique per-run marker and a blocked workload form the causal
				// contract; release must only emit the marker after this call.
				if strings.Contains(buffered, request.LiveMarker) {
					return errors.New("live marker was buffered before workload release")
				}
				if err := release(ctx); err != nil {
					return fmt.Errorf("release live workload: %w", err)
				}
				released = true
			}
		}
	}
}
