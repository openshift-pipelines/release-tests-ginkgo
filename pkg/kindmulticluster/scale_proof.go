package kindmulticluster

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os/exec"

	corev1 "k8s.io/api/core/v1"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/multicluster"
)

func (e *scaleEnvironment) prove(ctx context.Context, run *scaleRun) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(run.ctx, cancel)
	defer stop()
	placement, err := run.wait(ctx, runDispatched)
	if err != nil {
		return fmt.Errorf("dispatch %s: %w", run.name, err)
	}
	spoke := e.clusters[placement.worker+1]
	marker := "live-" + rand.Text()
	release := func(ctx context.Context) error {
		command := exec.CommandContext(ctx, "kubectl", "--kubeconfig", spoke.path, "--request-timeout=60s",
			"-n", namespace, "exec", placement.pod, "-c", container, "--",
			"sh", "-c", `printf '%s\n' "$1" > /tmp/release-gate`, "sh", marker)
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("release %s on %s (%s): %w", run.name, spoke.name, output, err)
		}
		return nil
	}
	if run.target >= 0 {
		// Only the deterministic representative on each spoke opens a stream.
		err = withProxy(ctx, e.clusters[0].clients, func(ctx context.Context, endpoint proxyEndpoint) error {
			return multicluster.VerifyLiveLogs(ctx, multicluster.LogRequest{
				Endpoint: endpoint.address, Token: endpoint.token, TLS: endpoint.tls,
				Namespace: namespace, PipelineRun: run.name, Pod: placement.pod, Container: container,
				ReadyMarker: readyLine, LiveMarker: marker,
			}, release)
		})
	} else {
		stream, streamErr := spoke.clients.KubeClient.Kube.CoreV1().Pods(namespace).GetLogs(
			placement.pod, &corev1.PodLogOptions{Container: container, Follow: true},
		).Stream(ctx)
		if streamErr != nil {
			return fmt.Errorf("subscribe to producer readiness for %s: %w", run.name, streamErr)
		}
		if readyErr := awaitProducerReady(stream); readyErr != nil {
			_ = stream.Close()
			return fmt.Errorf("observe producer readiness for %s: %w", run.name, readyErr)
		}
		if closeErr := stream.Close(); closeErr != nil {
			return fmt.Errorf("close producer readiness stream for %s: %w", run.name, closeErr)
		}
		err = release(ctx)
	}
	if err != nil {
		_, _, observationErr := run.progress(runFinished)
		return errors.Join(err, observationErr)
	}
	if _, err := run.wait(ctx, runFinished); err != nil {
		return fmt.Errorf("completion %s: %w", run.name, err)
	}
	run.verified = true
	return nil
}

func awaitProducerReady(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if scanner.Text() == readyLine {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return errors.New("producer log stream closed before readiness")
}
