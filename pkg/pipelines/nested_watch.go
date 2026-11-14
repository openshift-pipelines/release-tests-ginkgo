package pipelines

import (
	"context"
	"errors"

	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	typed "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
)

// ErrNestedWatchClosed identifies a recoverable watch interruption.
var ErrNestedWatchClosed = errors.New("watch closed before completion")

// ResumeNestedPipelineRun relists after a watch interruption without extending
// the original deadline. Deletion and non-expiration API errors remain failures.
func ResumeNestedPipelineRun(ctx context.Context, client typed.PipelineRunInterface, name string) (*v1.PipelineRun, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		options := metav1.ListOptions{FieldSelector: "metadata.name=" + name}
		list, err := client.List(ctx, options)
		if err != nil {
			return nil, err
		}
		if len(list.Items) == 0 {
			return nil, apierrors.NewNotFound(v1.Resource("pipelineruns"), name)
		}
		run := &list.Items[0]
		condition := run.Status.GetCondition(apis.ConditionSucceeded)
		if condition != nil && (condition.Status == corev1.ConditionTrue || condition.Status == corev1.ConditionFalse) {
			return run, nil
		}
		options.ResourceVersion = list.ResourceVersion
		stream, err := client.Watch(ctx, options)
		if apierrors.IsResourceExpired(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		run, err = ReadNestedWatch(ctx, stream.ResultChan(), name)
		stream.Stop()
		if errors.Is(err, ErrNestedWatchClosed) || apierrors.IsResourceExpired(err) {
			continue
		}
		return run, err
	}
}
