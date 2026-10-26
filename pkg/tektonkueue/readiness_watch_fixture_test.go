package tektonkueue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

type stoppedReadinessWatch struct {
	watch.Interface
	once    sync.Once
	stopped chan struct{}
}

func (w *stoppedReadinessWatch) Stop() {
	w.once.Do(func() {
		w.Interface.Stop()
		close(w.stopped)
	})
}

type readinessWatchEvent struct {
	name   string
	stream *stoppedReadinessWatch
}

type readinessWatchFixture struct {
	environment *Environment
	clients     map[string]*dynamicfake.FakeDynamicClient
	subscribed  chan readinessWatchEvent
	probed      chan struct{}
}

func watchEnvironment(t *testing.T, mutate func(string, *readinessFixture)) *readinessWatchFixture {
	t.Helper()
	f := &readinessWatchFixture{
		clients:    make(map[string]*dynamicfake.FakeDynamicClient),
		subscribed: make(chan readinessWatchEvent, 64), probed: make(chan struct{}, 1),
	}
	clusters := make([]Cluster, 0, 2)
	for _, name := range []string{"hub", "spoke-1"} {
		fixture := readyClusterFixture(t, name)
		fixture.pods[0].Namespace = "openshift-pipelines"
		fixture.pods[0].Labels = map[string]string{"app": "multicluster-proxy-aae"}
		fixture.pods[0].Status = corev1.PodStatus{
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "proxy", Ready: true}},
		}
		if mutate != nil {
			mutate(name, fixture)
		}
		pod, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&fixture.pods[0])
		if err != nil {
			t.Fatal(err)
		}
		pod["apiVersion"], pod["kind"] = "v1", "Pod"
		objects := make([]runtime.Object, 1, 1+len(fixture.objects))
		objects[0] = &unstructured.Unstructured{Object: pod}
		listKinds := map[schema.GroupVersionResource]string{podGVR: "PodList"}
		for path, object := range fixture.objects {
			parts := strings.Split(path, "/")
			resource := schema.GroupVersionResource{Group: parts[2], Version: parts[3], Resource: parts[4]}
			if parts[4] == "namespaces" {
				object.SetNamespace(parts[5])
				resource.Resource = parts[6]
			}
			listKinds[resource] = object.GetKind() + "List"
			objects = append(objects, object)
		}
		client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
		client.PrependReactor("list", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
			listAction := action.(k8stesting.ListActionImpl)
			list, err := client.Tracker().List(action.GetResource(), listAction.GetKind(), action.GetNamespace())
			if err != nil {
				return true, nil, err
			}
			metadata, err := meta.ListAccessor(list)
			if err != nil {
				return true, nil, err
			}
			metadata.SetResourceVersion("42")
			return true, list, nil
		})
		client.PrependWatchReactor("*", func(action k8stesting.Action) (bool, watch.Interface, error) {
			if rv := action.(k8stesting.WatchAction).GetWatchRestrictions().ResourceVersion; rv != "42" {
				return true, nil, fmt.Errorf("watch resourceVersion=%q, want list snapshot 42", rv)
			}
			stream, err := client.Tracker().Watch(action.GetResource(), action.GetNamespace())
			if err != nil {
				return true, nil, err
			}
			wrapped := &stoppedReadinessWatch{Interface: stream, stopped: make(chan struct{})}
			f.subscribed <- readinessWatchEvent{name: name + "/" + action.GetResource().Resource, stream: wrapped}
			return true, wrapped, nil
		})
		f.clients[name] = client
		clusters = append(clusters, Cluster{Name: name, Clients: &clients.Clients{
			Dynamic: client, KubeConfig: &rest.Config{Host: "https://" + name + ".example.test"},
		}})
	}
	f.environment = NewEnvironment(clusters[0], clusters[1:], readinessPrefix)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/proxy/ready"):
			worker, err := f.clients["hub"].Resource(multiKueueClusterGVR).Get(r.Context(), readinessPrefix+"-spoke-1", metav1.GetOptions{})
			if err == nil {
				err = resourceReady(worker, readinessResource{gvr: multiKueueClusterGVR, condition: "Active"})
			}
			if err != nil {
				t.Errorf("HTTP /ready requested before worker readiness: %v", err)
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			select {
			case f.probed <- struct{}{}:
			default:
				t.Error("unexpected repeated HTTP /ready request")
			}
		case strings.HasSuffix(r.URL.Path, "/pods"):
			pods, err := f.clients["hub"].Resource(podGVR).Namespace("openshift-pipelines").List(r.Context(),
				metav1.ListOptions{LabelSelector: r.URL.Query().Get("labelSelector")})
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if err := json.NewEncoder(w).Encode(pods); err != nil {
				t.Error(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	kube, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL, QPS: 100, Burst: 200})
	if err != nil {
		t.Fatal(err)
	}
	f.environment.Hub.Clients.KubeClient = &clients.KubeClient{Kube: kube}
	return f
}

func startReadinessWait(t *testing.T, environment *Environment) (context.Context, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	done := make(chan error, 1)
	go func() {
		done <- environment.WaitReady(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		select {
		case <-done:
		case <-cleanupCtx.Done():
			t.Error("readiness wait did not stop after cancellation")
		}
	})
	return ctx, cancel, done
}

func receiveReadinessEvent[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatalf("waiting for readiness handshake: %v", ctx.Err())
		var zero T
		return zero
	}
}

func subscribedReadinessWatches(t *testing.T, ctx context.Context, f *readinessWatchFixture) []*stoppedReadinessWatch {
	t.Helper()
	var streams []*stoppedReadinessWatch
	for {
		event := receiveReadinessEvent(t, ctx, f.subscribed)
		streams = append(streams, event.stream)
		if event.name == "spoke-1/pods" {
			return streams
		}
	}
}
