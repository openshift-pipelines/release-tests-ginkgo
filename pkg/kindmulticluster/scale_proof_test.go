package kindmulticluster

import (
	"io"
	"testing"
)

func TestAwaitProducerReadyWaitsForCausalMarker(t *testing.T) {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- awaitProducerReady(reader) }()

	select {
	case err := <-done:
		t.Fatalf("readiness returned before FIFO marker: %v", err)
	default:
	}
	if _, err := writer.Write([]byte("container-started\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("unrelated log line released producer: %v", err)
	default:
	}
	if _, err := writer.Write([]byte(readyLine + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}
