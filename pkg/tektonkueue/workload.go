package tektonkueue

import (
	"context"
	"fmt"
	"strings"
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/apis"
)

const (
	multiKueueController = "kueue.x-k8s.io/multikueue"
	queueLabel           = "kueue.x-k8s.io/queue-name"
	explicitQueueSuffix  = "-explicit"
	workloadImage        = "registry.access.redhat.com/ubi9/ubi-minimal@sha256:34880b64c07f28f64d95737f82f891516de9a3b43583f39970f7bf8e4cfa48b7"
	workloadLogMarker    = "multi-cluster-execution-ok"
	workloadLogContainer = "step-prove-execution"
)

// Result identifies the selected spoke and contains its workload logs.
type Result struct {
	PipelineRun string
	Spoke       string
	Logs        string
}

type execution struct {
	workloadName string
}

// Execute creates one hub PipelineRun and validates its admission and execution on exactly one spoke.
func (e *Environment) Execute(ctx context.Context, name string, prelabelled bool) (*Result, error) {
	run, execution, err := e.createPipelineRun(ctx, name, prelabelled, 90*time.Second)
	if err != nil {
		return nil, err
	}
	execution.workloadName, err = e.waitForHubWorkload(ctx, run)
	if err != nil {
		return nil, err
	}
	spoke, err := e.waitForSpokeExecution(ctx, run.Name)
	if err != nil {
		return nil, err
	}
	if err := e.waitForHubRunning(ctx, run.Name, spoke); err != nil {
		return nil, err
	}
	logs, err := e.waitForCompletion(ctx, run.Name, execution.workloadName, spoke)
	if err != nil {
		return nil, err
	}
	return &Result{PipelineRun: run.Name, Spoke: spoke.Name, Logs: logs}, nil
}

func (e *Environment) createPipelineRun(ctx context.Context, name string, prelabelled bool, duration time.Duration) (*pipelinev1.PipelineRun, *execution, error) {
	run, expectedQueue := e.newPipelineRun(name, prelabelled, duration)
	execution := &execution{}
	var uid types.UID
	e.addCleanup(func(cleanupCtx context.Context) error {
		current, getErr := e.Hub.Clients.PipelineRunClient.Get(cleanupCtx, run.Name, metav1.GetOptions{})
		if getErr == nil {
			if !e.owns(current.Labels) || (uid != "" && current.UID != uid) {
				return fmt.Errorf("refusing to delete replacement PipelineRun %s", run.Name)
			}
			if err := ignoreNotFound(e.Hub.Clients.PipelineRunClient.Delete(cleanupCtx, run.Name, metav1.DeleteOptions{})); err != nil {
				return err
			}
		} else if !apierrors.IsNotFound(getErr) {
			return getErr
		}
		return e.waitForExecutionObjectsGone(cleanupCtx, run.Name, execution.workloadName)
	})
	created, err := e.Hub.Clients.PipelineRunClient.Create(ctx, run, metav1.CreateOptions{})
	if err != nil {
		return nil, execution, fmt.Errorf("create hub PipelineRun: %w", err)
	}
	uid = created.UID
	if err := e.validateHubAdmission(created, expectedQueue); err != nil {
		return nil, execution, err
	}
	return created, execution, nil
}

func (e *Environment) newPipelineRun(name string, prelabelled bool, duration time.Duration) (*pipelinev1.PipelineRun, string) {
	timeout := metav1.Duration{Duration: duration + 10*time.Minute}
	labels := e.ownedLabels()
	expectedQueue := e.Prefix
	if prelabelled {
		expectedQueue = e.explicitQueueName()
		labels[queueLabel] = expectedQueue
	}
	return &pipelinev1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      e.Prefix + "-" + name,
			Namespace: e.Namespace,
			Labels:    labels,
		},
		Spec: pipelinev1.PipelineRunSpec{
			Timeouts:        &pipelinev1.TimeoutFields{Pipeline: &timeout},
			TaskRunTemplate: pipelinev1.PipelineTaskRunTemplate{ServiceAccountName: "default"},
			PipelineSpec: &pipelinev1.PipelineSpec{Tasks: []pipelinev1.PipelineTask{{
				Name: "execute-on-spoke",
				TaskSpec: &pipelinev1.EmbeddedTask{TaskSpec: pipelinev1.TaskSpec{Steps: []pipelinev1.Step{{
					Name:   "prove-execution",
					Image:  workloadImage,
					Script: fmt.Sprintf("#!/bin/sh\necho %s\nsleep %d\n", workloadLogMarker, int(duration.Seconds())),
				}}}},
			}}},
		},
	}, expectedQueue
}

func (e *Environment) explicitQueueName() string {
	return e.Prefix + explicitQueueSuffix
}

func (e *Environment) validateHubAdmission(run *pipelinev1.PipelineRun, queueName string) error {
	if got := run.Labels[queueLabel]; got != queueName {
		return fmt.Errorf("hub PipelineRun %s queue label = %q, want %q", run.Name, got, queueName)
	}
	if run.Spec.ManagedBy == nil || *run.Spec.ManagedBy != multiKueueController {
		return fmt.Errorf("hub PipelineRun %s managedBy was not set to %q", run.Name, multiKueueController)
	}
	if run.Spec.Status != pipelinev1.PipelineRunSpecStatusPending {
		return fmt.Errorf("hub PipelineRun %s spec.status = %q, want %q", run.Name, run.Spec.Status, pipelinev1.PipelineRunSpecStatusPending)
	}
	return nil
}

func (e *Environment) waitForHubWorkload(ctx context.Context, run *pipelinev1.PipelineRun) (string, error) {
	resource := e.Hub.Clients.Dynamic.Resource(workloadGVR).Namespace(e.Namespace)
	var name string
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		list, err := resource.List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, nil
		}
		for i := range list.Items {
			for _, owner := range list.Items[i].GetOwnerReferences() {
				if owner.UID == run.UID {
					name = list.Items[i].GetName()
					return true, nil
				}
			}
		}
		return false, nil
	})
	if err != nil {
		return "", fmt.Errorf("hub Workload was not created for PipelineRun %s: %w", run.Name, err)
	}
	return name, nil
}

func (e *Environment) waitForSpokeExecution(ctx context.Context, runName string) (Cluster, error) {
	seen := map[string]bool{}
	var selected Cluster
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		for _, spoke := range e.Spokes {
			worker, err := spoke.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{})
			if err == nil {
				if worker.Spec.ManagedBy != nil {
					return false, fmt.Errorf("worker PipelineRun %s on %s retained managedBy %q", runName, spoke.Name, *worker.Spec.ManagedBy)
				}
				seen[spoke.Name] = true
				selected = spoke
			} else if !apierrors.IsNotFound(err) {
				return false, err
			}
		}
		if len(seen) > 1 {
			return false, fmt.Errorf("PipelineRun appeared on multiple spokes")
		}
		if len(seen) == 0 {
			return false, nil
		}
		taskRuns, err := selected.Clients.TaskRunClient.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun=" + runName})
		if err != nil {
			return false, err
		}
		return len(taskRuns.Items) > 0, nil
	})
	if err != nil {
		return Cluster{}, fmt.Errorf("PipelineRun %s did not execute on exactly one spoke: %w", runName, err)
	}

	hubTaskRuns, err := e.Hub.Clients.TaskRunClient.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun=" + runName})
	if err != nil {
		return Cluster{}, err
	}
	if len(hubTaskRuns.Items) != 0 {
		return Cluster{}, fmt.Errorf("PipelineRun %s unexpectedly created TaskRuns on the hub", runName)
	}
	return selected, nil
}

func (e *Environment) waitForHubRunning(ctx context.Context, runName string, spoke Cluster) error {
	var hubState, workerState string
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		hub, err := e.Hub.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		worker, err := spoke.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		hubCondition := hub.Status.GetCondition(apis.ConditionSucceeded)
		workerCondition := worker.Status.GetCondition(apis.ConditionSucceeded)
		if hubCondition == nil || workerCondition == nil {
			return false, nil
		}
		hubState = fmt.Sprintf("%s: %s", hubCondition.Reason, hubCondition.Message)
		workerState = fmt.Sprintf("%s: %s", workerCondition.Reason, workerCondition.Message)
		if hubCondition.Status == corev1.ConditionFalse || workerCondition.Status == corev1.ConditionFalse {
			return false, fmt.Errorf("PipelineRun failed before Running was observed (hub=%s, %s=%s)", hubState, spoke.Name, workerState)
		}
		running := pipelinev1.PipelineRunReasonRunning.String()
		return hubCondition.Status == corev1.ConditionUnknown && workerCondition.Status == corev1.ConditionUnknown && hubCondition.Reason == running && workerCondition.Reason == running, nil
	})
	if err != nil {
		return fmt.Errorf("hub PipelineRun %s did not mirror Running status from %s (hub=%s, worker=%s): %w", runName, spoke.Name, hubState, workerState, err)
	}
	return nil
}

func (e *Environment) waitForCompletion(ctx context.Context, runName, workloadName string, spoke Cluster) (string, error) {
	workerObservedSuccess := false
	seen := map[string]bool{spoke.Name: true}
	var hubState, workerState, logs, logState string
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := e.recordSpokes(ctx, runName, seen); err != nil {
			return false, err
		}
		if logs == "" {
			candidate, err := e.pipelineRunLogs(ctx, spoke, runName)
			switch {
			case err != nil:
				logState = err.Error()
			case !strings.Contains(candidate, workloadLogMarker):
				logState = "expected marker not present"
			default:
				logs = candidate
				logState = "captured"
			}
		}

		worker, workerErr := spoke.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{})
		if workerErr == nil {
			terminal, succeeded, state := pipelineRunState(worker)
			workerState = state
			if terminal && !succeeded {
				return false, fmt.Errorf("PipelineRun failed on %s (%s)", spoke.Name, state)
			}
			workerObservedSuccess = workerObservedSuccess || succeeded
		} else if !apierrors.IsNotFound(workerErr) {
			return false, workerErr
		}

		hub, hubErr := e.Hub.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{})
		if hubErr != nil {
			return false, hubErr
		}
		terminal, succeeded, state := pipelineRunState(hub)
		hubState = state
		if terminal && !succeeded {
			return false, fmt.Errorf("hub PipelineRun failed after execution on %s (%s)", spoke.Name, state)
		}
		return succeeded && workerObservedSuccess && logs != "", nil
	})
	if err != nil {
		return "", fmt.Errorf("PipelineRun %s did not complete (hub=%s, %s=%s, logs=%s): %w", runName, hubState, spoke.Name, workerState, logState, err)
	}

	if err := e.waitForWorkloadFinished(ctx, workloadName, runName, seen); err != nil {
		return "", err
	}
	if err := e.waitForWorkerCleanup(ctx, spoke, runName, workloadName, seen); err != nil {
		return "", err
	}

	hubTaskRuns, err := e.Hub.Clients.TaskRunClient.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun=" + runName})
	if err != nil {
		return "", err
	}
	if len(hubTaskRuns.Items) != 0 {
		return "", fmt.Errorf("PipelineRun %s created TaskRuns on the hub", runName)
	}
	return logs, nil
}

func (e *Environment) pipelineRunLogs(ctx context.Context, spoke Cluster, runName string) (string, error) {
	pods, err := spoke.Clients.KubeClient.Kube.CoreV1().Pods(e.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "tekton.dev/pipelineRun=" + runName,
	})
	if err != nil {
		return "", fmt.Errorf("list PipelineRun pods on %s: %w", spoke.Name, err)
	}
	if len(pods.Items) != 1 {
		return "", fmt.Errorf("found %d PipelineRun pods on %s, want 1", len(pods.Items), spoke.Name)
	}
	logs, err := spoke.Clients.KubeClient.Kube.CoreV1().Pods(e.Namespace).GetLogs(
		pods.Items[0].Name,
		&corev1.PodLogOptions{Container: workloadLogContainer},
	).DoRaw(ctx)
	if err != nil {
		return "", fmt.Errorf("read PipelineRun logs on %s: %w", spoke.Name, err)
	}
	return string(logs), nil
}

func (e *Environment) recordSpokes(ctx context.Context, runName string, seen map[string]bool) error {
	for _, candidate := range e.Spokes {
		if _, err := candidate.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{}); err == nil {
			seen[candidate.Name] = true
		} else if !apierrors.IsNotFound(err) {
			return err
		}
	}
	if len(seen) > 1 {
		return fmt.Errorf("PipelineRun appeared on multiple spokes")
	}
	return nil
}

func pipelineRunState(run *pipelinev1.PipelineRun) (terminal, succeeded bool, detail string) {
	condition := run.Status.GetCondition(apis.ConditionSucceeded)
	if condition == nil {
		return false, false, "condition not reported"
	}
	detail = fmt.Sprintf("%s: %s", condition.Reason, condition.Message)
	switch condition.Status {
	case corev1.ConditionTrue:
		return true, true, detail
	case corev1.ConditionFalse:
		return true, false, detail
	default:
		return false, false, detail
	}
}

func (e *Environment) waitForWorkloadFinished(ctx context.Context, workloadName, runName string, seen map[string]bool) error {
	resource := e.Hub.Clients.Dynamic.Resource(workloadGVR).Namespace(e.Namespace)
	var last string
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := e.recordSpokes(ctx, runName, seen); err != nil {
			return false, err
		}
		workload, getErr := resource.Get(ctx, workloadName, metav1.GetOptions{})
		if getErr != nil {
			return false, nil
		}
		finished, detail := conditionTrue(workload, "Finished")
		last = detail
		return finished, nil
	})
	if err != nil {
		return fmt.Errorf("hub Workload %s did not finish (%s): %w", workloadName, last, err)
	}
	return nil
}

func (e *Environment) waitForExecutionObjectsGone(ctx context.Context, runName, workloadName string) error {
	hubWorkloads := e.Hub.Clients.Dynamic.Resource(workloadGVR).Namespace(e.Namespace)
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
		if _, err := e.Hub.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
			return false, ignoreNotFound(err)
		}
		if workloadName != "" {
			if _, err := hubWorkloads.Get(ctx, workloadName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				return false, ignoreNotFound(err)
			}
		}
		for _, spoke := range e.Spokes {
			if _, err := spoke.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
				return false, ignoreNotFound(err)
			}
			if workloadName != "" {
				workloads := spoke.Clients.Dynamic.Resource(workloadGVR).Namespace(e.Namespace)
				if _, err := workloads.Get(ctx, workloadName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
					return false, ignoreNotFound(err)
				}
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("execution objects were not deleted before scheduler restoration: %w", err)
	}
	return nil
}

func (e *Environment) waitForWorkerCleanup(ctx context.Context, spoke Cluster, runName, workloadName string, seen map[string]bool) error {
	workloads := spoke.Clients.Dynamic.Resource(workloadGVR).Namespace(e.Namespace)
	err := wait.PollUntilContextTimeout(ctx, pollInterval, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := e.recordSpokes(ctx, runName, seen); err != nil {
			return false, err
		}
		_, runErr := spoke.Clients.PipelineRunClient.Get(ctx, runName, metav1.GetOptions{})
		_, workloadErr := workloads.Get(ctx, workloadName, metav1.GetOptions{})
		return apierrors.IsNotFound(runErr) && apierrors.IsNotFound(workloadErr), nil
	})
	if err != nil {
		return fmt.Errorf("worker objects were not cleaned from %s: %w", spoke.Name, err)
	}
	return nil
}
