package tektonkueue

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var wireRun = proxyRun{namespace: "test-ns", name: "test-run", worker: "test-spoke-1", pod: "worker-pod",
	workload: "workload", task: "task", runUID: "worker-uid", taskUID: "task-uid", podUID: "pod-uid"}

const wireBearer = "sha256~an-explicit-oauth-token"

func proxyWire(t *testing.T, handler http.HandlerFunc) *consoleProxy {
	t.Helper()
	// Model the console boundary, not the proxy service: cookie authentication
	// precedes both HTTP and WebSocket routing. UserToken forwarding replaces
	// backend Authorization from the authenticated session, never an incoming bearer.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("openshift-session-token")
		if err != nil || cookie.Value != "opaque-session" {
			http.Error(w, "session required", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("client sent bearer auth instead of console session")
		}
		r.Header.Set("Authorization", "Bearer "+wireBearer)
		if r.URL.Path == "/api/kubernetes/apis/user.openshift.io/v1/users/~" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"kind":"User","metadata":{"name":"console-user"}}`))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Timeout = 5 * time.Second
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(origin, []*http.Cookie{{Name: "openshift-session-token", Value: "opaque-session", Path: "/", Secure: true}})
	client.Jar = jar
	return &consoleProxy{base: server.URL + consoleProxyBase, user: "console-user", expires: time.Now().Add(time.Minute), client: client}
}

func proxyJSON(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()
	if r.Header.Get("Authorization") != "Bearer "+wireBearer {
		t.Error("missing explicit bearer authorization")
	}
	if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, consoleProxyBase+"/api/v1/namespaces/test-ns/") {
		t.Errorf("wrong proxy route: %s %s", r.Method, r.URL.Path)
	}
	w.Header().Set("X-Worker-Cluster", wireRun.worker)
	w.Header().Set("Content-Type", "application/json")
	var value any
	switch r.URL.Path {
	case consoleProxyBase + wireRun.path("resolve"):
		value = map[string]string{"name": wireRun.worker, "state": "Admitted", "workloadName": "workload"}
	case consoleProxyBase + wireRun.path("taskruns"):
		value = pipelinev1.TaskRunList{Items: []pipelinev1.TaskRun{{
			ObjectMeta: metav1.ObjectMeta{Name: "task", Namespace: wireRun.namespace, UID: wireRun.taskUID,
				OwnerReferences: []metav1.OwnerReference{{Kind: "PipelineRun", Name: wireRun.name, UID: wireRun.runUID}},
				Labels:          map[string]string{"tekton.dev/pipelineRun": wireRun.name}},
			Status: pipelinev1.TaskRunStatus{TaskRunStatusFields: pipelinev1.TaskRunStatusFields{PodName: wireRun.pod}},
		}}}
	case consoleProxyBase + wireRun.path("pods"):
		value = corev1.PodList{Items: []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: wireRun.pod, Namespace: wireRun.namespace, UID: wireRun.podUID,
				OwnerReferences: []metav1.OwnerReference{{Kind: "TaskRun", Name: wireRun.task, UID: wireRun.taskUID}},
				Labels:          map[string]string{"tekton.dev/pipelineRun": wireRun.name}},
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
				Name: workloadLogContainer, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}}},
		}}}
	default:
		return false
	}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
	return true
}

func TestLiveLogsUsesOneAuthenticatedSocketForPostSubscriptionChallenge(t *testing.T) {
	// Given a real TLS proxy which splits each marker over separate WS messages.
	challenge := make(chan string, 1)
	closed := make(chan struct{})
	proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) {
		if proxyJSON(t, w, r) {
			return
		}
		if r.URL.Path != consoleProxyBase+"/api/v1/namespaces/test-ns/logs/stream" ||
			r.URL.Query().Get("pipelineRun") != wireRun.name || r.URL.Query().Get("pod") != wireRun.pod ||
			r.URL.Query().Get("container") != workloadLogContainer {
			t.Errorf("wrong log endpoint or query: %s", r.URL)
			http.NotFound(w, r)
			return
		}
		conn, err := (&websocket.Upgrader{Subprotocols: []string{"binary.k8s.io"}, WriteBufferSize: 8}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		defer close(closed)
		if err := conn.WriteMessage(websocket.TextMessage, []byte(workloadLogMarker[:10])); err != nil {
			t.Error(err)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(workloadLogMarker[10:]+"\n")); err != nil {
			t.Error(err)
			return
		}
		select {
		case marker := <-challenge:
			writer, err := conn.NextWriter(websocket.TextMessage)
			if err != nil {
				t.Error(err)
				return
			}
			for _, part := range []string{marker[:5], marker[5:] + "\n"} {
				if _, err := writer.Write([]byte(part)); err != nil {
					t.Error(err)
					return
				}
			}
			if err := writer.Close(); err != nil {
				t.Error(err)
				return
			}
		case <-r.Context().Done():
			return
		}
		_, _, _ = conn.ReadMessage() // client closes after consuming its challenge
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var generated string
	// When gate 1 is released only after the first complete marker is received.
	logs, err := proxy.liveLogs(ctx, wireRun, func(_ context.Context, marker string) error {
		generated = marker
		challenge <- marker
		return nil
	})
	// Then acceptance bytes came from that socket, including its new random challenge.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs, workloadLogMarker+"\n") || !strings.Contains(logs, generated+"\n") || !strings.HasPrefix(generated, "live-") {
		t.Fatalf("missing live challenge in console log stream: %q", logs)
	}
	receiveReadinessEvent(t, ctx, closed)
}

func TestProxyRejectsWrongWorkerUnrelatedResourcesAndLoginResponses(t *testing.T) {
	for _, scenario := range []string{"wrong worker", "unrelated pod", "unrelated task", "login", "html", "invalid json", "trailing json", "unauthorized"} {
		t.Run(scenario, func(t *testing.T) {
			// Given invalid wire data from a console endpoint.
			proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) {
				switch scenario {
				case "login":
					http.Redirect(w, r, "/auth/login", http.StatusFound)
				case "html":
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte("<html>login</html>"))
				case "invalid json":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("<html>login</html>"))
				case "trailing json":
					proxyJSON(t, w, r)
					_, _ = w.Write([]byte("{}"))
				case "unauthorized":
					w.WriteHeader(http.StatusForbidden)
				case "wrong worker":
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("X-Worker-Cluster", "wrong")
					_, _ = w.Write([]byte(`{"name":"worker-one","state":"Admitted","workloadName":"workload"}`))
				default:
					if strings.HasSuffix(r.URL.Path, "/pods") && scenario == "unrelated pod" {
						w.Header().Set("Content-Type", "application/json")
						w.Header().Set("X-Worker-Cluster", wireRun.worker)
						_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"other"}}]}`))
					} else if strings.HasSuffix(r.URL.Path, "/taskruns") && scenario == "unrelated task" {
						w.Header().Set("Content-Type", "application/json")
						w.Header().Set("X-Worker-Cluster", wireRun.worker)
						_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"other"}}]}`))
					} else {
						proxyJSON(t, w, r)
					}
				}
			})
			// When /resolve, /taskruns and /pods are validated.
			_, err := proxy.pod(t.Context(), wireRun)
			// Then no invalid response can reach log acceptance.
			if err == nil {
				t.Fatal("accepted invalid proxy response")
			}
		})
	}
}

func TestProxyQueryEncoding(t *testing.T) {
	// Given characters that must not become path segments or query delimiters.
	run := proxyRun{namespace: "ns space", name: "run/+?&"}
	// When
	endpoint, err := url.Parse("https://console.example" + consoleProxyBase + run.logPath("pod +&?"))
	// Then each query value round-trips without creating extra parameters.
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.Query().Get("pipelineRun") != run.name || endpoint.Query().Get("pod") != "pod +&?" || len(endpoint.Query()) != 3 {
		t.Fatalf("incorrect query encoding: %s", endpoint)
	}
	if run.path("pods") != "/api/v1/namespaces/ns%20space/pipelineruns/run%2F+%3F&/pods" {
		t.Fatalf("incorrect path escaping: %s", run.path("pods"))
	}
}

func TestProxyRequiresTrustedTLS(t *testing.T) {
	// Given a self-signed console not present in the runner's trust store.
	proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) })
	proxy.client.Transport = http.DefaultTransport.(*http.Transport).Clone()
	// When
	_, err := proxy.pod(t.Context(), wireRun)
	// Then certificate validation fails instead of falling back to insecure TLS.
	if err == nil {
		t.Fatalf("untrusted TLS = %v", err)
	}
}

func TestLiveLogsEarlyCloseAndCancellation(t *testing.T) {
	for _, cancelStream := range []bool{false, true} {
		t.Run(map[bool]string{false: "early close", true: "cancel"}[cancelStream], func(t *testing.T) {
			// Given a socket that cannot deliver its ready marker.
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) {
				if proxyJSON(t, w, r) {
					return
				}
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.Close() }()
				if cancelStream {
					cancel()
					_, _, _ = conn.ReadMessage()
				}
			})
			// When
			_, err := proxy.liveLogs(ctx, wireRun, func(context.Context, string) error {
				t.Error("released gate 1 without a ready marker")
				return nil
			})
			// Then the read terminates and cancellation retains its identity.
			if err == nil || (cancelStream && !errors.Is(err, context.Canceled)) {
				t.Fatalf("stream termination = %v", err)
			}
		})
	}
}
