package tektonkueue

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	operatorv1alpha1 "github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var errRoutingConfigChanged = errors.New("scheduler owned state changed concurrently")

// expected changes only after an acknowledged write. A failed readiness wait
// retains the disabled/intermediate state for a later cleanup attempt.
type routingConfigOwner struct {
	cluster  Cluster
	uid      types.UID
	original operatorv1alpha1.Scheduler
	expected operatorv1alpha1.Scheduler
}

func newRoutingConfigOwner(ctx context.Context, cluster Cluster) (*routingConfigOwner, error) {
	tc, err := cluster.Clients.TektonConfig().Get(ctx, "config", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &routingConfigOwner{cluster: cluster, uid: tc.UID,
		original: *tc.Spec.Scheduler.DeepCopy(), expected: *tc.Spec.Scheduler.DeepCopy()}, nil
}

func sameRoutingOwned(left, right operatorv1alpha1.Scheduler) bool {
	leftMap, leftFound := left.Options.ConfigMaps[routingConfigMap]
	rightMap, rightFound := right.Options.ConfigMaps[routingConfigMap]
	return equalBoolPointers(left.Disabled, right.Disabled) &&
		left.MultiClusterDisabled == right.MultiClusterDisabled &&
		left.MultiClusterRole == right.MultiClusterRole &&
		reflect.DeepEqual(left.Config, right.Config) &&
		leftFound == rightFound && reflect.DeepEqual(leftMap, rightMap)
}

func (o *routingConfigOwner) update(ctx context.Context, desired operatorv1alpha1.Scheduler) error {
	client := o.cluster.Clients.TektonConfig()
	for range 5 {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := client.Get(ctx, "config", metav1.GetOptions{})
		if err != nil {
			return err
		}
		// This is the EXACT object/resourceVersion subsequently written. Every
		// conflict retry repeats the comparison; it never adopts a fresh base.
		if current.UID != o.uid || !sameRoutingOwned(current.Spec.Scheduler, o.expected) {
			return fmt.Errorf("%w on %s", errRoutingConfigChanged, o.cluster.Name)
		}
		current.Spec.Scheduler.Disabled = desired.Disabled
		current.Spec.Scheduler.MultiClusterDisabled = desired.MultiClusterDisabled
		current.Spec.Scheduler.MultiClusterRole = desired.MultiClusterRole
		current.Spec.Scheduler.Config = *desired.Config.DeepCopy()
		if override, found := desired.Options.ConfigMaps[routingConfigMap]; found {
			if current.Spec.Scheduler.Options.ConfigMaps == nil {
				current.Spec.Scheduler.Options.ConfigMaps = map[string]corev1.ConfigMap{}
			}
			current.Spec.Scheduler.Options.ConfigMaps[routingConfigMap] = *override.DeepCopy()
		} else {
			delete(current.Spec.Scheduler.Options.ConfigMaps, routingConfigMap)
		}
		updated, err := client.Update(ctx, current, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			continue
		}
		if err != nil {
			return err
		}
		o.expected = *updated.Spec.Scheduler.DeepCopy()
		return nil
	}
	return fmt.Errorf("scheduler update on %s exhausted resourceVersion conflicts", o.cluster.Name)
}

func (o *routingConfigOwner) restart(ctx context.Context, desired operatorv1alpha1.Scheduler) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	disabled := o.expected.DeepCopy()
	disabled.Disabled = ptr(true)
	if err := o.update(ctx, *disabled); err != nil {
		return err
	}
	if err := o.waitScheduler(ctx, nil); err != nil {
		return err
	}
	if err := o.update(ctx, desired); err != nil {
		return err
	}
	if desired.IsDisabled() {
		return nil
	}
	return o.waitScheduler(ctx, &desired)
}
