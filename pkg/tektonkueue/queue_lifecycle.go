package tektonkueue

import (
	"context"
	"fmt"
	"strings"
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

const lifecycleRunDuration = 10 * time.Minute

type lifecycleRun struct {
	run       *pipelinev1.PipelineRun
	execution *execution
}

// ExerciseQueueLifecycle validates queue limits, unavailable workers, cancellation, and draining.
func (e *Environment) ExerciseQueueLifecycle(ctx context.Context) error {
	if err := e.exerciseQuotaAndCancellation(ctx); err != nil {
		return fmt.Errorf("quota and cancellation: %w", err)
	}
	if err := e.exerciseUnavailableWorkers(ctx); err != nil {
		return fmt.Errorf("unavailable workers: %w", err)
	}
	if err := e.exerciseQueueDrain(ctx, false); err != nil {
		return fmt.Errorf("spoke queue drain: %w", err)
	}
	if err := e.exerciseQueueDrain(ctx, true); err != nil {
		return fmt.Errorf("hub queue drain: %w", err)
	}
	return nil
}

func (e *Environment) exerciseQuotaAndCancellation(ctx context.Context) error {
	runs := make([]*lifecycleRun, 3)
	var err error
	runs[0], err = e.createLifecycleRun(ctx, "quota-1")
	if err != nil {
		return err
	}
	firstSpoke, err := e.waitForLifecycleRun(ctx, runs[0])
	if err != nil {
		return err
	}
	for i := 1; i < len(runs); i++ {
		runs[i], err = e.createLifecycleRun(ctx, fmt.Sprintf("quota-%d", i+1))
		if err != nil {
			return err
		}
	}

	for i := 0; i < 2; i++ {
		if err := e.waitForWorkloadCondition(ctx, e.Hub, runs[i].execution.workloadName, "QuotaReserved", "True", ""); err != nil {
			return err
		}
	}
	if err := e.waitForWorkloadCondition(ctx, e.Hub, runs[2].execution.workloadName, "QuotaReserved", "False", ""); err != nil {
		return err
	}
	if err := e.ensureWorkloadConditionNotTrue(ctx, e.Hub, runs[2].execution.workloadName, "QuotaReserved", 15*time.Second); err != nil {
		return err
	}
	if err := e.ensurePendingWithoutWorkers(ctx, runs[2].run.Name, 15*time.Second); err != nil {
		return err
	}
	active := len(e.Spokes)
	if active > 2 {
		active = 2
	}
	if err := e.waitForWorkerExecutionCount(ctx, pipelineRunNames(runs), active); err != nil {
		return err
	}

	if err := e.cancelWorkerPipelineRun(ctx, firstSpoke, runs[0].run.Name); err != nil {
		return err
	}
	if err := e.waitForFailedCompletion(ctx, runs[0], firstSpoke); err != nil {
		return err
	}

	if _, err := e.waitForLifecycleRun(ctx, runs[1]); err != nil {
		return err
	}
	if err := e.deleteHubExecution(ctx, runs[1]); err != nil {
		return err
	}
	if _, err := e.waitForLifecycleRun(ctx, runs[2]); err != nil {
		return err
	}
	return e.deleteHubExecution(ctx, runs[2])
}

func (e *Environment) exerciseUnavailableWorkers(ctx context.Context) error {
	restores := make([]cleanupFunc, 0, len(e.Spokes))
	for _, spoke := range e.Spokes {
		restore, err := e.stopLocalQueue(ctx, spoke, queueStopPolicyHold)
		if err != nil {
			return err
		}
		restores = append(restores, restore)
	}

	run, err := e.createLifecycleRun(ctx, "no-workers")
	if err != nil {
		return err
	}
	if err := e.waitForWorkloadCondition(ctx, e.Hub, run.execution.workloadName, "QuotaReserved", "True", ""); err != nil {
		return err
	}
	if err := e.waitForAdmissionCheckState(ctx, run.execution.workloadName, "Pending"); err != nil {
		return err
	}
	if err := e.ensurePendingWithoutWorkers(ctx, run.run.Name, 15*time.Second); err != nil {
		return err
	}
	if err := restoreQueues(ctx, restores); err != nil {
		return err
	}
	if _, err := e.waitForLifecycleRun(ctx, run); err != nil {
		return err
	}
	return e.deleteHubExecution(ctx, run)
}

func (e *Environment) exerciseQueueDrain(ctx context.Context, hub bool) error {
	name := "spoke-drain"
	if hub {
		name = "hub-drain"
	}
	run, err := e.createLifecycleRun(ctx, name)
	if err != nil {
		return err
	}
	spoke, err := e.waitForLifecycleRun(ctx, run)
	if err != nil {
		return err
	}
	var otherRestores []cleanupFunc
	if !hub {
		for _, candidate := range e.Spokes {
			if candidate.Name == spoke.Name {
				continue
			}
			restore, err := e.stopLocalQueue(ctx, candidate, queueStopPolicyHold)
			if err != nil {
				return err
			}
			otherRestores = append(otherRestores, restore)
		}
	}
	target := spoke
	if hub {
		target = e.Hub
	}
	restore, err := e.stopLocalQueue(ctx, target, queueStopPolicyHoldAndDrain)
	if err != nil {
		return err
	}
	if hub {
		if err := e.waitForWorkloadCondition(ctx, target, run.execution.workloadName, "Evicted", "True", "LocalQueueStopped"); err != nil {
			return err
		}
	} else if err := e.waitForAdmissionCheckState(ctx, run.execution.workloadName, "Retry"); err != nil {
		return err
	}
	if err := e.waitForWorkerStopped(ctx, spoke, run.run.Name); err != nil {
		return err
	}
	if err := e.deleteHubExecution(ctx, run); err != nil {
		return err
	}
	if err := restore(ctx); err != nil {
		return err
	}
	return restoreQueues(ctx, otherRestores)
}

func (e *Environment) createLifecycleRun(ctx context.Context, name string) (*lifecycleRun, error) {
	run, execution, err := e.createPipelineRun(ctx, name, false, lifecycleRunDuration)
	if err != nil {
		return nil, err
	}
	execution.workloadName, err = e.waitForHubWorkload(ctx, run)
	if err != nil {
		return nil, err
	}
	return &lifecycleRun{run: run, execution: execution}, nil
}

func (e *Environment) waitForLifecycleRun(ctx context.Context, run *lifecycleRun) (Cluster, error) {
	spoke, err := e.waitForSpokeExecution(ctx, run.run.Name)
	if err != nil {
		return Cluster{}, err
	}
	if err := e.waitForHubRunning(ctx, run.run.Name, spoke); err != nil {
		return Cluster{}, err
	}
	return spoke, nil
}

func pipelineRunNames(runs []*lifecycleRun) []string {
	names := make([]string, len(runs))
	for i := range runs {
		names[i] = runs[i].run.Name
	}
	return names
}

func (e *Environment) waitForWorkloadCondition(ctx context.Context, cluster Cluster, workloadName, conditionType, status, reason string) error {
	resource := cluster.Clients.Dynamic.Resource(workloadGVR).Namespace(e.Namespace)
	var last string
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		workload, err := resource.Get(ctx, workloadName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		gotStatus, detail, found := conditionStatus(workload, conditionType)
		last = detail
		return found && gotStatus == status && (reason == "" || strings.HasPrefix(detail, reason+":")), nil
	})
	if err != nil {
		return fmt.Errorf("workload %s on %s did not report %s=%s (%s): %w", workloadName, cluster.Name, conditionType, status, last, err)
	}
	return nil
}

func (e *Environment) waitForAdmissionCheckState(ctx context.Context, workloadName, state string) error {
	resource := e.Hub.Clients.Dynamic.Resource(workloadGVR).Namespace(e.Namespace)
	var last string
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		workload, err := resource.Get(ctx, workloadName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		checks, _, err := unstructured.NestedSlice(workload.Object, "status", "admissionChecks")
		if err != nil {
			return false, err
		}
		for _, item := range checks {
			check, ok := item.(map[string]any)
			if !ok || check["name"] != e.Prefix {
				continue
			}
			last = fmt.Sprint(check["state"])
			return last == state, nil
		}
		last = "not reported"
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("admission check for workload %s did not reach %s (last=%s): %w", workloadName, state, last, err)
	}
	return nil
}

func (e *Environment) ensureWorkloadConditionNotTrue(ctx context.Context, cluster Cluster, workloadName, conditionType string, duration time.Duration) error {
	resource := cluster.Clients.Dynamic.Resource(workloadGVR).Namespace(e.Namespace)
	return consistently(ctx, duration, func(ctx context.Context) error {
		workload, err := resource.Get(ctx, workloadName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if status, detail, _ := conditionStatus(workload, conditionType); status == "True" {
			return fmt.Errorf("workload %s on %s unexpectedly reported %s=True (%s)", workloadName, cluster.Name, conditionType, detail)
		}
		return nil
	})
}

func (e *Environment) waitForWorkerExecutionCount(ctx context.Context, runNames []string, want int) error {
	var last string
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		total := 0
		counts := make([]string, 0, len(e.Spokes))
		for _, spoke := range e.Spokes {
			count := 0
			for _, runName := range runNames {
				worker, err := spoke.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{})
				if err == nil {
					if worker.Spec.ManagedBy != nil {
						return false, fmt.Errorf("worker PipelineRun %s on %s retained managedBy %q", runName, spoke.Name, *worker.Spec.ManagedBy)
					}
					count++
				} else if !apierrors.IsNotFound(err) {
					return false, err
				}
			}
			if count > 1 {
				return false, fmt.Errorf("spoke %s ran %d PipelineRuns with capacity for one", spoke.Name, count)
			}
			total += count
			counts = append(counts, fmt.Sprintf("%s=%d", spoke.Name, count))
		}
		last = fmt.Sprintf("total=%d, %s", total, strings.Join(counts, ", "))
		if total > want {
			return false, fmt.Errorf("worker execution count exceeded quota: %s, want at most %d", last, want)
		}
		return total == want, nil
	})
	if err != nil {
		return fmt.Errorf("worker execution count did not reach %d (%s): %w", want, last, err)
	}
	return nil
}

func (e *Environment) ensurePendingWithoutWorkers(ctx context.Context, runName string, duration time.Duration) error {
	return consistently(ctx, duration, func(ctx context.Context) error {
		hub, err := e.Hub.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if hub.Spec.Status != pipelinev1.PipelineRunSpecStatusPending {
			return fmt.Errorf("hub PipelineRun %s spec.status = %q while pending", runName, hub.Spec.Status)
		}
		if terminal, _, state := pipelineRunState(hub); terminal {
			return fmt.Errorf("hub PipelineRun %s became terminal while pending (%s)", runName, state)
		}
		taskRuns, err := e.Hub.Clients.TaskRunClient.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun=" + runName})
		if err != nil {
			return err
		}
		if len(taskRuns.Items) != 0 {
			return fmt.Errorf("pending PipelineRun %s created %d hub TaskRuns", runName, len(taskRuns.Items))
		}
		for _, spoke := range e.Spokes {
			if _, err := spoke.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{}); err == nil {
				return fmt.Errorf("pending PipelineRun %s unexpectedly appeared on %s", runName, spoke.Name)
			} else if !apierrors.IsNotFound(err) {
				return err
			}
		}
		return nil
	})
}

func consistently(ctx context.Context, duration time.Duration, check func(context.Context) error) error {
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if err := check(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return nil
		case <-ticker.C:
		}
	}
}

func (e *Environment) cancelWorkerPipelineRun(ctx context.Context, spoke Cluster, runName string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		run, err := spoke.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		run.Spec.Status = pipelinev1.PipelineRunSpecStatusCancelled
		_, err = spoke.Clients.PipelineRunClient.Update(ctx, run, metav1.UpdateOptions{})
		return err
	})
}

func (e *Environment) waitForFailedCompletion(ctx context.Context, run *lifecycleRun, spoke Cluster) error {
	var last string
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		hub, err := e.Hub.Clients.PipelineRunClient.Get(ctx, run.run.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		terminal, succeeded, state := pipelineRunState(hub)
		last = state
		if terminal && succeeded {
			return false, fmt.Errorf("canceled PipelineRun %s unexpectedly succeeded", run.run.Name)
		}
		return terminal, nil
	})
	if err != nil {
		return fmt.Errorf("hub did not report worker cancellation for PipelineRun %s (%s): %w", run.run.Name, last, err)
	}
	seen := map[string]bool{spoke.Name: true}
	if err := e.waitForWorkloadFinished(ctx, run.execution.workloadName, run.run.Name, seen); err != nil {
		return err
	}
	return e.waitForWorkerCleanup(ctx, spoke, run.run.Name, run.execution.workloadName, seen)
}

func (e *Environment) waitForWorkerStopped(ctx context.Context, spoke Cluster, runName string) error {
	var last string
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		run, err := spoke.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			last = "deleted"
			return true, nil
		}
		if err != nil {
			return false, err
		}
		terminal, succeeded, state := pipelineRunState(run)
		last = state
		if terminal && succeeded {
			return false, fmt.Errorf("drained PipelineRun %s unexpectedly succeeded on %s", runName, spoke.Name)
		}
		return terminal, nil
	})
	if err != nil {
		return fmt.Errorf("PipelineRun %s did not stop on %s (%s): %w", runName, spoke.Name, last, err)
	}
	return nil
}

func (e *Environment) deleteHubExecution(ctx context.Context, run *lifecycleRun) error {
	current, err := e.Hub.Clients.PipelineRunClient.Get(ctx, run.run.Name, metav1.GetOptions{})
	if err == nil {
		if !e.owns(current.Labels) || current.UID != run.run.UID {
			return fmt.Errorf("refusing to delete replacement PipelineRun %s", run.run.Name)
		}
		if err := e.Hub.Clients.PipelineRunClient.Delete(ctx, run.run.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	return e.waitForExecutionObjectsGone(ctx, run.run.Name, run.execution.workloadName)
}

func restoreQueues(ctx context.Context, restores []cleanupFunc) error {
	for i := len(restores) - 1; i >= 0; i-- {
		if err := restores[i](ctx); err != nil {
			return err
		}
	}
	return nil
}
