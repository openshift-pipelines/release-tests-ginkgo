package pac

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-github/v74/github"
	pacv1alpha1 "github.com/openshift-pipelines/pipelines-as-code/pkg/apis/pipelinesascode/v1alpha1"
	pacfake "github.com/openshift-pipelines/pipelines-as-code/pkg/generated/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

func githubTestClient(t *testing.T, handler http.Handler) *github.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	baseURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	client := github.NewClient(server.Client())
	client.BaseURL = baseURL
	client.UploadURL = baseURL
	return client
}

func kubeTestClient(t *testing.T, secretExists bool) *kubernetes.Clientset {
	t.Helper()
	var mu sync.Mutex
	var secret *corev1.Secret
	if secretExists {
		secret = &corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: githubWebhookConfigName, Namespace: "test", ResourceVersion: "1"},
			Data:       map[string][]byte{"provider.token": []byte("original")},
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		secretPath := "/api/v1/namespaces/test/secrets/" + githubWebhookConfigName
		switch {
		case r.Method == http.MethodGet && r.URL.Path == secretPath:
			if secret == nil {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`))
				return
			}
			_ = json.NewEncoder(w).Encode(secret)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/namespaces/test/secrets":
			next := &corev1.Secret{}
			if err := json.NewDecoder(r.Body).Decode(next); err != nil {
				t.Errorf("decode Secret create request: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			next.ResourceVersion = "1"
			secret = next
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(secret)
		case r.Method == http.MethodPut && r.URL.Path == secretPath:
			next := &corev1.Secret{}
			if err := json.NewDecoder(r.Body).Decode(next); err != nil {
				t.Errorf("decode Secret update request: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			next.ResourceVersion = "2"
			secret = next
			_ = json.NewEncoder(w).Encode(secret)
		case r.Method == http.MethodDelete && r.URL.Path == secretPath:
			secret = nil
			_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Success","code":200}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/apis/apps/v1/namespaces/test/deployments/gosmee-client":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`))
		default:
			http.Error(w, "unexpected Kubernetes request: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client, err := kubernetes.NewForConfig(&rest.Config{
		Host: server.URL,
		ContentConfig: rest.ContentConfig{
			ContentType:        "application/json",
			AcceptContentTypes: "application/json",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestSetupGitHubProjectConfiguresGeneratedPipelineRunName(t *testing.T) {
	const (
		namespace = "test"
		owner     = "test-owner"
	)
	var repoName string
	client := githubTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/user/repos":
			var request struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode create request: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			repoName = request.Name
			_, _ = fmt.Fprintf(w, `{"name":%q,"html_url":"https://github.com/%s/%s","default_branch":"main","owner":{"login":%q}}`, repoName, owner, repoName, owner)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/"+owner+"/"):
			_, _ = fmt.Fprintf(w, `{"name":%q,"default_branch":"main","owner":{"login":%q}}`, repoName, owner)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/hooks"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			http.Error(w, "unexpected request: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	SetGitHubClient(client)
	t.Cleanup(func() { SetGitHubClient(nil) })
	t.Setenv("PAC_GITHUB_TOKEN", "token")
	t.Setenv("PAC_GITHUB_WEBHOOK_TOKEN", "webhook")
	t.Setenv("PAC_GITHUB_ORG", "")
	oldProjectURL := projectURL
	projectURL = ""
	t.Cleanup(func() { projectURL = oldProjectURL })

	cs := &clients.Clients{
		KubeClient:   &clients.KubeClient{Kube: kubeTestClient(t, false)},
		PacClientset: pacfake.NewSimpleClientset().PipelinesascodeV1alpha1(),
	}
	if _, _, err := SetupGitHubProject(cs, namespace, "https://smee.example/test"); err != nil {
		t.Fatal(err)
	}
	fileName := pullRequestFile()
	t.Cleanup(func() { _ = os.Remove(fileName) })
	if err := GeneratePipelineRunYaml("pull_request", "main"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "name: "+repoName+"-pull-request") {
		t.Fatal("generated PipelineRun name does not use the GitHub repository name")
	}
}

func TestSetupGitHubProjectRollsBackPartialSetup(t *testing.T) {
	const (
		namespace = "test"
		owner     = "test-owner"
	)
	var repoName string
	deleteCalls := 0
	client := githubTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/user/repos":
			var request struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode create request: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			repoName = request.Name
			_, _ = fmt.Fprintf(w, `{"name":%q,"html_url":"https://github.com/%s/%s","default_branch":"main","owner":{"login":%q}}`, repoName, owner, repoName, owner)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/"+owner+"/"):
			_, _ = fmt.Fprintf(w, `{"name":%q,"default_branch":"main","owner":{"login":%q}}`, repoName, owner)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/hooks"):
			http.Error(w, `{"message":"hook failed"}`, http.StatusInternalServerError)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/repos/"+owner+"/"):
			deleteCalls++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected request: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	SetGitHubClient(client)
	t.Cleanup(func() { SetGitHubClient(nil) })
	t.Setenv("PAC_GITHUB_TOKEN", "token")
	t.Setenv("PAC_GITHUB_WEBHOOK_TOKEN", "webhook")
	t.Setenv("PAC_GITHUB_ORG", "")

	cs := &clients.Clients{
		KubeClient:   &clients.KubeClient{Kube: kubeTestClient(t, false)},
		PacClientset: pacfake.NewSimpleClientset().PipelinesascodeV1alpha1(),
	}
	if _, _, err := SetupGitHubProject(cs, namespace, "https://smee.example/test"); err == nil {
		t.Fatal("expected webhook creation to fail")
	}
	if deleteCalls != 1 {
		t.Fatalf("expected one GitHub repository rollback, got %d", deleteCalls)
	}
	if _, err := cs.KubeClient.Kube.CoreV1().Secrets(namespace).Get(context.Background(), githubWebhookConfigName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected webhook secret to be removed, got %v", err)
	}
	if _, err := cs.PacClientset.Repositories(namespace).Get(context.Background(), sanitizeGitHubK8sName(repoName), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected PAC Repository to be removed, got %v", err)
	}
}

func TestSetupGitHubProjectPreservesUntouchedSecret(t *testing.T) {
	const (
		namespace = "test"
		owner     = "test-owner"
	)
	client := githubTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/user/repos":
			_, _ = fmt.Fprintf(w, `{"name":"test-repo","default_branch":"main","owner":{"login":%q}}`, owner)
		case r.Method == http.MethodGet:
			http.Error(w, `{"message":"not ready"}`, http.StatusInternalServerError)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	SetGitHubClient(client)
	t.Cleanup(func() { SetGitHubClient(nil) })
	t.Setenv("PAC_GITHUB_TOKEN", "token")
	t.Setenv("PAC_GITHUB_WEBHOOK_TOKEN", "webhook")
	t.Setenv("PAC_GITHUB_ORG", "")

	cs := &clients.Clients{
		KubeClient:   &clients.KubeClient{Kube: kubeTestClient(t, true)},
		PacClientset: pacfake.NewSimpleClientset().PipelinesascodeV1alpha1(),
	}
	if _, _, err := SetupGitHubProject(cs, namespace, "https://smee.example/test"); err == nil {
		t.Fatal("expected repository readiness to fail")
	}
	if _, err := cs.KubeClient.Kube.CoreV1().Secrets(namespace).Get(context.Background(), githubWebhookConfigName, metav1.GetOptions{}); err != nil {
		t.Fatalf("expected untouched webhook secret to remain, got %v", err)
	}
}

func TestCleanupPACGitHubContinuesAfterRepositoryDeleteFailure(t *testing.T) {
	const (
		namespace = "test"
		repoName  = "test-repo"
	)
	client := githubTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"delete failed"}`, http.StatusInternalServerError)
	}))
	SetGitHubClient(client)
	t.Cleanup(func() { SetGitHubClient(nil) })

	repository := &pacv1alpha1.Repository{ObjectMeta: metav1.ObjectMeta{Name: sanitizeGitHubK8sName(repoName), Namespace: namespace}}
	cs := &clients.Clients{
		KubeClient:   &clients.KubeClient{Kube: kubeTestClient(t, true)},
		PacClientset: pacfake.NewSimpleClientset(repository).PipelinesascodeV1alpha1(),
	}

	err := CleanupPACGitHub(cs, namespace, "gosmee-client", "test-owner", repoName)
	if err == nil || !strings.Contains(err.Error(), "delete github repository") {
		t.Fatalf("expected GitHub deletion error, got %v", err)
	}
	if _, err := cs.KubeClient.Kube.CoreV1().Secrets(namespace).Get(context.Background(), githubWebhookConfigName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected webhook secret to be removed, got %v", err)
	}
	if _, err := cs.PacClientset.Repositories(namespace).Get(context.Background(), sanitizeGitHubK8sName(repoName), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected PAC Repository to be removed, got %v", err)
	}
}
