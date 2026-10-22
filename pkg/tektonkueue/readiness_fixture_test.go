package tektonkueue

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	operatorv1alpha1 "github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

const readinessPrefix = "rtg-ready"

type readinessFixture struct {
	objects     map[string]*unstructured.Unstructured
	proxyStatus int
	pods        []corev1.Pod
}

func readyEnvironment(t *testing.T, mutate func(string, *readinessFixture)) *Environment {
	t.Helper()
	cluster := func(name string) Cluster {
		fixture := readyClusterFixture(t, name)
		if mutate != nil {
			mutate(name, fixture)
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			var result any
			switch {
			case strings.HasSuffix(r.URL.Path, "/proxy/ready"):
				if r.URL.Path != "/api/v1/namespaces/openshift-pipelines/pods/http:proxy-pod:8080/proxy/ready" {
					t.Errorf("unexpected proxy probe path: %s", r.URL.Path)
				}
				w.WriteHeader(fixture.proxyStatus)
				return
			case strings.HasSuffix(r.URL.Path, "/pods"):
				if selector := r.URL.Query().Get("labelSelector"); selector != "app=multicluster-proxy-aae" {
					t.Errorf("proxy pod selector = %q, want Deployment selector", selector)
				}
				result = corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}, Items: fixture.pods}
			case strings.HasSuffix(r.URL.Path, "/deployments"):
				selector, err := labels.Parse(r.URL.Query().Get("labelSelector"))
				if err != nil {
					t.Error(err)
					http.Error(w, "invalid selector", http.StatusBadRequest)
					return
				}
				list := unstructured.UnstructuredList{Object: map[string]any{"apiVersion": "apps/v1", "kind": "DeploymentList"}}
				for path, object := range fixture.objects {
					if strings.HasPrefix(path, r.URL.Path+"/") && selector.Matches(labels.Set(object.GetLabels())) {
						list.Items = append(list.Items, *object)
					}
				}
				result = &list
			default:
				object, found := fixture.objects[r.URL.Path]
				if !found {
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				result = object
			}
			if err := json.NewEncoder(w).Encode(result); err != nil {
				t.Error(err)
			}
		}))
		t.Cleanup(server.Close)
		cfg := &rest.Config{Host: server.URL, QPS: 100, Burst: 200}
		kube, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		dynamicClient, err := dynamic.NewForConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return Cluster{Name: name, Clients: &clients.Clients{
			KubeConfig: cfg, KubeClient: &clients.KubeClient{Kube: kube}, Dynamic: dynamicClient,
		}}
	}
	return NewEnvironment(cluster("hub"), []Cluster{cluster("spoke-1")}, readinessPrefix)
}

func readyClusterFixture(t *testing.T, name string) *readinessFixture {
	t.Helper()
	fixture := &readinessFixture{objects: map[string]*unstructured.Unstructured{}, proxyStatus: http.StatusOK}
	add := func(path, kind, condition string) *unstructured.Unstructured {
		object := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": strings.Join(strings.Split(path, "/")[2:4], "/"), "kind": kind,
			"metadata": map[string]any{"name": path[strings.LastIndex(path, "/")+1:], "generation": int64(2)},
			"spec":     map[string]any{"targetNamespace": "openshift-pipelines"},
		}}
		if condition != "" {
			object.Object["status"] = readyResourceStatus(t, kind, condition)
		}
		fixture.objects[path] = object
		return object
	}
	operatorPath := "/apis/operator.tekton.dev/v1alpha1/"
	kueuePath := "/apis/kueue.x-k8s.io/v1beta2/"
	add("/apis/kueue.openshift.io/v1/kueues/cluster", "Kueue", "Available")
	tc := add(operatorPath+"tektonconfigs/config", "TektonConfig", "Ready")
	role := operatorv1alpha1.MultiClusterRoleSpoke
	if name == "hub" {
		role = operatorv1alpha1.MultiClusterRoleHub
	}
	disabled := false
	scheduler := operatorv1alpha1.Scheduler{Disabled: &disabled,
		MultiClusterConfig: operatorv1alpha1.MultiClusterConfig{MultiClusterRole: role}}
	scheduler.QueueName = readinessPrefix
	scheduler.MultiKueueOverride = role == operatorv1alpha1.MultiClusterRoleHub
	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&scheduler)
	if err != nil {
		t.Fatal(err)
	}
	tc.Object["spec"].(map[string]any)["scheduler"] = value
	add(kueuePath+"clusterqueues/"+readinessPrefix, "ClusterQueue", "Active")
	add(kueuePath+"namespaces/"+readinessPrefix+"/localqueues/"+readinessPrefix, "LocalQueue", "Active")
	add(kueuePath+"resourceflavors/"+readinessPrefix, "ResourceFlavor", "")
	components := []struct{ resource, kind, name string }{
		{"tektonschedulers", "TektonScheduler", "scheduler"},
	}
	if name == "hub" {
		components = append(components,
			struct{ resource, kind, name string }{"tektonmulticlusterproxyaaes", "TektonMulticlusterProxyAAE", "multicluster-proxy-aae"},
			struct{ resource, kind, name string }{"syncerservices", "SyncerService", "syncer-service"})
		add(kueuePath+"admissionchecks/"+readinessPrefix, "AdmissionCheck", "Active")
		add(kueuePath+"multikueueconfigs/"+readinessPrefix, "MultiKueueConfig", "")
		add(kueuePath+"multikueueclusters/"+readinessPrefix+"-spoke-1", "MultiKueueCluster", "Active")
	}
	for _, component := range components {
		componentObject := add(operatorPath+component.resource+"/"+component.name, component.kind, "Ready")
		if component.kind == "TektonScheduler" {
			componentSpec := runtime.DeepCopyJSON(value)
			componentSpec["targetNamespace"] = "openshift-pipelines"
			componentObject.Object["spec"] = componentSpec
		}
		deployment := &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
			ObjectMeta: metav1.ObjectMeta{Name: component.name, Namespace: "openshift-pipelines", Generation: 2,
				Labels: map[string]string{operatorv1alpha1.LabelOperandName: component.name}},
			Spec: appsv1.DeploymentSpec{Replicas: ptr(int32(1)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": component.name}}},
			Status: appsv1.DeploymentStatus{ObservedGeneration: 2, UpdatedReplicas: 1, AvailableReplicas: 1,
				Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}}},
		}
		value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
		if err != nil {
			t.Fatal(err)
		}
		fixture.objects["/apis/apps/v1/namespaces/openshift-pipelines/deployments/"+component.name] = &unstructured.Unstructured{Object: value}
	}
	fixture.pods = []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "proxy-pod"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "proxy",
			ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/ready", Port: intstr.FromInt32(8080)},
			}},
		}}},
	}}
	return fixture
}

func readyResourceStatus(t *testing.T, kind, condition string) map[string]any {
	t.Helper()
	knativeStatus := duckv1.Status{Conditions: duckv1.Conditions{{Type: apis.ConditionReady, Status: corev1.ConditionTrue}}}
	var status any
	switch kind {
	case "TektonConfig":
		status = &operatorv1alpha1.TektonConfigStatus{Status: knativeStatus}
	case "TektonScheduler":
		status = &operatorv1alpha1.TektonSchedulerStatus{Status: knativeStatus}
	case "TektonMulticlusterProxyAAE":
		status = &operatorv1alpha1.TektonMulticlusterProxyAAEStatus{Status: knativeStatus}
	case "SyncerService":
		knativeStatus.ObservedGeneration = 2
		status = &operatorv1alpha1.SyncerServiceStatus{Status: knativeStatus}
	case "Kueue":
		status = &operatorv1.OperatorStatus{ObservedGeneration: 2, Conditions: []operatorv1.OperatorCondition{
			{Type: condition, Status: "True"},
		}}
	default:
		return map[string]any{"conditions": []any{map[string]any{
			"type": condition, "status": "True", "observedGeneration": int64(2),
		}}}
	}
	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(status)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
