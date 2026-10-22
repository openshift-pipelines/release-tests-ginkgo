package tektonkueue

import (
	"net/http"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

func TestPlacementRetainsTransientForbiddenIdentitiesBeforeBindAndAfterCompletion(t *testing.T) {
	for _, phase := range []string{"before bind", "during observation", "after terminal success"} {
		for _, scenario := range []string{"hub TaskRun", "hub Pod", "wrong-spoke TaskRun", "wrong-spoke Pod", "duplicate TaskRun", "duplicate Pod"} {
			t.Run(phase+"/"+scenario, func(t *testing.T) {
				f := executionEnvironment(t)
				armed := armFixture(t, f, proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) }))
				f.start()
				if phase != "before bind" {
					if err := armed.watches.bind(f.run); err != nil {
						t.Fatal(err)
					}
				}
				if err := armed.watches.drain(armed.ctx); err != nil {
					t.Fatal(err)
				}
				gvr, name := taskRunGVR, "task"
				if scenario == "hub Pod" || scenario == "wrong-spoke Pod" || scenario == "duplicate Pod" {
					gvr, name = podGVR, wireRun.pod
				}
				source, err := f.dynamic["spoke-1"].Resource(gvr).Namespace(wireRun.namespace).Get(armed.ctx, name, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if phase == "after terminal success" {
					if err := f.complete(armed.ctx); err != nil {
						t.Fatal(err)
					}
				}
				target := "spoke-1"
				switch scenario {
				case "hub TaskRun", "hub Pod":
					target = "hub"
				case "wrong-spoke TaskRun", "wrong-spoke Pod":
					target = "spoke-2"
				default:
					source.SetName("duplicate")
					source.SetUID("duplicate-uid")
				}
				resource := f.dynamic[target].Resource(gvr).Namespace(wireRun.namespace)
				if _, err := resource.Create(armed.ctx, source, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := resource.Delete(armed.ctx, source.GetName(), metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				if phase == "before bind" {
					if err := armed.watches.drain(armed.ctx); err != nil {
						t.Fatal(err)
					}
					err = armed.watches.bind(f.run)
				} else {
					err = armed.watches.wait(armed.ctx, func() (bool, error) { return true, nil })
				}
				if err == nil {
					t.Fatal("accepted a transient forbidden identity absent from current snapshots")
				}
			})
		}
	}
}

func TestBoundIdentitiesRejectReplacementsAndChangedOwners(t *testing.T) {
	for _, scenario := range []string{"hub run", "spoke run", "TaskRun", "Pod", "Workload", "task owner", "pod owner", "workload owner", "task pod link"} {
		t.Run(scenario, func(t *testing.T) {
			f, armed := boundFixture(t)
			cluster, gvr, name := "spoke-1", taskRunGVR, wireRun.task
			switch scenario {
			case "hub run":
				cluster, gvr, name = "hub", pipelineRunGVR, wireRun.name
			case "spoke run":
				gvr, name = pipelineRunGVR, wireRun.name
			case "Pod", "pod owner":
				gvr, name = podGVR, wireRun.pod
			case "Workload", "workload owner":
				cluster, gvr, name = "hub", workloadGVR, wireRun.workload
			}
			resource := f.dynamic[cluster].Resource(gvr).Namespace(wireRun.namespace)
			object, err := resource.Get(armed.ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "task owner", "pod owner", "workload owner":
				owners := object.GetOwnerReferences()
				owners[0].UID = "wrong-parent"
				object.SetOwnerReferences(owners)
				_, err = resource.Update(armed.ctx, object, metav1.UpdateOptions{})
			case "task pod link":
				object.Object["status"] = map[string]any{"podName": "other-pod"}
				_, err = resource.Update(armed.ctx, object, metav1.UpdateOptions{})
			default:
				if err := resource.Delete(armed.ctx, name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				object.SetUID("replacement")
				_, err = resource.Create(armed.ctx, object, metav1.CreateOptions{})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := armed.watches.wait(armed.ctx, func() (bool, error) { return true, nil }); err == nil {
				t.Fatal("accepted changed bound identity")
			}
		})
	}
}

func TestOwnerLinkedPodWithoutRunLabelIsStillClassified(t *testing.T) {
	f, armed := boundFixture(t)
	pod, err := f.dynamic["spoke-1"].Resource(podGVR).Namespace(wireRun.namespace).Get(armed.ctx, wireRun.pod, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pod.SetLabels(nil)
	pod.SetName("duplicate")
	pod.SetUID("duplicate")
	if _, err := f.dynamic["spoke-1"].Resource(podGVR).Namespace(wireRun.namespace).Create(armed.ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := armed.watches.wait(armed.ctx, func() (bool, error) { return true, nil }); err == nil {
		t.Fatal("ignored owner-linked duplicate Pod")
	}
}

func TestExecutionHistoryBoundIsFailClosed(t *testing.T) {
	f, armed := boundFixture(t)
	var sub *executionSubscription
	for _, s := range armed.watches.subscriptions {
		if s.cluster.Name == "hub" && s.gvr == pipelineRunGVR {
			sub = s
			break
		}
	}
	if sub == nil {
		t.Fatal("missing hub subscription")
	}
	original := armed.watches.events[0]
	for len(armed.watches.events) < 4096 {
		armed.watches.events = append(armed.watches.events, original)
	}
	object, err := f.dynamic["hub"].Resource(pipelineRunGVR).Namespace(wireRun.namespace).Get(armed.ctx, wireRun.name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := armed.watches.record(sub, watch.Modified, object); err == nil {
		t.Fatal("history exhaustion was ignored")
	}
}

func TestUIDReplacementsAfterTerminalSuccessCannotBeAcceptedAsCleanup(t *testing.T) {
	for _, gvr := range []schema.GroupVersionResource{pipelineRunGVR, taskRunGVR, podGVR, workloadGVR} {
		t.Run(gvr.Resource, func(t *testing.T) {
			f, armed := boundFixture(t)
			cluster := "spoke-1"
			if gvr == workloadGVR {
				cluster = "hub"
			}
			identity := *armed.watches.retained[cluster].member(gvr)
			replacement := identity.object.DeepCopy()
			if err := f.complete(armed.ctx); err != nil {
				t.Fatal(err)
			}
			if gvr == workloadGVR {
				if err := f.dynamic[cluster].Resource(gvr).Namespace(wireRun.namespace).Delete(armed.ctx, replacement.GetName(), metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			replacement.SetUID("replacement-after-success")
			if _, err := f.dynamic[cluster].Resource(gvr).Namespace(wireRun.namespace).Create(armed.ctx, replacement, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := armed.watches.wait(armed.ctx, func() (bool, error) { return true, nil }); err == nil {
				t.Fatal("accepted post-terminal identity replacement")
			}
		})
	}
}

func TestUnownedCandidateWorkloadIsNotExecutionButMustBeCleaned(t *testing.T) {
	f, armed := boundFixture(t)
	if len(armed.watches.seen) != 1 || armed.watches.seen["spoke-2"] {
		t.Fatal("candidate Workload counted as execution")
	}
	f.holdCandidate = true
	if err := f.complete(armed.ctx); err != nil {
		t.Fatal(err)
	}
	checked := make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	go func() {
		done <- armed.watches.wait(armed.ctx, func() (bool, error) {
			ready, err := armed.workerCleanup(armed.ctx)
			if !ready {
				once.Do(func() { close(checked) })
			}
			return ready, err
		})
	}()
	receiveReadinessEvent(t, armed.ctx, checked)
	select {
	case err := <-done:
		t.Fatalf("cleanup ignored unowned candidate: %v", err)
	default:
	}
	if err := f.dynamic["spoke-2"].Resource(workloadGVR).Namespace(wireRun.namespace).Delete(armed.ctx, wireRun.workload, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := receiveReadinessEvent(t, armed.ctx, done); err != nil {
		t.Fatal(err)
	}
}
