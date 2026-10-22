package tektonkueue

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

func TestRoutingExistingKueueCleanupConflicts(t *testing.T) {
	for _, mode := range []string{"preserve foreign values", "replacement", "cancel", "exhausted"} {
		t.Run(mode, func(t *testing.T) {
			// Given: setup adds only the missing PipelineRun integrations.
			env := &Environment{Prefix: "test", routingWaits: true}
			initial := env.requiredKueue()
			initial.SetUID("original")
			initial.SetLabels(env.ownedLabels())
			foreign := map[string]any{"group": "foreign.dev", "resource": "jobs", "version": "v1"}
			paths := [][]string{{"spec", "config", "integrations", "externalFrameworks"}, {"spec", "config", "multiKueue", "externalFrameworks"}}
			for _, path := range paths {
				if err := unstructured.SetNestedSlice(initial.Object, []any{foreign}, path...); err != nil {
					t.Fatal(err)
				}
			}
			api := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), initial)
			cluster := Cluster{Name: "hub", Clients: &clients.Clients{Dynamic: api}}
			if err := env.ensureKueue(t.Context(), cluster); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			attempts := 0
			api.PrependReactor("update", "kueues", func(clienttesting.Action) (bool, runtime.Object, error) {
				attempts++
				if attempts > 1 && mode != "exhausted" {
					return false, nil, nil
				}
				current, err := api.Tracker().Get(kueueOperatorGVR, "", "cluster")
				if err != nil {
					return true, nil, err
				}
				kueue := current.(*unstructured.Unstructured)
				kueue.SetAnnotations(map[string]string{"foreign": "preserved"})
				if mode == "replacement" {
					kueue.SetUID("replacement")
				}
				if err := api.Tracker().Update(kueueOperatorGVR, kueue, ""); err != nil {
					return true, nil, err
				}
				if mode == "cancel" {
					cancel()
				}
				return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "kueue.openshift.io", Resource: "kueues"}, "cluster", errors.New("concurrent edit"))
			})
			// When: run the actual registered restoration, not the retry helper.
			err := env.Cleanup(ctx)
			// Then: conflicts refetch without overwriting foreign state.
			switch mode {
			case "preserve foreign values":
				if err != nil || attempts != 2 {
					t.Fatalf("cleanup = %v after %d updates, want success after one conflict", err, attempts)
				}
			case "replacement":
				if err == nil || attempts != 1 {
					t.Fatalf("replacement cleanup = %v after %d updates", err, attempts)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) || attempts != 1 {
					t.Fatalf("canceled cleanup = %v after %d updates", err, attempts)
				}
			case "exhausted":
				if !apierrors.IsConflict(err) || attempts != 5 {
					t.Fatalf("bounded cleanup = %v after %d updates, want 5 conflicts", err, attempts)
				}
			}
			current, err := api.Resource(kueueOperatorGVR).Get(t.Context(), "cluster", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if current.GetAnnotations()["foreign"] != "preserved" {
				t.Fatal("lost foreign Kueue annotation")
			}
			for _, path := range paths {
				items, _, err := unstructured.NestedSlice(current.Object, path...)
				if err != nil {
					t.Fatal(err)
				}
				if len(items) == 0 || items[0].(map[string]any)["group"] != "foreign.dev" {
					t.Fatal("lost foreign framework")
				}
				if mode == "preserve foreign values" && len(items) != 1 {
					t.Fatalf("owned integration was not restored: %v", items)
				}
			}
			if mode == "replacement" && current.GetUID() != "replacement" {
				t.Fatal("replacement Kueue was overwritten")
			}
		})
	}
}
