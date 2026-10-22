package tektonkueue

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	operatorv1alpha1 "github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	operatorfake "github.com/tektoncd/operator/pkg/client/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/features"
	featurestesting "k8s.io/client-go/features/testing"
	clienttesting "k8s.io/client-go/testing"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

var configTestGVR = operatorv1alpha1.SchemeGroupVersion.WithResource("tektonconfigs")

type configLifecycleHarness struct {
	api      *operatorfake.Clientset
	owner    *routingConfigOwner
	revision int
	writes   int
	onUpdate func(*operatorv1alpha1.TektonConfig) error
}

func newConfigLifecycleHarness(t *testing.T) *configLifecycleHarness {
	t.Helper()
	featurestesting.SetFeatureDuringTest(t, features.WatchListClient, false)
	initial := &operatorv1alpha1.TektonConfig{ObjectMeta: metav1.ObjectMeta{Name: "config", UID: "config-uid", ResourceVersion: "1"}}
	initial.Spec.Scheduler.Disabled = ptr(false)
	initial.Spec.Scheduler.Config = (&Environment{Prefix: "test", Namespace: "test"}).routingConfig(true)
	initial.Spec.Scheduler.Options.ConfigMaps = map[string]corev1.ConfigMap{
		routingConfigMap: {Data: map[string]string{"config.yaml": "owned"}},
	}
	h := &configLifecycleHarness{api: operatorfake.NewSimpleClientset(initial), revision: 1}
	h.api.PrependReactor("update", "tektonconfigs", func(action clienttesting.Action) (bool, runtime.Object, error) {
		input := action.(clienttesting.UpdateAction).GetObject().(*operatorv1alpha1.TektonConfig)
		if h.onUpdate != nil {
			if err := h.onUpdate(input); err != nil {
				return true, nil, err
			}
		}
		current, err := h.current()
		if err != nil {
			return true, nil, err
		}
		if current.ResourceVersion != input.ResourceVersion {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "tektonconfigs"}, "config", errors.New("resourceVersion changed"))
		}
		ack := input.DeepCopy()
		h.writes++
		if err := h.store(ack); err != nil {
			return true, nil, err
		}
		schedulerGVR := operatorv1alpha1.SchemeGroupVersion.WithResource("tektonschedulers")
		if ack.Spec.Scheduler.IsDisabled() {
			if err := h.api.Tracker().Delete(schedulerGVR, "", operatorv1alpha1.TektonSchedulerResourceName); err != nil && !apierrors.IsNotFound(err) {
				return true, nil, err
			}
		} else {
			scheduler := readyRoutingScheduler(t, ack.Spec.Scheduler)
			if err := h.api.Tracker().Create(schedulerGVR, scheduler, ""); err != nil {
				return true, nil, err
			}
		}
		return true, ack, nil
	})
	cluster := Cluster{Name: "hub", Clients: &clients.Clients{Operator: h.api.OperatorV1alpha1()}}
	var err error
	h.owner, err = newRoutingConfigOwner(t.Context(), cluster)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *configLifecycleHarness) current() (*operatorv1alpha1.TektonConfig, error) {
	obj, err := h.api.Tracker().Get(configTestGVR, "", "config")
	if err != nil {
		return nil, err
	}
	return obj.(*operatorv1alpha1.TektonConfig), nil
}

func (h *configLifecycleHarness) store(tc *operatorv1alpha1.TektonConfig) error {
	h.revision++
	tc.ResourceVersion = strconv.Itoa(h.revision)
	return h.api.Tracker().Update(configTestGVR, tc, "")
}

func TestRoutingRestoreRejectsForeignValuesAtEveryWriteBoundary(t *testing.T) {
	for _, field := range []string{"CEL", "ConfigMap", "Disabled", "UID"} {
		for _, stage := range []string{"before restore", "disable conflict", "after disable", "enable conflict"} {
			t.Run(field+"/"+stage, func(t *testing.T) {
				// Given: cleanup owns applied CEL config but must not overwrite a
				// foreign value even when it equals a prior lifecycle state.
				h := newConfigLifecycleHarness(t)
				edit := func() error {
					current, err := h.current()
					if err != nil {
						return err
					}
					switch field {
					case "CEL":
						current.Spec.Scheduler.CEL.Expressions = []string{`label("admin", "value")`}
					case "ConfigMap":
						current.Spec.Scheduler.Options.ConfigMaps[routingConfigMap] = corev1.ConfigMap{Data: map[string]string{"config.yaml": "foreign"}}
					case "Disabled":
						current.Spec.Scheduler.Disabled = nil
					case "UID":
						current.UID = "foreign-config"
					}
					return h.store(current)
				}
				if stage == "before restore" {
					if err := edit(); err != nil {
						t.Fatal(err)
					}
				}
				h.onUpdate = func(input *operatorv1alpha1.TektonConfig) error {
					trigger := stage == "disable conflict" || stage == "enable conflict" && !input.Spec.Scheduler.IsDisabled()
					if trigger {
						h.onUpdate = nil
						return edit() // The harness now returns an actual RV conflict.
					}
					return nil
				}
				if stage == "after disable" {
					// Mutate the stored object AFTER an acknowledged disable but
					// before the next Get: this is not a resourceVersion conflict.
					edited := false
					h.api.PrependReactor("get", "tektonconfigs", func(clienttesting.Action) (bool, runtime.Object, error) {
						if h.writes == 1 && !edited {
							edited = true
							return false, nil, edit()
						}
						return false, nil, nil
					})
				}
				target := h.owner.original.DeepCopy()
				target.QueueName = "restored"
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				// When: call the same restart operation registered by cleanup.
				err := h.owner.restart(ctx, *target)
				// Then: no foreign field is adopted or overwritten.
				if !errors.Is(err, errRoutingConfigChanged) {
					t.Fatalf("restore error = %v, want owned-state conflict", err)
				}
				current, getErr := h.current()
				if getErr != nil {
					t.Fatal(getErr)
				}
				if current.Spec.Scheduler.QueueName == target.QueueName {
					t.Fatal("restore overwrote foreign state")
				}
				switch field {
				case "CEL":
					if !reflect.DeepEqual(current.Spec.Scheduler.CEL.Expressions, []string{`label("admin", "value")`}) {
						t.Fatal("lost foreign CEL")
					}
				case "ConfigMap":
					if current.Spec.Scheduler.Options.ConfigMaps[routingConfigMap].Data["config.yaml"] != "foreign" {
						t.Fatal("lost foreign option")
					}
				case "Disabled":
					if current.Spec.Scheduler.Disabled != nil {
						t.Fatal("lost foreign Disabled")
					}
				case "UID":
					if current.UID != "foreign-config" {
						t.Fatal("lost replacement config")
					}
				}
			})
		}
	}
}

func TestRoutingRestoreRecoversAcknowledgedDisableAfterCancellation(t *testing.T) {
	// Given: cancellation immediately after the disable write.
	h := newConfigLifecycleHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	h.onUpdate = func(*operatorv1alpha1.TektonConfig) error {
		h.onUpdate = nil
		cancel()
		return nil
	}
	target := h.owner.original.DeepCopy()
	target.QueueName = "restored"
	// When
	err := h.owner.restart(ctx, *target)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("partial transition error = %v", err)
	}
	recovery, finish := context.WithTimeout(t.Context(), 10*time.Second)
	defer finish()
	if err := h.owner.restart(recovery, *target); err != nil {
		t.Fatal(err)
	}
	// Then: cleanup uses the acknowledged intermediate state, not the initial one.
	current, err := h.current()
	if err != nil || current.Spec.Scheduler.IsDisabled() || current.Spec.Scheduler.QueueName != "restored" {
		t.Fatalf("partial-disable recovery = %+v, %v", current, err)
	}
}
