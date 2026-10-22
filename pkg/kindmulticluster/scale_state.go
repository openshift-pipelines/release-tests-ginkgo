package kindmulticluster

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	watchtools "k8s.io/client-go/tools/watch"
)

type runPhase uint8

const (
	runDispatched runPhase = iota
	runFinished
)

type runPlacement struct {
	worker int
	pod    string
}

type scaleRun struct {
	name   string
	uid    types.UID // Assigned from this run's Create response before verification.
	target int       // -1 permits either worker; 0 and 1 require the targeted worker.

	mu       sync.Mutex
	hub      *pipelinev1.PipelineRun
	workers  [2]*pipelinev1.PipelineRun
	pods     [2]*corev1.Pod
	workload *unstructured.Unstructured
	selected string // Retain selection after Kueue releases finished quota.
	failure  error
	changed  chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	group    errgroup.Group
	watches  []*watchtools.RetryWatcher
	verified bool
}

func newScaleRun(name string) *scaleRun {
	return &scaleRun{name: name, target: -1, changed: make(chan struct{}, 1)}
}

func (r *scaleRun) fail(err error) {
	r.mu.Lock()
	if r.failure == nil {
		r.failure = err
	}
	r.mu.Unlock()
	select {
	case r.changed <- struct{}{}:
	default:
	}
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *scaleRun) close() {
	if r.cancel != nil {
		r.cancel()
	}
	for _, events := range r.watches {
		events.Stop()
		<-events.Done()
	}
	// Reader errors are retained in failure and exposed by progress.
	if err := r.group.Wait(); err != nil {
		r.fail(err)
	}
}

func (r *scaleRun) wait(ctx context.Context, phase runPhase) (runPlacement, error) {
	for {
		placement, ready, err := r.progress(phase)
		if err != nil || ready {
			return placement, err
		}
		select {
		case <-ctx.Done():
			return runPlacement{}, ctx.Err()
		case <-r.changed:
		}
	}
}

func (r *scaleRun) progress(phase runPhase) (runPlacement, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failure != nil {
		return runPlacement{}, false, r.failure
	}
	worker := -1
	for i := range r.workers {
		if r.workers[i] != nil || r.pods[i] != nil {
			if worker != -1 {
				return runPlacement{}, false, fmt.Errorf("%s executed on multiple spokes", r.name)
			}
			worker = i
		}
	}
	if worker == -1 || r.workers[worker] == nil || r.hub == nil || r.workload == nil {
		return runPlacement{}, false, nil
	}
	if r.hub.UID != r.uid {
		return runPlacement{}, false, fmt.Errorf("hub PipelineRun %s was replaced", r.name)
	}
	if r.target != -1 && worker != r.target {
		return runPlacement{}, false, fmt.Errorf("%s executed on spoke %d, want %d", r.name, worker+1, r.target+1)
	}
	remote := r.workers[worker]
	if remote.UID == "" || remote.UID == r.uid ||
		(remote.Spec.ManagedBy != nil && *remote.Spec.ManagedBy == "kueue.x-k8s.io/multikueue") {
		return runPlacement{}, false, fmt.Errorf("worker PipelineRun is not an independent locally managed execution")
	}
	ownerMatches := false
	for _, owner := range r.workload.GetOwnerReferences() {
		ownerMatches = ownerMatches || owner.UID == r.uid
	}
	if !ownerMatches {
		return runPlacement{}, false, fmt.Errorf("workload owner does not match %s", r.name)
	}
	if r.selected == "" {
		return runPlacement{}, false, nil
	}
	if r.selected != fmt.Sprintf("release-spoke-%d", worker+1) {
		return runPlacement{}, false, fmt.Errorf("workload selected %q but %s executed on spoke %d", r.selected, r.name, worker+1)
	}
	hubSuccess, err := runSucceeded(r.hub)
	if err != nil {
		return runPlacement{}, false, err
	}
	workerSuccess, err := runSucceeded(remote)
	if err != nil {
		return runPlacement{}, false, err
	}
	pod := r.pods[worker]
	if pod == nil {
		return runPlacement{}, false, nil
	}
	placement := runPlacement{worker: worker, pod: pod.Name}
	switch phase {
	case runDispatched:
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == container && status.State.Running != nil {
				return placement, true, nil
			}
		}
		return placement, false, nil
	case runFinished:
		if !hubSuccess || !workerSuccess {
			return placement, false, nil
		}
		if len(r.hub.Status.ChildReferences) != 1 || len(remote.Status.ChildReferences) != 1 ||
			!reflect.DeepEqual(r.hub.Status.ChildReferences, remote.Status.ChildReferences) {
			return placement, false, fmt.Errorf("hub did not return %s's spoke TaskRun reference", r.name)
		}
		conditions, _, err := unstructured.NestedSlice(r.workload.Object, "status", "conditions")
		if err != nil {
			return placement, false, err
		}
		for _, raw := range conditions {
			condition, ok := raw.(map[string]any)
			if !ok {
				return placement, false, fmt.Errorf("invalid Workload condition")
			}
			if condition["type"] == "Finished" {
				return placement, condition["status"] == "True", nil
			}
		}
		return placement, false, nil
	default:
		return placement, false, fmt.Errorf("unknown run phase %d", phase)
	}
}

func runSucceeded(run *pipelinev1.PipelineRun) (bool, error) {
	for _, condition := range run.Status.Conditions {
		if condition.Type != "Succeeded" {
			continue
		}
		switch condition.Status {
		case corev1.ConditionTrue:
			return true, nil
		case corev1.ConditionFalse:
			return false, fmt.Errorf("%s failed: %s: %s", run.Name, condition.Reason, condition.Message)
		case corev1.ConditionUnknown:
			return false, nil
		default:
			return false, fmt.Errorf("invalid completion status %q", condition.Status)
		}
	}
	return false, nil
}
