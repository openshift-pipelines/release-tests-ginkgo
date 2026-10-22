package tektonkueue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	pipelinefake "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/streaming/pkg/httpstream"
	"k8s.io/streaming/pkg/httpstream/spdy"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

type executionFixture struct {
	environment   *Environment
	run           *pipelinev1.PipelineRun
	dynamic       map[string]*dynamicfake.FakeDynamicClient
	tekton        map[string]*pipelinefake.Clientset
	subscriptions atomic.Int32
	onGate        func(string, string) error
	start         func()
	holdCandidate bool
}

func executionEnvironment(t *testing.T) *executionFixture {
	t.Helper()
	f := &executionFixture{dynamic: map[string]*dynamicfake.FakeDynamicClient{}, tekton: map[string]*pipelinefake.Clientset{}}
	run := &pipelinev1.PipelineRun{
		TypeMeta: metav1.TypeMeta{APIVersion: "tekton.dev/v1", Kind: "PipelineRun"},
		ObjectMeta: metav1.ObjectMeta{Name: wireRun.name, Namespace: wireRun.namespace, UID: "hub-uid",
			Labels: map[string]string{"app.kubernetes.io/managed-by": "release-tests-ginkgo", "app.kubernetes.io/instance": "test"}},
	}
	f.run = run
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.exec(t, w, r) }))
	t.Cleanup(server.Close)
	clusters := make([]Cluster, 0, 3)
	for _, name := range []string{"hub", "spoke-1", "spoke-2"} {
		dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
			pipelineRunGVR: "PipelineRunList", taskRunGVR: "TaskRunList", workloadGVR: "WorkloadList", podGVR: "PodList", secretGVR: "SecretList",
		})
		dynamic.PrependWatchReactor("*", func(action k8stesting.Action) (bool, watch.Interface, error) {
			stream, err := dynamic.Tracker().Watch(action.GetResource(), action.GetNamespace())
			if err == nil {
				f.subscriptions.Add(1)
			}
			return true, stream, err
		})
		tekton := pipelinefake.NewSimpleClientset()
		cfg := &rest.Config{Host: server.URL, BearerToken: name}
		kube, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		clusters = append(clusters, Cluster{Name: name, Clients: &clients.Clients{
			Dynamic: dynamic, Tekton: tekton, PipelineRunClient: tekton.TektonV1().PipelineRuns(wireRun.namespace),
			KubeConfig: cfg, KubeClient: &clients.KubeClient{Kube: kube},
		}})
		f.dynamic[name], f.tekton[name] = dynamic, tekton
	}
	f.environment = NewEnvironment(clusters[0], clusters[1:], "test")
	f.environment.Namespace = wireRun.namespace
	f.start = func() {
		for _, name := range []string{"hub", "spoke-1"} {
			copy := run.DeepCopy()
			if name != "hub" {
				copy.UID = "worker-uid"
			}
			if _, err := f.tekton[name].TektonV1().PipelineRuns(run.Namespace).Create(t.Context(), copy, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(copy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.dynamic[name].Resource(pipelineRunGVR).Namespace(run.Namespace).
				Create(t.Context(), &unstructured.Unstructured{Object: object}, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		workload := object("Workload", "workload", run.Namespace, nil)
		workload.SetUID("workload-uid")
		workload.SetOwnerReferences([]metav1.OwnerReference{{Kind: "PipelineRun", Name: run.Name, UID: run.UID}})
		if _, err := f.dynamic["hub"].Resource(workloadGVR).Namespace(run.Namespace).Create(t.Context(), workload, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"spoke-1", "spoke-2"} {
			copy := workload.DeepCopy()
			copy.SetUID(types.UID(name + "-workload-uid"))
			copy.SetOwnerReferences(nil)
			if _, err := f.dynamic[name].Resource(workloadGVR).Namespace(run.Namespace).Create(t.Context(), copy, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		for _, gvr := range []schema.GroupVersionResource{taskRunGVR, podGVR} {
			kind, name := "TaskRun", "task"
			if gvr == podGVR {
				kind, name = "Pod", wireRun.pod
			}
			worker := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": gvr.GroupVersion().String(), "kind": kind,
				"metadata": map[string]any{"name": name, "namespace": run.Namespace, "labels": map[string]any{"tekton.dev/pipelineRun": run.Name}},
			}}
			if gvr == taskRunGVR {
				worker.SetUID(wireRun.taskUID)
				worker.SetOwnerReferences([]metav1.OwnerReference{{Kind: "PipelineRun", Name: run.Name, UID: wireRun.runUID}})
				worker.Object["status"] = map[string]any{"podName": wireRun.pod}
			}
			if gvr == podGVR {
				worker.SetUID(wireRun.podUID)
				worker.SetOwnerReferences([]metav1.OwnerReference{{Kind: "TaskRun", Name: wireRun.task, UID: wireRun.taskUID}})
				worker.Object["status"] = map[string]any{"containerStatuses": []any{map[string]any{"name": workloadLogContainer, "state": map[string]any{"running": map[string]any{}}}}}
			}
			if _, err := f.dynamic["spoke-1"].Resource(gvr).Namespace(run.Namespace).Create(t.Context(), worker, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return f
}

func (f *executionFixture) exec(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/test-ns/secrets/") {
		cluster := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		client := f.dynamic[cluster]
		if client == nil {
			http.Error(w, "unknown cluster", http.StatusForbidden)
			return
		}
		secret, err := client.Resource(secretGVR).Namespace(wireRun.namespace).Get(r.Context(), strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/test-ns/secrets/"), metav1.GetOptions{})
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(secret); err != nil {
			t.Error(err)
		}
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/v1/namespaces/test-ns/pods/worker-pod" {
		pod, err := f.dynamic["spoke-1"].Resource(podGVR).Namespace(wireRun.namespace).Get(r.Context(), wireRun.pod, metav1.GetOptions{})
		if err != nil {
			http.Error(w, "missing producer", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(pod); err != nil {
			t.Error(err)
		}
		return
	}
	command := r.URL.Query()["command"]
	if r.Method != "POST" || r.URL.Path != "/api/v1/namespaces/test-ns/pods/worker-pod/exec" ||
		r.URL.Query().Get("container") != workloadLogContainer || len(command) != 6 ||
		!reflect.DeepEqual(command[:4], []string{"sh", "-c", `printf '%s\n' "$2" > "/tmp/rtg-$1"`, "release"}) {
		t.Errorf("incorrect exec request: %s %s", r.Method, r.URL)
		http.Error(w, "incorrect exec", http.StatusBadRequest)
		return
	}
	if _, err := httpstream.Handshake(r, w, []string{"v4.channel.k8s.io"}); err != nil {
		t.Error(err)
		return
	}
	type pendingStream struct {
		stream  httpstream.Stream
		replied <-chan struct{}
	}
	streams := make(chan pendingStream, 2)
	conn := spdy.NewResponseUpgrader().UpgradeResponse(w, r, func(stream httpstream.Stream, replied <-chan struct{}) error {
		streams <- pendingStream{stream, replied}
		return nil
	})
	if conn == nil {
		return
	}
	defer func() { _ = conn.Close() }()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var errorStream httpstream.Stream
	var stderr httpstream.Stream
	for range 2 {
		select {
		case pending := <-streams:
			select {
			case <-pending.replied:
			case <-ctx.Done():
				t.Error(ctx.Err())
				return
			}
			if pending.stream.Headers().Get(corev1.StreamType) == corev1.StreamTypeError {
				errorStream = pending.stream
			} else {
				stderr = pending.stream
			}
		case <-ctx.Done():
			t.Error(ctx.Err())
			return
		}
	}
	status := metav1.Status{Status: metav1.StatusSuccess}
	if err := f.onGate(command[4], command[5]); err != nil {
		status.Status, status.Message = metav1.StatusFailure, err.Error()
	}
	if err := stderr.Close(); err != nil {
		t.Error(err)
	}
	if err := json.NewEncoder(errorStream).Encode(status); err != nil {
		t.Error(err)
	}
	if err := errorStream.Close(); err != nil {
		t.Error(err)
	}
	select {
	case <-conn.CloseChan():
	case <-ctx.Done():
		t.Error(ctx.Err())
	}
}

func (f *executionFixture) complete(ctx context.Context) error {
	for _, name := range []string{"spoke-1", "hub"} {
		resource := f.dynamic[name].Resource(pipelineRunGVR).Namespace(wireRun.namespace)
		run, err := resource.Get(ctx, wireRun.name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		run.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": "True"}}}
		if _, err := resource.Update(ctx, run, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	resource := f.dynamic["hub"].Resource(workloadGVR).Namespace(wireRun.namespace)
	workload, err := resource.Get(ctx, "workload", metav1.GetOptions{})
	if err != nil {
		return err
	}
	workload.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Finished", "status": "True"}}}
	if _, err := resource.Update(ctx, workload, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return f.workerGC(ctx)
}

func (f *executionFixture) workerGC(ctx context.Context) error {
	for gvr, name := range map[schema.GroupVersionResource]string{pipelineRunGVR: wireRun.name, taskRunGVR: "task", podGVR: wireRun.pod} {
		if err := f.dynamic["spoke-1"].Resource(gvr).Namespace(wireRun.namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("worker GC: %w", err)
		}
	}
	for _, name := range []string{"spoke-1", "spoke-2"} {
		if name == "spoke-2" && f.holdCandidate {
			continue
		}
		if err := f.dynamic[name].Resource(workloadGVR).Namespace(wireRun.namespace).Delete(ctx, wireRun.workload, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	return nil
}
