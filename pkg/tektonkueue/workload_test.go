package tektonkueue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

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
