package tektonkueue

import (
	"context"
	"net/http"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestAssertReadyWhenEveryPrerequisiteIsReady(t *testing.T) {
	// Given current CRs, observed Deployments, active queues/workers and a live /ready response.
	environment := readyEnvironment(t, nil)
	// When
	err := environment.AssertReady(context.Background())
	// Then
	if err != nil {
		t.Fatalf("AssertReady() = %v", err)
	}
}

func TestAssertReadyRejectsMissingStaleOrUnreadyResources(t *testing.T) {
	for _, cluster := range []string{"hub", "spoke-1"} {
		fixture := readyClusterFixture(t, cluster)
		for path, object := range fixture.objects {
			mutations := []string{"missing", "terminating"}
			if _, found := object.Object["status"]; found {
				mutations = append(mutations, "unready")
				switch object.GetKind() {
				case "TektonConfig", "TektonScheduler", "TektonMulticlusterProxyAAE":
					// These v0.80 controllers do not publish generation observations.
				default:
					mutations = append(mutations, "stale")
				}
			}
			for _, mutation := range mutations {
				t.Run(cluster+path+"/"+mutation, func(t *testing.T) {
					// Given exactly one missing, stale, terminating or unready dependency.
					environment := readyEnvironment(t, func(name string, fixture *readinessFixture) {
						if name != cluster {
							return
						}
						object := fixture.objects[path]
						switch mutation {
						case "missing":
							delete(fixture.objects, path)
						case "terminating":
							deletion := metav1.NewTime(metav1.Unix(1, 0).Time)
							object.SetDeletionTimestamp(&deletion)
						case "stale":
							object.SetGeneration(3)
						case "unready":
							if object.GetKind() == "Deployment" {
								object.Object["status"].(map[string]any)["availableReplicas"] = int64(0)
							} else {
								conditions := object.Object["status"].(map[string]any)["conditions"].([]any)
								conditions[0].(map[string]any)["status"] = "False"
							}
						}
					})
					// When
					err := environment.AssertReady(context.Background())
					// Then
					if err == nil {
						t.Fatalf("AssertReady() accepted %s %s %s", cluster, path, mutation)
					}
					if !strings.Contains(err.Error(), cluster) {
						t.Fatalf("error does not identify cluster %s: %v", cluster, err)
					}
				})
			}
		}
	}
}

func TestAssertReadyRejectsProxyFailures(t *testing.T) {
	for _, scenario := range []string{"unavailable", "no pods", "no ready probe"} {
		t.Run(scenario, func(t *testing.T) {
			// Given a ready control plane but no successful proxy /ready.
			environment := readyEnvironment(t, func(name string, fixture *readinessFixture) {
				if name != "hub" {
					return
				}
				switch scenario {
				case "unavailable":
					fixture.proxyStatus = http.StatusServiceUnavailable
				case "no pods":
					fixture.pods = nil
				case "no ready probe":
					fixture.pods[0].Spec.Containers[0].ReadinessProbe = nil
				}
			})
			// When
			err := environment.AssertReady(context.Background())
			// Then
			if err == nil || !strings.Contains(err.Error(), "proxy") {
				t.Fatalf("AssertReady() = %v, want proxy readiness failure", err)
			}
		})
	}
}

func TestAssertReadyRejectsIncompleteDeploymentRollouts(t *testing.T) {
	for _, scenario := range []string{"scaled to zero", "old replicas", "unavailable condition", "unrelated labels"} {
		t.Run(scenario, func(t *testing.T) {
			// Given a Deployment that cannot prove the component is available.
			environment := readyEnvironment(t, func(name string, fixture *readinessFixture) {
				if name != "hub" {
					return
				}
				deployment := fixture.objects["/apis/apps/v1/namespaces/openshift-pipelines/deployments/scheduler"]
				switch scenario {
				case "scaled to zero":
					deployment.Object["spec"].(map[string]any)["replicas"] = int64(0)
				case "old replicas":
					deployment.Object["status"].(map[string]any)["updatedReplicas"] = int64(0)
				case "unavailable condition":
					conditions := deployment.Object["status"].(map[string]any)["conditions"].([]any)
					conditions[0].(map[string]any)["status"] = "False"
				case "unrelated labels":
					deployment.SetLabels(nil)
				}
			})
			// When
			err := environment.AssertReady(context.Background())
			// Then
			if err == nil || !strings.Contains(err.Error(), "scheduler") {
				t.Fatalf("AssertReady() = %v, want scheduler Deployment failure", err)
			}
		})
	}
}

func TestAssertReadyRejectsDisabledOrMisconfiguredMulticluster(t *testing.T) {
	for _, field := range []string{"disabled", "multi-cluster-disabled", "multi-cluster-role", "queue"} {
		t.Run(field, func(t *testing.T) {
			// Given a Ready TektonConfig whose scheduler no longer matches this run.
			environment := readyEnvironment(t, func(name string, fixture *readinessFixture) {
				if name != "hub" {
					return
				}
				tc := fixture.objects["/apis/operator.tekton.dev/v1alpha1/tektonconfigs/config"]
				scheduler := tc.Object["spec"].(map[string]any)["scheduler"].(map[string]any)
				switch field {
				case "disabled", "multi-cluster-disabled":
					scheduler[field] = true
				case "multi-cluster-role":
					scheduler[field] = "Spoke"
				case "queue":
					scheduler["config.yaml"].(map[string]any)["queueName"] = "another-queue"
				}
			})
			// When
			err := environment.AssertReady(context.Background())
			// Then
			if err == nil || !strings.Contains(err.Error(), "TektonConfig") {
				t.Fatalf("AssertReady() = %v, want scheduler configuration failure", err)
			}
		})
	}
}

func TestResourceReadyRejectsInvalidConditions(t *testing.T) {
	for _, scenario := range []string{"missing condition", "missing observation", "status stale", "degraded", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			// Given an invalid or stale status.
			object := &unstructured.Unstructured{Object: map[string]any{
				"metadata": map[string]any{"generation": int64(2)},
				"status":   readyResourceStatus(t, "SyncerService", "Ready"),
			}}
			status := object.Object["status"].(map[string]any)
			switch scenario {
			case "missing condition":
				delete(status, "conditions")
			case "missing observation":
				delete(status, "observedGeneration")
			case "status stale":
				status["observedGeneration"] = int64(1)
			case "degraded":
				status["conditions"] = append(status["conditions"].([]any), map[string]any{"type": "Degraded", "status": "True"})
			case "malformed":
				status["conditions"] = []any{"bad condition"}
			}
			// When
			err := resourceReady(object, readinessResource{gvr: operatorResource("syncerservices"), condition: "Ready"})
			// Then
			if err == nil {
				t.Fatal("resourceReady() accepted invalid status")
			}
		})
	}
}

func TestAssertReadyRejectsUnpropagatedOperatorConfiguration(t *testing.T) {
	for _, scenario := range []string{"scheduler role", "scheduler queue", "scheduler namespace", "proxy namespace"} {
		t.Run(scenario, func(t *testing.T) {
			// Given Ready=True without generation observations, but an old operand spec.
			environment := readyEnvironment(t, func(name string, fixture *readinessFixture) {
				if name != "hub" {
					return
				}
				path := "/apis/operator.tekton.dev/v1alpha1/tektonschedulers/scheduler"
				if scenario == "proxy namespace" {
					path = "/apis/operator.tekton.dev/v1alpha1/tektonmulticlusterproxyaaes/multicluster-proxy-aae"
				}
				spec := fixture.objects[path].Object["spec"].(map[string]any)
				switch scenario {
				case "scheduler role":
					spec["multi-cluster-role"] = "Spoke"
				case "scheduler queue":
					spec["config.yaml"].(map[string]any)["queueName"] = "old-queue"
				case "scheduler namespace", "proxy namespace":
					spec["targetNamespace"] = "old-namespace"
				}
			})
			// When
			err := environment.AssertReady(context.Background())
			// Then
			if err == nil || !strings.Contains(err.Error(), "has not converged") {
				t.Fatalf("AssertReady() = %v, want stale spec propagation failure", err)
			}
		})
	}
}

func TestResourceReadyRequiresSupportedObservations(t *testing.T) {
	for _, resource := range []readinessResource{
		{gvr: kueueOperatorGVR, condition: "Available"},
		{gvr: operatorResource("syncerservices"), condition: "Ready"},
		{gvr: clusterQueueGVR, condition: "Active"},
		{gvr: localQueueGVR, condition: "Active"},
		{gvr: admissionCheckGVR, condition: "Active"},
		{gvr: multiKueueClusterGVR, condition: "Active"},
	} {
		t.Run(resource.gvr.Resource, func(t *testing.T) {
			// Given Ready/Active=True but no supported generation observation.
			object := &unstructured.Unstructured{Object: map[string]any{
				"metadata": map[string]any{"generation": int64(2)},
				"status": map[string]any{"conditions": []any{
					map[string]any{"type": resource.condition, "status": "True"},
				}},
			}}
			// When
			err := resourceReady(object, resource)
			// Then
			if err == nil || !strings.Contains(err.Error(), "stale") {
				t.Fatalf("resourceReady() = %v, want missing observation failure", err)
			}
		})
	}
}
