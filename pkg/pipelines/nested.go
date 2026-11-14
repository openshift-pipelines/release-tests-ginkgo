package pipelines

import (
	"context"
	"fmt"

	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	typed "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/apis"
)

// ReadNestedWatch returns the named PipelineRun on success or failure, or a watch error.
func ReadNestedWatch(ctx context.Context, events <-chan watch.Event, name string) (*v1.PipelineRun, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("watch PipelineRun %s: %w", name, ctx.Err())
		case event, open := <-events:
			if !open {
				return nil, fmt.Errorf("PipelineRun %s: %w", name, ErrNestedWatchClosed)
			}
			if event.Type == watch.Error {
				return nil, fmt.Errorf("watch PipelineRun %s: %w", name, apierrors.FromObject(event.Object))
			}
			run, ok := event.Object.(*v1.PipelineRun)
			if !ok || run.Name != name {
				continue
			}
			if event.Type == watch.Deleted {
				return nil, fmt.Errorf("PipelineRun %s deleted before completion", name)
			}
			condition := run.Status.GetCondition(apis.ConditionSucceeded)
			if condition != nil && (condition.Status == corev1.ConditionTrue || condition.Status == corev1.ConditionFalse) {
				return run, nil
			}
		}
	}
}

// NestedChild finds one immediate child and validates the parent's published reference.
func NestedChild(ctx context.Context, client typed.PipelineRunInterface, parent *v1.PipelineRun, task string) (*v1.PipelineRun, error) {
	list, err := client.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var child *v1.PipelineRun
	for i := range list.Items {
		candidate := &list.Items[i]
		owner := metav1.GetControllerOf(candidate)
		if owner == nil || owner.UID != parent.UID {
			continue
		}
		if candidate.Labels["tekton.dev/pipelineTask"] != task {
			continue
		}
		if child != nil {
			return nil, fmt.Errorf("multiple immediate children for task %s", task)
		}
		if owner.Name != parent.Name || owner.Kind != "PipelineRun" || owner.APIVersion != "tekton.dev/v1" ||
			candidate.Labels["tekton.dev/pipelineRun"] != parent.Name {
			return nil, fmt.Errorf("child %s has incorrect parent metadata", candidate.Name)
		}
		references := 0
		for _, ref := range parent.Status.ChildReferences {
			if ref.Name == candidate.Name {
				if ref.Kind != "PipelineRun" || ref.APIVersion != "tekton.dev/v1" || ref.PipelineTaskName != task {
					return nil, fmt.Errorf("parent %s has incorrect child reference for %s", parent.Name, candidate.Name)
				}
				references++
			}
		}
		if references != 1 {
			return nil, fmt.Errorf("parent %s must reference child %s exactly once, got %d", parent.Name, candidate.Name, references)
		}
		child = candidate
	}
	return child, nil
}

// NestedTask requires exactly one successful owned TaskRun in a no-retry fixture.
func NestedTask(ctx context.Context, client typed.TaskRunInterface, parent *v1.PipelineRun, task string) (*v1.TaskRun, error) {
	list, err := client.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineTask=" + task})
	if err != nil {
		return nil, err
	}
	var matched *v1.TaskRun
	for i := range list.Items {
		run := &list.Items[i]
		if owner := metav1.GetControllerOf(run); owner != nil && owner.UID == parent.UID {
			if matched != nil {
				return nil, fmt.Errorf("multiple owned TaskRuns for task %s", task)
			}
			matched = run
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("missing owned TaskRun for task %s", task)
	}
	condition := matched.Status.GetCondition(apis.ConditionSucceeded)
	if condition == nil || condition.Status != corev1.ConditionTrue || matched.Status.CompletionTime == nil {
		return nil, fmt.Errorf("TaskRun %s did not succeed: %+v", matched.Name, matched.Status.Conditions)
	}
	return matched, nil
}
