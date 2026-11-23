package pipelines

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

func TestNestedResolverFixture(t *testing.T) {
	data, err := os.ReadFile(config.Path("testdata/pipelines-in-pipelines/resolver/child.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var pipeline v1.Pipeline
	if err := yaml.Unmarshal(data, &pipeline); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(pipeline.Spec.Tasks) != 1 || pipeline.Spec.Tasks[0].TaskSpec == nil {
		t.Fatal("fixture must execute one embedded leaf task")
	}
	steps := pipeline.Spec.Tasks[0].TaskSpec.Steps
	if len(steps) != 1 || len(steps[0].Env) != 1 || steps[0].Env[0].Name != "SOURCE" {
		t.Fatal("fixture must expose the source marker customized by each scenario")
	}
	for _, step := range pipeline.Spec.Tasks[0].TaskSpec.Steps {
		if !strings.Contains(step.Image, "@sha256:") {
			t.Fatalf("leaf image is not pinned: %s", step.Image)
		}
	}
}

func TestWaitNestedResolverConfig(t *testing.T) {
	for _, scenario := range []string{"current", "updated", "closed", "API error", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			config := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "system"},
				Data: map[string]string{"allowed-namespaces": "old"}}
			if scenario == "current" {
				config.Data["allowed-namespaces"] = "test"
			}
			client := fake.NewClientset(config)
			events := make(chan watch.Event, 1)
			switch scenario {
			case "updated":
				updated := config.DeepCopy()
				updated.Data["allowed-namespaces"] = "test"
				events <- watch.Event{Type: watch.Modified, Object: updated}
			case "API error":
				events <- watch.Event{Type: watch.Error, Object: &metav1.Status{Reason: metav1.StatusReasonForbidden, Code: 403}}
			}
			if scenario != "canceled" {
				close(events)
			}
			client.PrependWatchReactor("configmaps", func(action ktesting.Action) (bool, watch.Interface, error) {
				if action.GetNamespace() != "system" {
					t.Errorf("watch namespace = %q", action.GetNamespace())
				}
				return true, watch.NewProxyWatcher(events), nil
			})
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if scenario == "canceled" {
				cancel()
			}
			err := WaitNestedResolverConfig(ctx, client.CoreV1().ConfigMaps("system"), "policy",
				map[string]string{"allowed-namespaces": "test"})
			wantSuccess := scenario == "current" || scenario == "updated"
			if (err == nil) != wantSuccess || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%s: error=%v", scenario, err)
			}
			if scenario == "API error" && !apierrors.IsForbidden(err) {
				t.Fatalf("API error cause lost: %v", err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation cause lost: %v", err)
			}
		})
	}
}

func TestRestartNestedResolversWaitsForReplacementPods(t *testing.T) {
	for _, scenario := range []string{"replaced", "old still present", "replacement not ready", "identity changed", "API error"} {
		t.Run(scenario, func(t *testing.T) {
			old := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "resolver-old", Namespace: "system", UID: "old",
					Labels: map[string]string{"app.kubernetes.io/component": "resolvers", "app.kubernetes.io/part-of": "tekton-pipelines"}},
				Spec:   corev1.PodSpec{ServiceAccountName: "resolver"},
				Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
			}
			replacement := old.DeepCopy()
			replacement.Name, replacement.UID = "resolver-new", "new"
			if scenario == "replacement not ready" {
				replacement.Status.Conditions[0].Status = corev1.ConditionFalse
			}
			if scenario == "identity changed" {
				replacement.Spec.ServiceAccountName = "other"
			}
			events := make(chan watch.Event, 2)
			if scenario == "API error" {
				events <- watch.Event{Type: watch.Error, Object: &metav1.Status{Reason: metav1.StatusReasonForbidden, Code: 403}}
			} else {
				events <- watch.Event{Type: watch.Added, Object: replacement}
				if scenario != "old still present" {
					events <- watch.Event{Type: watch.Deleted, Object: old}
				}
			}
			close(events)
			client := fake.NewClientset(old)
			subscribed := false
			client.PrependWatchReactor("pods", func(ktesting.Action) (bool, watch.Interface, error) {
				subscribed = true
				return true, watch.NewProxyWatcher(events), nil
			})
			client.PrependReactor("delete", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
				if !subscribed {
					t.Error("pod deleted before subscribing to its replacement")
				}
				options := action.(ktesting.DeleteAction).GetDeleteOptions()
				if options.Preconditions == nil || options.Preconditions.UID == nil || *options.Preconditions.UID != old.UID {
					t.Error("pod deletion must be pinned to the original UID")
				}
				return true, nil, nil
			})
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			identity, err := RestartNestedResolvers(ctx, client, "system")
			if scenario == "replaced" {
				if err != nil || identity != "resolver" {
					t.Fatalf("identity=%q error=%v", identity, err)
				}
			} else if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("%s: identity=%q error=%v", scenario, identity, err)
			}
			if scenario == "API error" && !apierrors.IsForbidden(err) {
				t.Fatalf("API error cause lost: %v", err)
			}
		})
	}
}

func TestNestedPipelineReadAllowedUsesSelectedIdentity(t *testing.T) {
	for _, scenario := range []string{"resolver allowed", "leaf denied", "evaluation error", "API error"} {
		t.Run(scenario, func(t *testing.T) {
			identity := types.NamespacedName{Namespace: "system", Name: "resolver"}
			if scenario == "leaf denied" {
				identity = types.NamespacedName{Namespace: "runner", Name: "leaf"}
			}
			client := fake.NewClientset()
			client.PrependReactor("create", "subjectaccessreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
				request := action.(ktesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
				if request.Spec.User != "system:serviceaccount:"+identity.Namespace+":"+identity.Name ||
					!slices.Equal(request.Spec.Groups, []string{"system:serviceaccounts", "system:serviceaccounts:" + identity.Namespace, "system:authenticated"}) {
					t.Errorf("wrong review identity: %+v", request.Spec)
				}
				want := authorizationv1.ResourceAttributes{Namespace: "source", Verb: "get", Group: "tekton.dev", Version: "v1", Resource: "pipelines", Name: "pinned"}
				if request.Spec.ResourceAttributes == nil || *request.Spec.ResourceAttributes != want {
					t.Errorf("wrong review resource: %+v", request.Spec.ResourceAttributes)
				}
				if scenario == "API error" {
					return true, nil, errors.New("review failed")
				}
				request.Status.Allowed = scenario != "leaf denied"
				if scenario == "evaluation error" {
					request.Status.EvaluationError = "authorizer unavailable"
				}
				return true, request, nil
			})
			allowed, err := NestedPipelineReadAllowed(t.Context(), client, identity, types.NamespacedName{Namespace: "source", Name: "pinned"})
			if scenario == "evaluation error" || scenario == "API error" {
				if err == nil || allowed {
					t.Fatalf("error=%v allowed=%t", err, allowed)
				}
			} else if err != nil || allowed != (scenario == "resolver allowed") {
				t.Fatalf("error=%v allowed=%t", err, allowed)
			}
		})
	}
}
