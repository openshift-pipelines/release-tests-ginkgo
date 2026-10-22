package tektonkueue

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	operatorv1alpha1 "github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	operatorfake "github.com/tektoncd/operator/pkg/client/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/features"
	featurestesting "k8s.io/client-go/features/testing"
	clienttesting "k8s.io/client-go/testing"
	"knative.dev/pkg/apis"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

func TestReplaceRoutingConfigRestartsThroughTektonConfig(t *testing.T) {
	// The generated fake does not implement watch-list initial events/bookmarks.
	featurestesting.SetFeatureDuringTest(t, features.WatchListClient, false)
	// Given: the operator owns scheduler lifecycle and forces the hub spec's
	// boolean true, while preserving its supported ConfigMap options.
	initial := &operatorv1alpha1.TektonConfig{ObjectMeta: metav1.ObjectMeta{Name: "config"}}
	initial.Spec.Scheduler.Disabled = ptr(false)
	initial.Spec.Scheduler.Options.ConfigMaps = map[string]corev1.ConfigMap{
		"unrelated": {Data: map[string]string{"keep": "value"}},
	}
	api := operatorfake.NewSimpleClientset(initial, &operatorv1alpha1.TektonScheduler{
		ObjectMeta: metav1.ObjectMeta{Name: operatorv1alpha1.TektonSchedulerResourceName},
	})
	var transitions []bool
	api.PrependReactor("update", "tektonconfigs", func(action clienttesting.Action) (bool, runtime.Object, error) {
		tc := action.(clienttesting.UpdateAction).GetObject().(*operatorv1alpha1.TektonConfig)
		disabled := tc.Spec.Scheduler.IsDisabled()
		transitions = append(transitions, disabled)
		schedulerGVR := operatorv1alpha1.SchemeGroupVersion.WithResource("tektonschedulers")
		if disabled {
			if err := api.Tracker().Delete(schedulerGVR, "", operatorv1alpha1.TektonSchedulerResourceName); err != nil {
				return true, nil, err
			}
		} else {
			scheduler := readyRoutingScheduler(t, tc.Spec.Scheduler)
			scheduler.Spec.MultiKueueOverride = true
			if err := api.Tracker().Create(schedulerGVR, scheduler, ""); err != nil {
				return true, nil, err
			}
		}
		return false, nil, nil
	})
	desired := initial.Spec.Scheduler.DeepCopy()
	desired.Config = (&Environment{Prefix: "test", Namespace: "test"}).routingConfig(true)
	desired.Options.ConfigMaps = map[string]corev1.ConfigMap{
		routingConfigMap: {Data: map[string]string{"config.yaml": `{"queueName":"test","multiKueueOverride":false}`}},
	}
	cluster := Cluster{Name: "hub", Clients: &clients.Clients{Operator: api.OperatorV1alpha1()}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	owner, err := newRoutingConfigOwner(ctx, cluster)
	if err != nil {
		t.Fatal(err)
	}

	// When
	if err := owner.restart(ctx, *desired); err != nil {
		t.Fatal(err)
	}

	// Then: both lifecycle transitions occurred and unrelated options survived.
	if !reflect.DeepEqual(transitions, []bool{true, false}) {
		t.Fatalf("scheduler transitions = %v, want disabled then enabled", transitions)
	}
	tc, err := api.OperatorV1alpha1().TektonConfigs().Get(ctx, "config", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tc.Spec.Scheduler.Config, desired.Config) ||
		!reflect.DeepEqual(tc.Spec.Scheduler.Options.ConfigMaps[routingConfigMap], desired.Options.ConfigMaps[routingConfigMap]) ||
		tc.Spec.Scheduler.Options.ConfigMaps["unrelated"].Data["keep"] != "value" {
		t.Fatalf("configuration update lost requested or unrelated settings: %+v", tc.Spec.Scheduler)
	}
}

func readyRoutingScheduler(t *testing.T, desired operatorv1alpha1.Scheduler) *operatorv1alpha1.TektonScheduler {
	t.Helper()
	// Operator v0.80 publishes Ready but does not publish observedGeneration.
	var scheduler operatorv1alpha1.TektonScheduler
	if err := json.Unmarshal([]byte(`{
		"apiVersion": "operator.tekton.dev/v1alpha1",
		"kind": "TektonScheduler",
		"metadata": {"name": "scheduler", "generation": 1},
		"status": {"conditions": [{"type": "Ready", "status": "True"}]}
	}`), &scheduler); err != nil {
		t.Fatal(err)
	}
	scheduler.Spec.Scheduler = *desired.DeepCopy()
	return &scheduler
}

func TestRoutingSchedulerReadinessWithoutObservedGeneration(t *testing.T) {
	// Given: a serialized v0.80 Ready scheduler with a positive generation.
	desired := &operatorv1alpha1.Scheduler{}
	desired.Config = (&Environment{Prefix: "test", Namespace: "test"}).routingConfig(true)
	for _, test := range []struct {
		name   string
		change func(*operatorv1alpha1.TektonScheduler)
		want   bool
	}{
		{"matching ready", func(*operatorv1alpha1.TektonScheduler) {}, true},
		{"stale queue", func(s *operatorv1alpha1.TektonScheduler) {
			s.Spec.QueueName = "previous-queue"
		}, false},
		{"stale CEL", func(s *operatorv1alpha1.TektonScheduler) {
			s.Spec.CEL.Expressions = []string{`label("previous", "value")`}
		}, false},
		{"unready", func(s *operatorv1alpha1.TektonScheduler) {
			s.Status.SetConditions([]apis.Condition{{Type: apis.ConditionReady, Status: corev1.ConditionFalse}})
		}, false},
		{"missing Ready", func(s *operatorv1alpha1.TektonScheduler) {
			s.Status.SetConditions(nil)
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheduler := readyRoutingScheduler(t, *desired)
			test.change(scheduler)
			wire, err := json.Marshal(scheduler)
			if err != nil {
				t.Fatal(err)
			}
			if scheduler.Generation <= 0 || bytes.Contains(wire, []byte(`"observedGeneration"`)) {
				t.Fatalf("fixture is not a v0.80 scheduler: %s", wire)
			}
			// When
			ready := routingSchedulerReady(scheduler, desired)
			// Then: missing observation is supported, but stale spec/unready is not.
			if ready != test.want {
				t.Fatalf("scheduler readiness = %v, want %v", ready, test.want)
			}
		})
	}
}
