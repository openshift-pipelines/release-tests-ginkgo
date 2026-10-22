package tektonkueue

import (
	"context"
	"reflect"

	operatorv1alpha1 "github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
)

func (o *routingConfigOwner) waitScheduler(ctx context.Context, desired *operatorv1alpha1.Scheduler) error {
	schedulers := o.cluster.Clients.Operator.TektonSchedulers()
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return schedulers.List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return schedulers.Watch(ctx, options)
		},
	}
	_, err := watchtools.UntilWithSync(ctx, lw, &operatorv1alpha1.TektonScheduler{}, func(store cache.Store) (bool, error) {
		return desired == nil && len(store.List()) == 0, nil
	}, func(event watch.Event) (bool, error) {
		if desired == nil {
			return event.Type == watch.Deleted, nil
		}
		scheduler, ok := event.Object.(*operatorv1alpha1.TektonScheduler)
		return ok && event.Type != watch.Deleted &&
			routingSchedulerReady(scheduler, desired), nil
	})
	return err
}

func routingSchedulerReady(scheduler *operatorv1alpha1.TektonScheduler, desired *operatorv1alpha1.Scheduler) bool {
	// restart observes deletion first; v0.80 does not publish observedGeneration.
	return scheduler.Spec.QueueName == desired.QueueName &&
		reflect.DeepEqual(scheduler.Spec.CEL, desired.CEL) && scheduler.Status.IsReady()
}
