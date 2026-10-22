package tektonkueue

import (
	"context"
	"fmt"
	"reflect"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
)

var podGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

type readinessSubscription struct {
	client dynamic.NamespaceableResourceInterface
	stream watch.Interface
	name   string
}

func (s *readinessSubscription) subscribe(ctx context.Context) error {
	if s.stream != nil {
		s.stream.Stop()
	}
	snapshot, err := s.client.List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list %s before readiness watch: %w", s.name, err)
	}
	s.stream, err = s.client.Watch(ctx, metav1.ListOptions{ResourceVersion: snapshot.GetResourceVersion()})
	if err != nil {
		return fmt.Errorf("watch %s: %w", s.name, err)
	}
	return nil
}

// WaitReady waits at most ten minutes for resource and proxy-pod readiness.
// Setup may precede it: each watch starts at its list's resourceVersion before
// any readiness read, so changes during subscription or evaluation are retained.
// It never probes HTTP; the final one-shot AssertReady verifies /ready only after
// all workers and the proxy's HTTP-readiness-probed pods are ready.
func (e *Environment) WaitReady(ctx context.Context) error {
	if err := e.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var subscriptions []*readinessSubscription
	defer func() {
		for _, subscription := range subscriptions {
			if subscription.stream != nil {
				subscription.stream.Stop()
			}
		}
	}()
	cases := []reflect.SelectCase{{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ctx.Done())}}
	for _, cluster := range e.clusters() {
		resources := append(e.readinessResources(cluster), readinessResource{gvr: deploymentGVR}, readinessResource{gvr: podGVR})
		seen := make(map[schema.GroupVersionResource]bool, len(resources))
		for _, resource := range resources {
			if seen[resource.gvr] {
				continue
			}
			seen[resource.gvr] = true
			subscription := &readinessSubscription{
				client: cluster.Clients.Dynamic.Resource(resource.gvr),
				name:   cluster.Name + "/" + resource.gvr.Resource,
			}
			subscriptions = append(subscriptions, subscription)
			if err := subscription.subscribe(ctx); err != nil {
				return err
			}
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(subscription.stream.ResultChan())})
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		last := e.assertResourcesReady(ctx)
		if last == nil {
			last = e.proxyReady(ctx, false)
		}
		if last == nil {
			return nil
		}
		index, value, open := reflect.Select(cases)
		if index == 0 {
			return fmt.Errorf("wait for readiness (%w): %w", last, ctx.Err())
		}
		subscription := subscriptions[index-1]
		if open {
			event := value.Interface().(watch.Event)
			if event.Type != watch.Error {
				continue
			}
			err := apierrors.FromObject(event.Object)
			if !apierrors.IsResourceExpired(err) && !apierrors.IsGone(err) {
				return fmt.Errorf("readiness watch %s: %w", subscription.name, err)
			}
		}
		// A closed or expired watch needs a new list/watch snapshot, not a timer.
		if err := subscription.subscribe(ctx); err != nil {
			return err
		}
		cases[index].Chan = reflect.ValueOf(subscription.stream.ResultChan())
	}
}
