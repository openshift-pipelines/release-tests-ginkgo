package kindmulticluster

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func TestSubscriptionEstablishedBeforeWorkloadRelease(t *testing.T) {
	// Given all observations except the worker's execution, held behind a signal.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	run := newScaleRun("subscription-run")
	run.uid = "hub-uid"
	run.ctx, run.cancel = context.WithCancel(ctx)
	release := make(chan struct{})
	var worker observedEvent
	for _, event := range successfulExecution(run, 0) {
		if event.worker == 0 && event.resource == "pipelineruns" {
			worker = event
		} else {
			deliver(t, run, event)
		}
	}
	object := unstructuredObject(t, worker.object)
	object.SetAPIVersion("tekton.dev/v1")
	object.SetKind("PipelineRun")
	object.SetResourceVersion("2")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Query().Get("fieldSelector") != "metadata.name="+run.name {
			t.Error("watch lost its per-run field selector")
		}
		if req.URL.Query().Get("watch") != "true" {
			if err := json.NewEncoder(w).Encode(map[string]any{
				"apiVersion": "tekton.dev/v1", "kind": "PipelineRunList",
				"metadata": map[string]any{"resourceVersion": "1"}, "items": []any{},
			}); err != nil {
				t.Error(err)
			}
			return
		}
		if req.URL.Query().Get("resourceVersion") != "1" {
			t.Error("watch did not resume from the listed resource version")
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		select {
		case <-release:
		case <-req.Context().Done():
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"type": "ADDED", "object": object.Object}); err != nil {
			t.Error(err)
			return
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		<-req.Context().Done()
	}))
	defer server.Close()
	defer run.close()
	client, err := dynamic.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	source := runSource{
		worker: 0, resource: "pipelineruns",
		client:  client.Resource(schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}).Namespace(namespace),
		options: metav1.ListOptions{FieldSelector: "metadata.name=" + run.name},
	}
	if err := run.subscribeSource(source); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := run.progress(runFinished); err != nil || ready {
		t.Fatalf("completion before release: ready=%t error=%v", ready, err)
	}

	// When the already-subscribed stream receives the released execution.
	close(release)
	placement, err := run.wait(ctx, runFinished)

	// Then it observes completion without sleeps, polling, or a later GET race.
	if err != nil || placement.worker != 0 {
		t.Fatalf("placement=%+v error=%v", placement, err)
	}
}
