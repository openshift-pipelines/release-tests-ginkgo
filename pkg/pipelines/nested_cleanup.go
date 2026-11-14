package pipelines

import (
	"context"
	"fmt"

	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	typed "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"knative.dev/pkg/apis"
)

// CancelNestedTaskRuns stops active TaskRuns owned by the supplied PipelineRuns,
// preserving their resources and statuses for failure diagnosis.
func CancelNestedTaskRuns(ctx context.Context, client typed.TaskRunInterface, owners []types.UID) ([]types.UID, error) {
	owned := make(map[types.UID]bool, len(owners))
	for _, uid := range owners {
		owned[uid] = true
	}
	list, err := client.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	uids := make([]types.UID, 0, len(list.Items))
	pending := make(map[types.UID]string)
	for _, run := range list.Items {
		owner := metav1.GetControllerOf(&run)
		if owner == nil || owner.Kind != "PipelineRun" || !owned[owner.UID] {
			continue
		}
		uids = append(uids, run.UID)
		condition := run.Status.GetCondition(apis.ConditionSucceeded)
		if condition == nil || condition.Status == corev1.ConditionUnknown {
			pending[run.UID] = run.Name
		}
	}
	if len(pending) == 0 {
		return uids, nil
	}
	stream, err := client.Watch(ctx, metav1.ListOptions{ResourceVersion: list.ResourceVersion})
	if err != nil {
		return nil, err
	}
	defer stream.Stop()
	patch := []byte(fmt.Sprintf(`{"spec":{"status":%q}}`, v1.TaskRunSpecStatusCancelled))
	for _, name := range pending {
		if _, err := client.Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		}
	}
	for len(pending) != 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event, open := <-stream.ResultChan():
			if !open {
				return nil, fmt.Errorf("taskrun cleanup watch closed before completion")
			}
			if event.Type == watch.Error {
				return nil, apierrors.FromObject(event.Object)
			}
			run, ok := event.Object.(*v1.TaskRun)
			if !ok {
				continue
			}
			condition := run.Status.GetCondition(apis.ConditionSucceeded)
			if event.Type == watch.Deleted || (condition != nil && condition.Status != corev1.ConditionUnknown) {
				delete(pending, run.UID)
			}
		}
	}
	return uids, nil
}

// WaitNestedPodsStopped observes terminal or deleted Pods owned by the supplied
// TaskRuns. It never deletes resources retained for debugging.
func WaitNestedPodsStopped(ctx context.Context, client coreclient.PodInterface, tasks []types.UID) error {
	owned := make(map[types.UID]bool, len(tasks))
	for _, uid := range tasks {
		owned[uid] = true
	}
	list, err := client.List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	pending := make(map[types.UID]bool)
	for _, pod := range list.Items {
		owner := metav1.GetControllerOf(&pod)
		if owner != nil && owner.Kind == "TaskRun" && owned[owner.UID] &&
			pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
			pending[pod.UID] = true
		}
	}
	if len(pending) == 0 {
		return nil
	}
	stream, err := client.Watch(ctx, metav1.ListOptions{ResourceVersion: list.ResourceVersion})
	if err != nil {
		return err
	}
	defer stream.Stop()
	for len(pending) != 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, open := <-stream.ResultChan():
			if !open {
				return fmt.Errorf("pod cleanup watch closed before completion")
			}
			if event.Type == watch.Error {
				return apierrors.FromObject(event.Object)
			}
			pod, ok := event.Object.(*corev1.Pod)
			if ok && (event.Type == watch.Deleted || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed) {
				delete(pending, pod.UID)
			}
		}
	}
	return nil
}
