package kindmulticluster

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"sync"
	"testing"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	pipelineclient "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

func TestBatchCreatesTenDistinctOwnedRuns(t *testing.T) {
	// Given an HTTP API with real client serialization and server-assigned UIDs.
	server, api := runAPIServer(t, 0)
	defer server.Close()
	batch := apiBatch(t, server)

	// When ten submissions succeed.
	for range 10 {
		if _, err := batch.create(t.Context(), func(*scaleRun) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}

	// Then all ten names, objects, and ownership identities are independent.
	stored := api.snapshot()
	if len(stored) != 10 || len(batch.runs) != 10 {
		t.Fatalf("API objects=%d owned runs=%d", len(stored), len(batch.runs))
	}
	uids := map[types.UID]bool{}
	states := map[*scaleRun]bool{}
	for _, run := range batch.runs {
		if run.uid == "" || uids[run.uid] || states[run] {
			t.Fatal("run ownership state was shared or empty")
		}
		uids[run.uid], states[run] = true, true
		if stored[run.name].UID != run.uid {
			t.Fatalf("ownership of %s does not match the API response", run.name)
		}
	}
	if err := batch.cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if remaining := api.snapshot(); len(remaining) != 0 {
		t.Fatalf("owned runs remain after cleanup: %v", remaining)
	}
}

func TestPartialBatchCleansAllOwnedRunsWithoutDeletingReplacement(t *testing.T) {
	// Given a server that rejects create six and replaces an earlier run.
	server, api := runAPIServer(t, 6)
	defer server.Close()
	workerServer, workerAPI := runAPIServer(t, 0)
	defer workerServer.Close()
	batch := apiBatch(t, server)
	batch.workers = []pipelineclient.PipelineRunInterface{apiBatch(t, workerServer).hub}
	var createErr error
	for range 10 {
		if _, createErr = batch.create(t.Context(), func(*scaleRun) error { return nil }); createErr != nil {
			break
		}
	}
	if createErr == nil || len(batch.runs) != 5 {
		t.Fatalf("partial create error=%v owned=%d", createErr, len(batch.runs))
	}
	replaced := batch.runs[1].name
	api.mu.Lock()
	api.stored[replaced].UID = "replacement-uid"
	api.mu.Unlock()
	workerAPI.mu.Lock()
	for _, run := range batch.runs {
		worker := &pipelinev1.PipelineRun{ObjectMeta: metav1.ObjectMeta{
			Name: run.name, UID: types.UID("worker-" + run.name),
		}}
		run.workers[0] = worker.DeepCopy()
		workerAPI.stored[run.name] = worker
	}
	replacedWorker := batch.runs[2].name
	workerAPI.stored[replacedWorker].UID = "replacement-worker-uid"
	workerAPI.mu.Unlock()

	// When cleanup encounters a UID conflict in the middle of the batch.
	cleanupErr := batch.cleanup(t.Context())

	// Then it retains the replacement, reports the conflict, and continues.
	stored := api.snapshot()
	if cleanupErr == nil || len(stored) != 1 || stored[replaced] == nil || stored[replaced].UID != "replacement-uid" {
		t.Fatalf("cleanup error=%v remaining=%v", cleanupErr, stored)
	}
	remainingWorkers := workerAPI.snapshot()
	if len(remainingWorkers) != 1 || remainingWorkers[replacedWorker] == nil ||
		remainingWorkers[replacedWorker].UID != "replacement-worker-uid" {
		t.Fatalf("worker replacements or cleanup were mishandled: %v", remainingWorkers)
	}
}

func apiBatch(t *testing.T, server *httptest.Server) *runBatch {
	t.Helper()
	client, err := pipelineclient.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return newRunBatch(client.PipelineRuns(namespace))
}

type runAPI struct {
	mu     sync.Mutex
	stored map[string]*pipelinev1.PipelineRun
}

func (a *runAPI) snapshot() map[string]*pipelinev1.PipelineRun {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := map[string]*pipelinev1.PipelineRun{}
	for name, run := range a.stored {
		result[name] = run.DeepCopy()
	}
	return result
}

func runAPIServer(t *testing.T, failCreate int) (*httptest.Server, *runAPI) {
	t.Helper()
	stored := map[string]*pipelinev1.PipelineRun{}
	api := &runAPI{stored: stored}
	creates := 0
	// Tests call this API serially. Every response completes before inspection.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case http.MethodPost:
			creates++
			if creates == failCreate {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			run := &pipelinev1.PipelineRun{}
			if err := json.NewDecoder(req.Body).Decode(run); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			run.UID = types.UID(fmt.Sprintf("uid-%d", creates))
			stored[run.Name] = run
			w.WriteHeader(http.StatusCreated)
			if err := json.NewEncoder(w).Encode(run); err != nil {
				t.Error(err)
			}
		case http.MethodDelete:
			name := path.Base(req.URL.Path)
			var options metav1.DeleteOptions
			if err := json.NewDecoder(req.Body).Decode(&options); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			existing := stored[name]
			if existing == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if options.Preconditions == nil || options.Preconditions.UID == nil || *options.Preconditions.UID != existing.UID {
				w.WriteHeader(http.StatusConflict)
				return
			}
			delete(stored, name)
			if err := json.NewEncoder(w).Encode(metav1.Status{Status: metav1.StatusSuccess}); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected API request: %s %s", req.Method, req.URL)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	return server, api
}
