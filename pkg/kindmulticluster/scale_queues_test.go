package kindmulticluster

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func TestReadScaleQueueChecksActualAPIQuota(t *testing.T) {
	for _, tc := range []struct {
		name  string
		quota any
		fail  bool
	}{
		{"ten-integer", int64(10), false},
		{"ten-quantity", "10", false},
		{"wrong-quota", int64(2), true},
		{"fractional-quota", "10500m", true},
		{"malformed-quota", "not-a-quantity", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a real HTTP read whose quota differs from any local fixture.
			queue := queueObject(tc.quota)
			called := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				called <- struct{}{}
				if req.Method != http.MethodGet || req.URL.Path != "/apis/kueue.x-k8s.io/v1beta2/clusterqueues/"+namespace {
					t.Errorf("unexpected API request: %s %s", req.Method, req.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(queue); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client, err := dynamic.NewForConfig(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}

			// When the scenario reads back its configured quota.
			_, err = readScaleQueue(t.Context(), client)

			// Then only an API-reported quota of exactly ten is accepted.
			select {
			case <-called:
			default:
				t.Fatal("quota check did not read the API")
			}
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v want failure=%t", err, tc.fail)
			}
		})
	}
}

func TestEligibilityRequiresAcknowledgedGenerationAndStoppedReason(t *testing.T) {
	for _, tc := range []struct {
		name, policy, status, reason string
		observed                     int64
		ready                        bool
	}{
		{"stale-stop", "Hold", "False", "Stopped", 1, false},
		{"unrelated-inactive", "Hold", "False", "FlavorNotFound", 2, false},
		{"acknowledged-stop", "Hold", "False", "Stopped", 2, true},
		{"stale-enable", "None", "True", "Ready", 1, false},
		{"acknowledged-enable", "None", "True", "Ready", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a controller observation of a queue generation.
			queue := queueObject(int64(10))
			queue.Object["status"] = map[string]any{"conditions": []any{map[string]any{
				"type": "Active", "status": tc.status, "reason": tc.reason, "observedGeneration": tc.observed,
			}}}

			// When the phase checks whether eligibility changed.
			ready, err := queueAcknowledged(queue, 2, tc.policy)

			// Then a stale status or unrelated failure cannot release a run.
			if err != nil || ready != tc.ready {
				t.Fatalf("ready=%t error=%v want=%t", ready, err, tc.ready)
			}
		})
	}
}

func queueObject(quota any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta2", "kind": "ClusterQueue",
		"metadata": map[string]any{"name": namespace, "uid": "queue-uid", "generation": int64(2)},
		"spec": map[string]any{"resourceGroups": []any{map[string]any{
			"coveredResources": []any{"tekton.dev/pipelineruns"},
			"flavors": []any{map[string]any{"name": namespace, "resources": []any{map[string]any{
				"name": "tekton.dev/pipelineruns", "nominalQuota": quota,
			}}}},
		}}},
	}}
}
