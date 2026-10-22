package tektonkueue

import (
	"context"
	"fmt"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	multiKueueController = "kueue.x-k8s.io/multikueue"
	queueLabel           = "kueue.x-k8s.io/queue-name"
	workloadImage        = "registry.access.redhat.com/ubi9/ubi-minimal@sha256:34880b64c07f28f64d95737f82f891516de9a3b43583f39970f7bf8e4cfa48b7"
	workloadLogMarker    = "multi-cluster-execution-ok"
	workloadLogContainer = "step-prove-execution"
)

// Result contains only console-session-authenticated proxy log bytes.
type Result struct {
	PipelineRun string
	Spoke       string
	Logs        string
}

// Execute arms session authentication, resource histories, and cleanup before create.
func (e *Environment) Execute(ctx context.Context) (*Result, error) {
	armed, err := e.Arm(ctx)
	if err != nil {
		return nil, err
	}
	run, err := e.createPipelineRun(armed.ctx)
	if err != nil {
		return nil, err
	}
	return armed.Observe(run)
}

func (s *ArmedExecution) observe(ctx context.Context, run *pipelinev1.PipelineRun) (*Result, error) {
	e, w := s.environment, s.watches
	if err := w.bind(run); err != nil {
		return nil, err
	}
	var spoke Cluster
	var pod corev1.Pod
	err := s.wait(ctx, func() (bool, error) {
		if len(w.seen) != 1 {
			return false, nil
		}
		for _, candidate := range e.Spokes {
			if w.seen[candidate.Name] {
				spoke = candidate
			}
		}
		if err := w.completePlacement(spoke.Name); err != nil {
			// Partial cross-resource delivery is expected; wait for the complete identity set.
			worker, hub := w.retained[spoke.Name], w.retained[e.Hub.Name]
			if hub == nil || hub.run == nil || hub.workload == nil || worker == nil ||
				worker.run == nil || worker.task == nil || worker.pod == nil {
				return false, nil
			}
			return false, err
		}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(w.retained[spoke.Name].pod.object.Object, &pod); err != nil {
			return false, fmt.Errorf("invalid observed Pod")
		}
		if s.pac != nil {
			ready, err := s.pacReady(run, spoke)
			if err != nil || !ready {
				return false, err
			}
		}
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == workloadLogContainer && status.State.Running != nil {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	hub, worker := w.retained[e.Hub.Name], w.retained[spoke.Name]
	target := gateTarget{cluster: spoke, namespace: e.Namespace, pod: pod.Name, podUID: pod.UID}
	logs, err := s.proxy.liveLogs(ctx, proxyRun{
		namespace: e.Namespace, name: run.Name, worker: e.workerName(spoke), pod: pod.Name,
		workload: hub.workload.object.GetName(), task: worker.task.object.GetName(),
		runUID: worker.run.object.GetUID(), taskUID: worker.task.object.GetUID(), podUID: pod.UID,
	}, func(ctx context.Context, marker string) error { return target.release(ctx, "gate1", marker) })
	if err != nil {
		return nil, err
	}
	if err := w.drain(ctx); err != nil {
		return nil, err
	}
	if err := w.completePlacement(spoke.Name); err != nil {
		return nil, err
	}
	if err := s.assertNonterminal(ctx, spoke); err != nil {
		return nil, err
	}
	if err := w.drain(ctx); err != nil {
		return nil, err
	}
	if err := w.completePlacement(spoke.Name); err != nil {
		return nil, err
	}
	if s.pac != nil {
		s.pac.allowDeletion = true
	}
	if err := target.release(ctx, "gate2", "finish"); err != nil {
		return nil, err
	}
	if err := s.wait(ctx, func() (bool, error) {
		workload := w.retained[e.Hub.Name].workload
		finished, _ := conditionTrue(workload.object, "Finished")
		return !workload.deleted && finished && w.success[e.Hub.Name] && w.success[spoke.Name], nil
	}); err != nil {
		return nil, err
	}
	if err := s.wait(ctx, func() (bool, error) { return s.workerCleanup(ctx) }); err != nil {
		return nil, err
	}
	return &Result{PipelineRun: run.Name, Spoke: spoke.Name, Logs: logs}, nil
}

func (s *ArmedExecution) workerCleanup(ctx context.Context) (bool, error) {
	w := s.watches
	hub := w.retained[s.environment.Hub.Name]
	for _, worker := range s.environment.Spokes {
		if !w.gone(worker.Name) {
			return false, nil
		}
		if hub == nil || hub.workload == nil {
			continue
		}
		// A current absence check complements the continuous history; it never
		// repairs a lost stream or supplies missing execution events.
		object, err := worker.Clients.Dynamic.Resource(workloadGVR).Namespace(s.environment.Namespace).
			Get(ctx, hub.workload.object.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return false, requestError(ctx, "verify worker Workload cleanup")
		}
		if c := w.retained[worker.Name]; c != nil && c.workload != nil && object.GetUID() != c.workload.object.GetUID() {
			return false, fmt.Errorf("worker Workload was replaced")
		}
		return false, nil
	}
	if s.pac != nil {
		return s.pacCleanup(ctx, false)
	}
	return true, nil
}

func (s *ArmedExecution) wait(ctx context.Context, predicate func() (bool, error)) error {
	return s.watches.wait(ctx, func() (bool, error) {
		for _, failed := range s.watches.failed {
			if failed {
				return false, fmt.Errorf("bound PipelineRun failed")
			}
		}
		return predicate()
	})
}

func (s *ArmedExecution) assertNonterminal(ctx context.Context, spoke Cluster) error {
	for _, cluster := range []Cluster{s.environment.Hub, spoke} {
		run, err := cluster.Clients.PipelineRunClient.Get(ctx, s.watches.runName, metav1.GetOptions{})
		if err != nil {
			return requestError(ctx, "read bound PipelineRun")
		}
		if run.UID != s.watches.retained[cluster.Name].run.object.GetUID() {
			return fmt.Errorf("nonterminal read found a replacement PipelineRun")
		}
		for _, condition := range run.Status.Conditions {
			if condition.Type == "Succeeded" && (condition.Status == "True" || condition.Status == "False") {
				return fmt.Errorf("bound PipelineRun is terminal before gate 2")
			}
		}
	}
	return nil
}
