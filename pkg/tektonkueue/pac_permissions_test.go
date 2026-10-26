package tektonkueue

import (
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

func TestPACSecretPermissionOptInIsExactAndNamespaceScoped(t *testing.T) {
	for _, scenario := range []string{"valid", "missing update", "outside allowance", "delete allowance"} {
		t.Run(scenario, func(t *testing.T) {
			var mu sync.Mutex
			var role *rbacv1.Role
			var binding *rbacv1.RoleBinding
			var credential []byte
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var result any
				switch r.Method + " " + r.URL.Path {
				case "POST /apis/rbac.authorization.k8s.io/v1/namespaces/test-ns/roles":
					var value rbacv1.Role
					data, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						return
					}
					if _, _, err := scheme.Codecs.UniversalDeserializer().Decode(data, nil, &value); err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					role = value.DeepCopy()
					mu.Unlock()
					result = &value
				case "POST /apis/rbac.authorization.k8s.io/v1/namespaces/test-ns/rolebindings":
					var value rbacv1.RoleBinding
					data, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						return
					}
					if _, _, err := scheme.Codecs.UniversalDeserializer().Decode(data, nil, &value); err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					binding = value.DeepCopy()
					mu.Unlock()
					result = &value
				case "GET /api/v1/namespaces/openshift-kueue-operator/secrets/test-spoke-1":
					result = &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, Data: map[string][]byte{"kubeconfig": credential}}
				case "POST /apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
					if r.Header.Get("Authorization") != "Bearer scoped-worker" {
						t.Error("did not review actual worker credential")
					}
					data, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						return
					}
					var review authorizationv1.SelfSubjectAccessReview
					if _, _, err := scheme.Codecs.UniversalDeserializer().Decode(data, nil, &review); err != nil {
						t.Error(err)
						return
					}
					a := review.Spec.ResourceAttributes
					mu.Lock()
					allowed := false
					if role != nil && binding != nil && a.Namespace == role.Namespace && binding.RoleRef.Name == role.Name &&
						len(binding.Subjects) == 1 && binding.Subjects[0].Name == "test-spoke-1" {
						for _, rule := range role.Rules {
							allowed = allowed || slices.Contains(rule.Resources, a.Resource) && slices.Contains(rule.Verbs, a.Verb)
						}
					}
					mu.Unlock()
					switch scenario {
					case "missing update":
						if a.Verb == "update" {
							allowed = false
						}
					case "outside allowance":
						if a.Namespace == "default" && a.Verb == "get" {
							allowed = true
						}
					case "delete allowance":
						if a.Namespace == "test-ns" && a.Verb == "delete" {
							allowed = true
						}
					}
					review.Status.Allowed = allowed
					result = &review
				default:
					t.Errorf("unexpected scope mutation/request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if err := json.NewEncoder(w).Encode(result); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			var err error
			credential, err = scopedKubeconfig(&rest.Config{Host: server.URL}, "scoped-worker", ca)
			if err != nil {
				t.Fatal(err)
			}
			admin, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL, TLSClientConfig: rest.TLSClientConfig{CAData: ca}})
			if err != nil {
				t.Fatal(err)
			}
			cs := &clients.Clients{KubeClient: &clients.KubeClient{Kube: admin}}
			e := NewEnvironment(Cluster{Name: "hub", Clients: cs}, []Cluster{{Name: "spoke-1", Clients: cs}}, "test")
			e.Namespace = "test-ns"
			err = e.EnablePACSecretSync(t.Context())
			if (err == nil) != (scenario == "valid") || e.pacSecretSync != (scenario == "valid") {
				t.Fatalf("opt-in permission proof = %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if role == nil || binding == nil || role.Namespace != "test-ns" || binding.Namespace != "test-ns" {
				t.Fatal("missing scoped RBAC")
			}
			rule := role.Rules[0]
			if !reflect.DeepEqual(rule.Verbs, []string{"get", "create", "update"}) || !reflect.DeepEqual(rule.Resources, []string{"secrets"}) || !reflect.DeepEqual(rule.APIGroups, []string{""}) {
				t.Fatal("PAC role exceeded required permissions")
			}
		})
	}
}

func TestPACArmingRequiresExplicitSecretPermissionOptIn(t *testing.T) {
	e := &Environment{}
	if _, err := e.ArmPAC(t.Context(), "correlation"); err == nil {
		t.Fatal("armed PAC without explicit opt-in")
	}
	for _, rule := range workerWriteRole("default").Rules {
		for _, resource := range rule.Resources {
			if resource == "secrets" {
				t.Fatal("default workload role gained Secret access")
			}
		}
	}
}
