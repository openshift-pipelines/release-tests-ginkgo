package tektonkueue

import (
	"context"
	"fmt"
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ArmedExecution establishes console authentication, continuous execution
// histories and registered failure cleanup before the caller triggers a run.
// It observes exactly one test-owned run using the basic two-gate protocol.
type ArmedExecution struct {
	environment *Environment
	watches     *executionWatches
	proxy       *consoleProxy
	ctx         context.Context
	cancel      context.CancelFunc
	root        *pipelinev1.PipelineRun
	pac         *pacExecution
}

// Arm must succeed before any external trigger. Environment.Cleanup is already
// responsible for the armed execution even if the trigger or later binding fails.
func (e *Environment) Arm(ctx context.Context) (*ArmedExecution, error) {
	proxy, err := discoverConsoleProxy(ctx, e.Hub.Clients)
	if err != nil {
		return nil, err
	}
	return e.arm(ctx, proxy)
}

func (e *Environment) arm(ctx context.Context, proxy *consoleProxy) (*ArmedExecution, error) {
	deadline := time.Now().Add(10 * time.Minute)
	if proxy.expires.Before(deadline) {
		deadline = proxy.expires
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	if err := proxy.authenticate(ctx, proxy.user); err != nil {
		cancel()
		return nil, err
	}
	watches, err := e.watchExecution(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	if err := watches.drain(ctx); err != nil {
		watches.stop()
		cancel()
		return nil, err
	}
	watches.armed = true
	context.AfterFunc(ctx, watches.stop)
	armed := &ArmedExecution{environment: e, watches: watches, proxy: proxy, ctx: ctx, cancel: cancel}
	e.addCleanup(armed.cleanup)
	return armed, nil
}

// Observe binds the server-verified hub UID to histories already armed before
// creation. No replacement run, delayed arming or creation/patching is accepted.
func (s *ArmedExecution) Observe(run *pipelinev1.PipelineRun) (*Result, error) {
	e := s.environment
	if s.root != nil {
		return nil, fmt.Errorf("armed execution is already bound")
	}
	if run == nil || run.Name == "" || run.UID == "" || run.Namespace != e.Namespace || !s.owns(run.Labels) {
		return nil, fmt.Errorf("binding requires a test-owned hub PipelineRun identity")
	}
	current, err := e.Hub.Clients.PipelineRunClient.Get(s.ctx, run.Name, metav1.GetOptions{})
	if err != nil {
		return nil, requestError(s.ctx, "verify hub PipelineRun identity")
	}
	if current.UID != run.UID || !s.owns(current.Labels) {
		return nil, fmt.Errorf("hub binding found a replacement PipelineRun")
	}
	s.root = current.DeepCopy()
	return s.observe(s.ctx, current)
}

func (s *ArmedExecution) cleanup(ctx context.Context) error {
	defer s.cancel()
	defer s.watches.stop()
	e, w := s.environment, s.watches
	historyErr := w.drain(ctx)
	if s.pac != nil {
		s.pac.allowDeletion = true
		s.pac.cleaning = true
	}
	// This list is only a guarded deletion inventory if the caller failed before
	// binding. It never replaces execution history or repairs a broken stream.
	if s.root == nil {
		for _, event := range w.events {
			if event.cluster != e.Hub.Name || event.gvr != pipelineRunGVR || !s.owns(event.object.GetLabels()) {
				continue
			}
			if s.root != nil && (s.root.Name != event.object.GetName() || s.root.UID != event.object.GetUID()) {
				return fmt.Errorf("cleanup found conflicting historic hub identities")
			}
			s.root = &pipelinev1.PipelineRun{ObjectMeta: metav1.ObjectMeta{
				Name: event.object.GetName(), Namespace: e.Namespace, UID: event.object.GetUID(), Labels: event.object.GetLabels(), Annotations: event.object.GetAnnotations(),
			}}
		}
		runs, err := e.Hub.Clients.PipelineRunClient.List(ctx, metav1.ListOptions{})
		if err != nil {
			return requestError(ctx, "inventory owned cleanup runs")
		}
		for i := range runs.Items {
			run := &runs.Items[i]
			if !s.owns(run.Labels) {
				continue
			}
			if s.root != nil {
				if s.root.Name != run.Name || s.root.UID != run.UID {
					return fmt.Errorf("cleanup found multiple or replaced owned hub PipelineRuns")
				}
				continue
			}
			s.root = run.DeepCopy()
		}
	}
	if s.root == nil {
		return historyErr
	}
	run := s.root
	current, err := e.Hub.Clients.PipelineRunClient.Get(ctx, run.Name, metav1.GetOptions{})
	if err == nil {
		if current.UID != run.UID || !s.owns(current.Labels) {
			return fmt.Errorf("refusing to delete a replacement hub PipelineRun")
		}
		uid := run.UID
		err = e.Hub.Clients.PipelineRunClient.Delete(ctx, run.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return requestError(ctx, "delete owned hub PipelineRun")
	}
	if historyErr != nil {
		return historyErr
	}
	if err := w.bind(run); err != nil {
		return err
	}
	return w.wait(ctx, func() (bool, error) {
		hub := w.retained[e.Hub.Name]
		if hub == nil || hub.run == nil || !hub.run.deleted {
			return false, nil
		}
		for _, cluster := range e.clusters() {
			if !w.gone(cluster.Name) {
				return false, nil
			}
		}
		ready, err := s.workerCleanup(ctx)
		if err != nil || !ready {
			return ready, err
		}
		if s.pac != nil {
			return s.pacCleanup(ctx, true)
		}
		return true, nil
	})
}
