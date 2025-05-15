package wait

import (
	"context"
	"fmt"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// PipelineRunSucceed returns a condition function that checks if a PipelineRun has succeeded
func PipelineRunSucceed(c *clients.Clients, name string) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
		// Try to get from the namespace stored in context or use the client's default namespace
		// We need to list and find by name since we don't know the namespace
		prs, err := c.PipelineRunClient.List(c.Ctx, metav1.ListOptions{FieldSelector: fmt.Sprintf("metadata.name=%s", name)})
		if err != nil || len(prs.Items) == 0 {
			return false, nil
		}
		pr := prs.Items[0]
		if len(pr.Status.Conditions) == 0 {
			return false, nil
		}
		condition := pr.Status.Conditions[0]
		return condition.Status == corev1.ConditionTrue && condition.Type == "Succeeded", nil
	}
}

// PipelineRunFailed returns a condition function that checks if a PipelineRun has failed
func PipelineRunFailed(c *clients.Clients, name string) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
		prs, err := c.PipelineRunClient.List(c.Ctx, metav1.ListOptions{FieldSelector: fmt.Sprintf("metadata.name=%s", name)})
		if err != nil || len(prs.Items) == 0 {
			return false, nil
		}
		pr := prs.Items[0]
		if len(pr.Status.Conditions) == 0 {
			return false, nil
		}
		condition := pr.Status.Conditions[0]
		return condition.Status == corev1.ConditionFalse && condition.Type == "Succeeded", nil
	}
}

// Running returns a condition function that checks if a PipelineRun is running
func Running(c *clients.Clients, name string) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
		prs, err := c.PipelineRunClient.List(c.Ctx, metav1.ListOptions{FieldSelector: fmt.Sprintf("metadata.name=%s", name)})
		if err != nil || len(prs.Items) == 0 {
			return false, nil
		}
		pr := prs.Items[0]
		if len(pr.Status.Conditions) == 0 {
			return false, nil
		}
		condition := pr.Status.Conditions[0]
		return condition.Status == corev1.ConditionUnknown && condition.Type == "Succeeded", nil
	}
}

// TaskRunRunning returns a condition function that checks if a TaskRun is running
func TaskRunRunning(c *clients.Clients, name string) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
		tr, err := c.TaskRunClient.Get(c.Ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		if len(tr.Status.Conditions) == 0 {
			return false, nil
		}
		condition := tr.Status.Conditions[0]
		return condition.Status == corev1.ConditionUnknown && condition.Type == "Succeeded", nil
	}
}

// FailedWithReason returns a condition function that checks if a PipelineRun/TaskRun failed with a specific reason
func FailedWithReason(c *clients.Clients, reason, name string) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
		prs, err := c.PipelineRunClient.List(c.Ctx, metav1.ListOptions{FieldSelector: fmt.Sprintf("metadata.name=%s", name)})
		if err != nil || len(prs.Items) == 0 {
			return false, nil
		}
		pr := prs.Items[0]
		if len(pr.Status.Conditions) == 0 {
			return false, nil
		}
		condition := pr.Status.Conditions[0]
		return condition.Status == corev1.ConditionFalse && condition.Reason == reason, nil
	}
}

// TaskRunFailedWithReason returns a condition function that checks if a TaskRun failed with a specific reason
func TaskRunFailedWithReason(c *clients.Clients, reason, name string) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (bool, error) {
		tr, err := c.TaskRunClient.Get(c.Ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		if len(tr.Status.Conditions) == 0 {
			return false, nil
		}
		condition := tr.Status.Conditions[0]
		return condition.Status == corev1.ConditionFalse && condition.Reason == reason, nil
	}
}

// WaitForPipelineRunState waits for a PipelineRun to reach a specific state
func WaitForPipelineRunState(c *clients.Clients, name string, inState wait.ConditionWithContextFunc, message string) error {
	return wait.PollUntilContextTimeout(c.Ctx, config.APIRetry, config.APITimeout, false, inState)
}

// WaitForTaskRunState waits for a TaskRun to reach a specific state
func WaitForTaskRunState(c *clients.Clients, name string, inState wait.ConditionWithContextFunc, message string) error {
	return wait.PollUntilContextTimeout(c.Ctx, config.APIRetry, config.APITimeout, false, inState)
}
