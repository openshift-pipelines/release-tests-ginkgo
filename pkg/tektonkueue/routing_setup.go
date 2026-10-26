package tektonkueue

import (
	"context"
	"fmt"
	"time"

	operatorv1alpha1 "github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
)

func (e *Environment) retryCleanupConflict(ctx context.Context, update func() error) error {
	if !e.routingWaits {
		return retry.RetryOnConflict(retry.DefaultRetry, update)
	}
	var err error
	for range 5 {
		if err = ctx.Err(); err != nil {
			return err
		}
		err = update()
		if !apierrors.IsConflict(err) {
			return err
		}
	}
	return err
}

func routingObjectGone(ctx context.Context, resource dynamic.ResourceInterface, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return watchRoutingResource(ctx, resource, func(items []unstructured.Unstructured) (bool, error) {
		for _, item := range items {
			if item.GetName() == name {
				return false, nil
			}
		}
		return true, nil
	})
}

type resourceCondition struct {
	name string
	kind string
}

func routingCondition(ctx context.Context, resource dynamic.ResourceInterface, expected resourceCondition) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return watchRoutingResource(ctx, resource, func(items []unstructured.Unstructured) (bool, error) {
		for _, item := range items {
			if item.GetName() == expected.name {
				ready, _ := conditionTrue(&item, expected.kind)
				return ready, nil
			}
		}
		return false, nil
	})
}

func (e *Environment) enableRoutingScheduler(ctx context.Context, cluster Cluster, role operatorv1alpha1.MultiClusterRole) error {
	owner, err := newRoutingConfigOwner(ctx, cluster)
	if err != nil {
		return err
	}
	e.addCleanup(func(cleanupCtx context.Context) error { return owner.restart(cleanupCtx, owner.original) })
	desired := owner.original.DeepCopy()
	desired.Disabled = ptr(false)
	desired.MultiClusterDisabled = false
	desired.MultiClusterRole = role
	desired.QueueName = e.Prefix
	desired.MultiKueueOverride = role == operatorv1alpha1.MultiClusterRoleHub
	return owner.restart(ctx, *desired)
}

func (e *Environment) waitForRoutingQueues(ctx context.Context) error {
	for _, cluster := range e.clusters() {
		if err := routingCondition(ctx, cluster.Clients.Dynamic.Resource(clusterQueueGVR), resourceCondition{e.Prefix, "Active"}); err != nil {
			return err
		}
		for _, name := range []string{e.Prefix, e.explicitQueueName()} {
			if err := routingCondition(ctx, cluster.Clients.Dynamic.Resource(localQueueGVR).Namespace(e.Namespace), resourceCondition{name, "Active"}); err != nil {
				return err
			}
		}
	}
	if err := routingCondition(ctx, e.Hub.Clients.Dynamic.Resource(admissionCheckGVR), resourceCondition{e.Prefix, "Active"}); err != nil {
		return err
	}
	for _, spoke := range e.Spokes {
		if err := routingCondition(ctx, e.Hub.Clients.Dynamic.Resource(multiKueueClusterGVR), resourceCondition{e.workerName(spoke), "Active"}); err != nil {
			return err
		}
		// After connection readiness, check the remaining credential scope
		// once instead of polling authorization.
		secret, err := e.Hub.Clients.KubeClient.Kube.CoreV1().Secrets(kueueNamespace).Get(ctx, e.workerName(spoke), metav1.GetOptions{})
		if err != nil {
			return err
		}
		if err := verifyWorkerScope(ctx, workerScope{kubeconfig: secret.Data["kubeconfig"], namespace: e.Namespace}); err != nil {
			return fmt.Errorf("verify %s credential: %w", spoke.Name, err)
		}
	}
	return nil
}
