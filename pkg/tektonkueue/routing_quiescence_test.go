package tektonkueue

import (
	"context"
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func TestExecuteRoutingRejectsPlacementQueuedAfterNamespaceFence(t *testing.T) {
	// Given: pause hub TaskRun delivery after its own namespace marker is gone.
	// A different namespace/stream remains outstanding during the late writes.
	h := newRoutingHarness(t, routingInjection{cluster: -1})
	h.pause = &routingWatchPause{output: make(chan watch.Event), fenced: make(chan struct{}),
		release: make(chan struct{}), done: make(chan struct{}), stopped: make(chan struct{})}
	h.namespaceBlocked, h.namespaceRelease = make(chan struct{}), make(chan struct{})
	h.finalFences = make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-h.namespaceRelease:
		default:
			close(h.namespaceRelease)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := h.env.ExecuteRouting(ctx, "remote")
		result <- err
	}()
	if err := awaitRoutingSignal(ctx, h.pause.fenced); err != nil {
		t.Fatal(err)
	}
	if err := awaitRoutingSignal(ctx, h.namespaceBlocked); err != nil {
		t.Fatal(err)
	}
	// When: queue wrong-cluster history behind the already consumed fence,
	// without allowing that producer to consume it before other streams finish.
	task := successfulRoutingTask()
	task.Labels = map[string]string{"tekton.dev/pipelineRun": h.run.Name}
	if err := h.put(0, task); err != nil {
		t.Fatal(err)
	}
	if err := h.drop(0, "taskruns", task.Name); err != nil {
		t.Fatal(err)
	}
	close(h.namespaceRelease)
	if err := awaitRoutingSignal(ctx, h.finalFences); err != nil {
		t.Fatal(err)
	}
	// Every final fence has been issued. Cancellation cannot replace consumption
	// of the paused stream's history and its post-quiescence fence.
	close(h.pause.release)
	// Then
	select {
	case err := <-result:
		if !errors.Is(err, errRoutingPlacement) {
			t.Fatalf("post-namespace-fence placement = %v, want placement rejection", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestRoutingNamespaceQuiescencePreservesReplacementAndForeignNamespaces(t *testing.T) {
	for _, change := range []string{"UID", "labels"} {
		t.Run(change, func(t *testing.T) {
			// Given: a namespace no longer owned by this environment.
			h := newRoutingHarness(t, routingInjection{cluster: -1})
			resource := h.apis[0].Resource(routingNamespaceGVR)
			namespace, err := resource.Get(t.Context(), "test", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if change == "UID" {
				namespace.SetUID("replacement")
			} else {
				namespace.SetLabels(map[string]string{"owner": "foreign"})
			}
			if _, err := resource.Update(t.Context(), namespace, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			// When
			err = h.env.quiesceRoutingNamespaces(t.Context())
			// Then
			if err == nil {
				t.Fatal("namespace teardown accepted foreign ownership")
			}
			if _, err := resource.Get(t.Context(), "test", metav1.GetOptions{}); err != nil {
				t.Fatal("foreign namespace was deleted:", err)
			}
		})
	}
}

func TestExecuteRoutingNamespaceTeardownAllowsDuplicateEnvironmentCleanup(t *testing.T) {
	// Given
	h := newRoutingHarness(t, routingInjection{cluster: -1})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := h.env.ExecuteRouting(ctx, "local"); err != nil {
		t.Fatal(err)
	}
	// When: normal deferred cleanup follows the execution's namespace teardown.
	for range 2 {
		if err := h.env.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Then: execution and observation namespaces are both gone everywhere.
	for _, api := range h.apis {
		for _, name := range []string{"test", "test-observation"} {
			if _, err := api.Resource(routingNamespaceGVR).Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				t.Fatalf("namespace %s remained: %v", name, err)
			}
		}
	}
}
