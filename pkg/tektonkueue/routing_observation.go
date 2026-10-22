package tektonkueue

import (
	"errors"
	"fmt"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
)

var errRoutingPlacement = errors.New("invalid routing placement")

type routingEvent struct {
	cluster int
	object  runtime.Object
}

type clusterRoutingState struct {
	runSeen    bool
	runSuccess bool
	tasks      map[string]bool
}

type routingState []clusterRoutingState

func newRoutingState(clusters int) routingState {
	state := make(routingState, clusters)
	for i := range state {
		state[i].tasks = map[string]bool{}
	}
	return state
}

func (s routingState) record(event routingEvent) error {
	state := &s[event.cluster]
	switch obj := event.object.(type) {
	case *pipelinev1.PipelineRun:
		state.runSeen = true
		if event.cluster > 0 && obj.Spec.ManagedBy != nil && *obj.Spec.ManagedBy != "tekton.dev/pipeline" {
			return fmt.Errorf("worker retained managedBy %q", *obj.Spec.ManagedBy)
		}
		terminal, success, detail := pipelineRunState(obj)
		if terminal && !success {
			return fmt.Errorf("PipelineRun failed on cluster %d: %s", event.cluster, detail)
		}
		state.runSuccess = state.runSuccess || success
	case *pipelinev1.TaskRun:
		condition := obj.Status.GetCondition(apis.ConditionSucceeded)
		if condition != nil && condition.Status == corev1.ConditionFalse {
			return fmt.Errorf("TaskRun %s failed on cluster %d: %s: %s", obj.Name, event.cluster, condition.Reason, condition.Message)
		}
		state.tasks[obj.Name] = state.tasks[obj.Name] || condition != nil && condition.Status == corev1.ConditionTrue
	}
	return nil
}

func (s routingState) placement(remote bool) (int, bool, error) {
	selected := 0
	for i := 1; i < len(s); i++ {
		if s[i].runSeen || len(s[i].tasks) > 0 {
			if !remote {
				return 0, false, fmt.Errorf("%w: local PipelineRun appeared on spoke %d", errRoutingPlacement, i)
			}
			if selected != 0 {
				return 0, false, fmt.Errorf("%w: remote PipelineRun appeared on multiple spokes", errRoutingPlacement)
			}
			selected = i
		}
	}
	if remote && len(s[0].tasks) > 0 {
		return 0, false, fmt.Errorf("%w: remote PipelineRun created TaskRuns on hub", errRoutingPlacement)
	}
	if remote && selected == 0 {
		return 0, false, nil
	}
	success := s[0].runSuccess && s[selected].runSuccess && len(s[selected].tasks) > 0
	for _, taskSuccess := range s[selected].tasks {
		success = success && taskSuccess
	}
	return selected, success, nil
}
