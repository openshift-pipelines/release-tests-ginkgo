package tektonkueue

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecuteRoutingValidatesDelayedTransientPlacementThroughFences(t *testing.T) {
	for _, test := range []struct {
		name      string
		route     string
		injection routingInjection
	}{
		{"remote hub TaskRun", "remote", routingInjection{0, "taskruns"}},
		{"remote second spoke", "remote", routingInjection{2, "pipelineruns"}},
		{"local spoke TaskRun", "local", routingInjection{1, "taskruns"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given: expected success is delivered first. The selected watch's
			// sentinel deletion is held while a transient wrong placement occurs.
			h := newRoutingHarness(t, test.injection)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := h.env.ExecuteRouting(ctx, test.route)
				result <- err
			}()
			if err := awaitRoutingSignal(ctx, h.fence); err != nil {
				t.Fatal(err)
			}
			// When: both create and delete precede the producer fence. The final
			// API snapshot has no such object, but history must still reject it.
			if test.injection.resource == "pipelineruns" {
				run := successfulRoutingRun()
				run.Name = h.run.Name
				if err := h.put(test.injection.cluster, run); err != nil {
					t.Fatal(err)
				}
				if err := h.drop(test.injection.cluster, "pipelineruns", run.Name); err != nil {
					t.Fatal(err)
				}
			} else {
				task := successfulRoutingTask()
				task.Labels = map[string]string{"tekton.dev/pipelineRun": h.run.Name}
				if err := h.put(test.injection.cluster, task); err != nil {
					t.Fatal(err)
				}
				if err := h.drop(test.injection.cluster, "taskruns", task.Name); err != nil {
					t.Fatal(err)
				}
			}
			close(h.release)
			// Then
			select {
			case err := <-result:
				if !errors.Is(err, errRoutingPlacement) {
					t.Fatalf("transient placement error = %v, want placement rejection", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func TestExecuteRoutingRetainsImmediatelyDeletedTerminalObjects(t *testing.T) {
	for _, route := range []string{"remote", "explicit", "local", "missing"} {
		t.Run(route, func(t *testing.T) {
			// Given: the harness emits Workload create/Finished/delete and
			// terminal worker create/delete before returning from hub Create.
			h := newRoutingHarness(t, routingInjection{cluster: -1})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			// When
			selected, err := h.env.ExecuteRouting(ctx, route)
			// Then
			if err != nil {
				t.Fatal(err)
			}
			want := "hub"
			if route == "remote" || route == "explicit" {
				want = "spoke-1"
			}
			if selected != want {
				t.Fatalf("selected %q, want %q", selected, want)
			}
			for _, stream := range h.watches {
				if err := awaitRoutingSignal(ctx, stream.stopped); err != nil {
					t.Fatal("producer was not joined:", err)
				}
			}
		})
	}
}

func TestExecuteRoutingCancellationStopsEveryProducer(t *testing.T) {
	// Given: no terminal events will arrive.
	h := newRoutingHarness(t, routingInjection{cluster: -1})
	h.immediate = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bounded, finish := context.WithTimeout(context.Background(), 10*time.Second)
	defer finish()
	result := make(chan error, 1)
	go func() {
		_, err := h.env.ExecuteRouting(ctx, "remote")
		result <- err
	}()
	if err := awaitRoutingSignal(bounded, h.created); err != nil {
		t.Fatal(err)
	}
	// When
	cancel()
	// Then
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
	case <-bounded.Done():
		t.Fatal(bounded.Err())
	}
	for _, stream := range h.watches {
		if err := awaitRoutingSignal(bounded, stream.stopped); err != nil {
			t.Fatal("cancellation leaked a producer:", err)
		}
	}
}
