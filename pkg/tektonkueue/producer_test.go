package tektonkueue

import (
	"bufio"
	"context"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProducerBlocksOnBothGatesAndEchoesOnlyTheReleasedChallenge(t *testing.T) {
	// Given the shipped producer with its container-local paths isolated per test.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	root := filepath.Join(t.TempDir(), "rtg-")
	command := exec.CommandContext(ctx, "sh", "-c", strings.ReplaceAll(producerScript, "/tmp/rtg-", root))
	stdout, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdout.Close() }()
	defer func() { _ = writer.Close() }()
	command.Stdout = writer
	lines := make(chan string)
	scanned := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				scanned <- ctx.Err()
				return
			}
		}
		scanned <- scanner.Err()
	}()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait(); close(done) }()
	t.Cleanup(func() {
		cancel()
		cleanupCtx, stop := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer stop()
		_ = receiveReadinessEvent(t, cleanupCtx, done) // cancellation may kill a still-running shell
	})
	if line := receiveReadinessEvent(t, ctx, lines); line != workloadLogMarker {
		t.Fatalf("ready marker = %q", line)
	}
	marker := "challenge-" + rand.Text()
	// When the reader has observed readiness and then releases each FIFO.
	for _, gate := range []string{"gate1", "gate2"} {
		write := exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' "$1" > "$2"`, "release", marker, root+gate)
		if err := write.Run(); err != nil {
			t.Fatal(err)
		}
		if gate == "gate1" {
			if line := receiveReadinessEvent(t, ctx, lines); line != marker {
				t.Fatalf("live marker = %q", line)
			}
		}
	}
	// Then the process can finish only after both blocking reads were satisfied.
	if err := receiveReadinessEvent(t, ctx, done); err != nil {
		t.Fatal(err)
	}
	if err := receiveReadinessEvent(t, ctx, scanned); err != nil {
		t.Fatal(err)
	}
}
