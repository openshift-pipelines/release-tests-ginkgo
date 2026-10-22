package tektonkueue

import (
	"context"
	"fmt"
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const routeLabel = "release-tests.tekton.dev/route"

type routingExpectation struct {
	queue  string
	remote bool
}

func (e *Environment) newRoutingRun(route string) (*pipelinev1.PipelineRun, routingExpectation, error) {
	run, _ := e.newPipelineRun("cel-"+route, false)
	// Completion is observed from watches; keeping a workload alive with sleep
	// would hide races in remote status propagation and worker garbage collection.
	run.Spec.PipelineSpec.Tasks[0].TaskSpec.Steps[0].Script = "#!/bin/sh\necho " + workloadLogMarker + "\n"
	expected := routingExpectation{queue: e.Prefix + "-local"}
	switch route {
	case "remote":
		run.Labels[routeLabel] = route
		expected = routingExpectation{queue: e.Prefix, remote: true}
	case "explicit":
		run.Labels[routeLabel] = route
		// Prove multiKueue(queue) overrides a conflicting input queue, rather
		// than merely preserving a label already equal to its output.
		run.Labels[queueLabel] = e.Prefix + "-local"
		expected = routingExpectation{queue: e.explicitQueueName(), remote: true}
	case "local":
		run.Labels[routeLabel] = route
	case "missing":
		// The route label must be absent, not an empty string.
	default:
		return nil, routingExpectation{}, fmt.Errorf("unknown routing scenario %q", route)
	}
	return run, expected, nil
}

func validateRoutingAdmission(run *pipelinev1.PipelineRun, expected routingExpectation) error {
	if run.Labels[queueLabel] != expected.queue {
		return fmt.Errorf("admitted queue = %q, want %q", run.Labels[queueLabel], expected.queue)
	}
	if expected.remote {
		if run.Spec.ManagedBy == nil || *run.Spec.ManagedBy != multiKueueController {
			return fmt.Errorf("admitted remote managedBy = %v, want %s", run.Spec.ManagedBy, multiKueueController)
		}
	} else if run.Spec.ManagedBy != nil && *run.Spec.ManagedBy != "tekton.dev/pipeline" {
		return fmt.Errorf("admitted local managedBy = %q, want default Tekton controller", *run.Spec.ManagedBy)
	}
	if run.Spec.Status != pipelinev1.PipelineRunSpecStatusPending {
		return fmt.Errorf("admitted spec.status = %q, want Pending", run.Spec.Status)
	}
	return nil
}

// ExecuteRouting proves CEL admission and actual TaskRun placement for one scenario.
// It observes short-lived workers before creation, including terminal delete events.
func (e *Environment) ExecuteRouting(ctx context.Context, route string) (string, error) {
	run, expected, err := e.newRoutingRun(route)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	observer, err := e.observeRouting(ctx, run.Name)
	if err != nil {
		return "", err
	}
	defer observer.stop()
	e.addCleanup(func(cleanupCtx context.Context) error {
		if err := e.deleteRoutingRun(cleanupCtx, run); err != nil {
			return err
		}
		return e.waitForRoutingDeletion(cleanupCtx, run.Name)
	})
	created, err := e.Hub.Clients.PipelineRunClient.Create(ctx, run, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create CEL %s PipelineRun (scheduler must support multiKueue CEL helpers): %w", route, err)
	}
	run.UID = created.UID
	observer.mu.Lock()
	observer.runUID = created.UID
	observer.mu.Unlock()
	if err := validateRoutingAdmission(created, expected); err != nil {
		return "", err
	}
	selected := 0
	if err := observer.wait(ctx, func() bool {
		index, done, err := observer.state.placement(expected.remote)
		if err != nil {
			observer.err = err
		}
		selected = index
		return done && observer.finished
	}); err != nil {
		return "", fmt.Errorf("CEL %s execution: %w", route, err)
	}
	if err := e.deleteRoutingRun(ctx, run); err != nil {
		return "", err
	}
	if err := observer.finish(ctx); err != nil {
		return "", err
	}
	// finish has joined every producer after its ordered deletion fence.
	if _, _, err := observer.state.placement(expected.remote); err != nil {
		return "", err
	}
	return e.clusters()[selected].Name, nil
}
