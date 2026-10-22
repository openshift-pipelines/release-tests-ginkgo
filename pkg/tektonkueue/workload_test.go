package tektonkueue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

func TestNewPipelineRunLeavesWebhookDefaultsUnset(t *testing.T) {
	environment := &Environment{Prefix: "test", Namespace: "test"}
	for _, test := range []struct {
		name        string
		prelabelled bool
		wantQueue   string
	}{
		{name: "defaulted"},
		{name: "prelabelled", prelabelled: true, wantQueue: "test-explicit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			run, expectedQueue := environment.newPipelineRun(test.name, test.prelabelled)
			if run.Spec.ManagedBy != nil || run.Spec.Status != "" {
				t.Fatalf("newPipelineRun() set webhook-owned fields: managedBy=%v, status=%q", run.Spec.ManagedBy, run.Spec.Status)
			}
			queue, found := run.Labels[queueLabel]
			if test.wantQueue == "" && found {
				t.Fatalf("newPipelineRun() queue label = %q, want absent", queue)
			}
			if test.wantQueue != "" && (!found || queue != test.wantQueue) {
				t.Fatalf("newPipelineRun() queue label = %q, want %q", queue, test.wantQueue)
			}
			wantExpected := test.wantQueue
			if wantExpected == "" {
				wantExpected = environment.Prefix
			}
			if expectedQueue != wantExpected {
				t.Fatalf("newPipelineRun() expected queue = %q, want %q", expectedQueue, wantExpected)
			}
		})
	}
}

func TestValidateHubAdmission(t *testing.T) {
	managedBy := multiKueueController
	admitted := &pipelinev1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{Name: "test-run", Labels: map[string]string{queueLabel: "test-queue"}},
		Spec: pipelinev1.PipelineRunSpec{
			ManagedBy: &managedBy,
			Status:    pipelinev1.PipelineRunSpecStatusPending,
		},
	}
	tests := []struct {
		name    string
		mutate  func(*pipelinev1.PipelineRun)
		wantErr string
	}{
		{name: "admitted"},
		{name: "queue changed", mutate: func(run *pipelinev1.PipelineRun) { run.Labels[queueLabel] = "other" }, wantErr: "queue label"},
		{name: "managedBy missing", mutate: func(run *pipelinev1.PipelineRun) { run.Spec.ManagedBy = nil }, wantErr: "managedBy"},
		{name: "managedBy changed", mutate: func(run *pipelinev1.PipelineRun) { value := "other"; run.Spec.ManagedBy = &value }, wantErr: "managedBy"},
		{name: "status not pending", mutate: func(run *pipelinev1.PipelineRun) { run.Spec.Status = "" }, wantErr: "spec.status"},
	}
	environment := &Environment{Prefix: "test-queue"}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := admitted.DeepCopy()
			if test.mutate != nil {
				test.mutate(run)
			}
			err := environment.validateHubAdmission(run, environment.Prefix)
			if test.wantErr == "" && err != nil {
				t.Fatalf("validateHubAdmission() error = %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("validateHubAdmission() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestPipelineRunLogsReadsSelectedSpoke(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/namespaces/test-ns/pods":
			if got, want := r.URL.Query().Get("labelSelector"), "tekton.dev/pipelineRun=test-run"; got != want {
				t.Errorf("labelSelector = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(corev1.PodList{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"},
				Items:    []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "worker-pod"}}},
			})
		case "/api/v1/namespaces/test-ns/pods/worker-pod/log":
			if got, want := r.URL.Query().Get("container"), workloadLogContainer; got != want {
				t.Errorf("container = %q, want %q", got, want)
			}
			_, _ = w.Write([]byte(workloadLogMarker + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	kube, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("create Kubernetes client: %v", err)
	}
	environment := &Environment{Namespace: "test-ns"}
	spoke := Cluster{
		Name: "spoke-1",
		Clients: &clients.Clients{
			KubeClient: &clients.KubeClient{Kube: kube},
		},
	}
	logs, err := environment.pipelineRunLogs(context.Background(), spoke, "test-run")
	if err != nil {
		t.Fatalf("pipelineRunLogs() error = %v", err)
	}
	if want := workloadLogMarker + "\n"; logs != want {
		t.Fatalf("pipelineRunLogs() = %q, want %q", logs, want)
	}
}
