package tektonkueue

import (
	"reflect"
	"testing"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
)

func TestRoutingAdmissionRejectsIncorrectMutations(t *testing.T) {
	for _, route := range []string{"remote", "explicit", "local", "missing"} {
		t.Run(route, func(t *testing.T) {
			// Given: input does not pre-set webhook-owned fields.
			e := &Environment{Prefix: "test", Namespace: "test"}
			input, expected, err := e.newRoutingRun(route)
			if err != nil {
				t.Fatal(err)
			}
			if input.Spec.ManagedBy != nil || input.Spec.Status != "" {
				t.Fatal("routing input pre-sets webhook-owned fields")
			}
			if _, present := input.Labels[routeLabel]; route == "missing" && present {
				t.Fatal("missing-label fixture contains the route label")
			}
			if route == "explicit" && input.Labels[queueLabel] == expected.queue {
				t.Fatal("explicit queue fixture cannot distinguish override from preservation")
			}
			admitted := input.DeepCopy()
			admitted.Labels[queueLabel] = expected.queue
			admitted.Spec.Status = pipelinev1.PipelineRunSpecStatusPending
			if expected.remote {
				controller := multiKueueController
				admitted.Spec.ManagedBy = &controller
			}
			for _, mutation := range []string{"correct", "queue", "controller", "pending"} {
				t.Run(mutation, func(t *testing.T) {
					run := admitted.DeepCopy()
					switch mutation {
					case "queue":
						run.Labels[queueLabel] = "wrong"
					case "controller":
						controller := multiKueueController
						if expected.remote {
							controller = "tekton.dev/pipeline"
						}
						run.Spec.ManagedBy = &controller
					case "pending":
						run.Spec.Status = ""
					}
					// When
					err := validateRoutingAdmission(run, expected)
					// Then
					if (err != nil) != (mutation != "correct") {
						t.Fatalf("validation error = %v for %s", err, mutation)
					}
				})
			}
		})
	}
}

func TestRoutingPlacementRequiresSuccessfulExecution(t *testing.T) {
	for _, test := range []struct {
		name    string
		remote  bool
		events  []routingEvent
		want    int
		done    bool
		wantErr bool
	}{
		{name: "admission alone is insufficient", remote: true, events: []routingEvent{{0, successfulRoutingRun()}}, done: false},
		{name: "remote success", remote: true, events: []routingEvent{{0, successfulRoutingRun()}, {1, successfulRoutingRun()}, {1, successfulRoutingTask()}}, want: 1, done: true},
		{name: "local success", events: []routingEvent{{0, successfulRoutingRun()}, {0, successfulRoutingTask()}}, done: true},
		{name: "remote task on hub", remote: true, events: []routingEvent{{0, successfulRoutingTask()}}, wantErr: true},
		{name: "local run on spoke", events: []routingEvent{{1, successfulRoutingRun()}}, wantErr: true},
		{name: "multiple spokes", remote: true, events: []routingEvent{{1, successfulRoutingRun()}, {2, successfulRoutingRun()}}, wantErr: true},
		{name: "task without terminal status", remote: true, events: []routingEvent{{0, successfulRoutingRun()}, {1, successfulRoutingRun()}, {1, &pipelinev1.TaskRun{ObjectMeta: metav1.ObjectMeta{Name: "task"}}}}, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			state := newRoutingState(3)
			for _, event := range test.events {
				if err := state.record(event); err != nil {
					t.Fatal(err)
				}
			}
			// When
			selected, done, err := state.placement(test.remote)
			// Then
			if (err != nil) != test.wantErr || err == nil && (selected != test.want || done != test.done) {
				t.Fatalf("placement = (%d, %t, %v), want (%d, %t), error=%t", selected, done, err, test.want, test.done, test.wantErr)
			}
		})
	}
}

func TestRoutingObservationsRejectFailedRunsAndTasks(t *testing.T) {
	run := successfulRoutingRun()
	run.Status.Conditions[0].Status = corev1.ConditionFalse
	task := successfulRoutingTask()
	task.Status.Conditions[0].Status = corev1.ConditionFalse
	controller := multiKueueController
	worker := successfulRoutingRun()
	worker.Spec.ManagedBy = &controller
	for _, event := range []routingEvent{{0, run}, {1, task}, {1, worker}} {
		// Given
		state := newRoutingState(2)
		// When
		err := state.record(event)
		// Then
		if err == nil {
			t.Fatalf("accepted invalid execution observation: %+v", event)
		}
	}
}

func TestRoutingObservationsRetainSuccessAfterOlderEvents(t *testing.T) {
	// Given: a terminal object can be deleted before the hub mirrors success.
	state := newRoutingState(2)
	for _, event := range []routingEvent{{1, successfulRoutingRun()}, {1, successfulRoutingTask()}} {
		if err := state.record(event); err != nil {
			t.Fatal(err)
		}
	}
	before := state[1].tasks["task"]
	// When: another notification has no terminal condition.
	if err := state.record(routingEvent{1, &pipelinev1.TaskRun{ObjectMeta: metav1.ObjectMeta{Name: "task"}}}); err != nil {
		t.Fatal(err)
	}
	// Then: previously observed success is not lost.
	if !before || !reflect.DeepEqual(state[1].tasks, map[string]bool{"task": true}) || !state[1].runSuccess {
		t.Fatalf("lost terminal observations: %+v", state[1])
	}
}

func successfulRoutingRun() *pipelinev1.PipelineRun {
	run := &pipelinev1.PipelineRun{}
	run.Status.SetConditions([]apis.Condition{{Type: apis.ConditionSucceeded, Status: corev1.ConditionTrue}})
	return run
}

func successfulRoutingTask() *pipelinev1.TaskRun {
	task := &pipelinev1.TaskRun{ObjectMeta: metav1.ObjectMeta{Name: "task"}}
	task.Status.SetConditions([]apis.Condition{{Type: apis.ConditionSucceeded, Status: corev1.ConditionTrue}})
	return task
}
