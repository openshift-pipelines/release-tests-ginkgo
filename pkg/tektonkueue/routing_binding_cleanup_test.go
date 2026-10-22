package tektonkueue

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

func TestRoutingClusterRoleBindingCleanupConflicts(t *testing.T) {
	for _, mode := range []string{
		"preserve foreign values", "UID", "labels", "subjects",
		"post-update labels", "post-update subjects", "exhausted", "canceled",
	} {
		t.Run(mode, func(t *testing.T) {
			// Given: real typed client requests against an API returning conflicts.
			var mu sync.Mutex
			var current, updated *rbacv1.ClusterRoleBinding
			attempts, deleteAttempts, deletes := 0, 0, 0
			postUpdate := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				respond := func(code int, value any) {
					w.WriteHeader(code)
					if err := json.NewEncoder(w).Encode(value); err != nil {
						t.Error(err)
					}
				}
				switch r.Method {
				case http.MethodGet:
					if current == nil {
						respond(http.StatusNotFound, &metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Reason: metav1.StatusReasonNotFound, Code: 404})
						return
					}
					respond(http.StatusOK, current)
				case http.MethodPost:
					current = &rbacv1.ClusterRoleBinding{}
					if err := json.NewDecoder(r.Body).Decode(current); err != nil {
						t.Error(err)
						return
					}
					current.TypeMeta = metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRoleBinding"}
					current.UID = "owned"
					current.ResourceVersion = "1"
					respond(http.StatusCreated, current)
				case http.MethodPut:
					attempts++
					if attempts == 1 && mode != "post-update labels" && mode != "post-update subjects" || mode == "exhausted" {
						current.Annotations = map[string]string{"foreign": "preserved"}
						switch mode {
						case "UID":
							current.UID = "replacement"
						case "labels":
							current.Labels = map[string]string{"owner": "foreign"}
						case "subjects":
							current.Subjects = append(current.Subjects, rbacv1.Subject{Kind: "User", Name: "foreign"})
						}
						respond(http.StatusConflict, &metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Reason: metav1.StatusReasonConflict, Code: 409})
						return
					}
					updated = &rbacv1.ClusterRoleBinding{}
					if err := json.NewDecoder(r.Body).Decode(updated); err != nil {
						t.Error(err)
						return
					}
					current = updated.DeepCopy()
					current.ResourceVersion = strconv.Itoa(attempts + 1)
					postUpdate = mode == "post-update labels" || mode == "post-update subjects"
					respond(http.StatusOK, current)
				case http.MethodDelete:
					deleteAttempts++
					if postUpdate {
						postUpdate = false
						current.Annotations = map[string]string{"foreign": "preserved"}
						current.ResourceVersion = "foreign-version"
						if mode == "post-update labels" {
							current.Labels = map[string]string{"owner": "foreign"}
						} else {
							current.Subjects = []rbacv1.Subject{{Kind: "User", Name: "foreign"}}
						}
					}
					var options metav1.DeleteOptions
					if err := json.NewDecoder(r.Body).Decode(&options); err != nil {
						t.Error(err)
						return
					}
					if options.Preconditions == nil || options.Preconditions.UID == nil || options.Preconditions.ResourceVersion == nil {
						t.Error("binding delete lacked owned UID and resourceVersion preconditions")
					}
					if options.Preconditions == nil || options.Preconditions.UID == nil ||
						options.Preconditions.ResourceVersion == nil ||
						*options.Preconditions.UID != current.UID ||
						*options.Preconditions.ResourceVersion != current.ResourceVersion {
						respond(http.StatusConflict, &metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Reason: metav1.StatusReasonConflict, Code: 409})
						return
					}
					deletes++
					current = nil
					respond(http.StatusOK, &metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: metav1.StatusSuccess})
				default:
					t.Errorf("unexpected API method %s", r.Method)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			kube, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL,
				ContentConfig: rest.ContentConfig{ContentType: "application/json", AcceptContentTypes: "application/json"}})
			if err != nil {
				t.Fatal(err)
			}
			env := &Environment{Prefix: "test", routingWaits: true}
			cluster := Cluster{Name: "hub", Clients: &clients.Clients{KubeClient: &clients.KubeClient{Kube: kube}}}
			binding := &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "test"},
				RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "test"},
				Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "owned", Namespace: "test"}}}
			if err := env.createClusterRoleBinding(t.Context(), cluster, binding); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			// When: invoke the registered cleanup through Environment.
			err = env.Cleanup(ctx)
			// Then
			mu.Lock()
			defer mu.Unlock()
			switch mode {
			case "preserve foreign values":
				if err != nil || attempts != 2 || deleteAttempts != 1 || deletes != 1 ||
					updated == nil || updated.Annotations["foreign"] != "preserved" || len(updated.Subjects) != 0 {
					t.Fatalf("cleanup = %v, updates=%d, delete attempts=%d, deletes=%d, updated=%+v",
						err, attempts, deleteAttempts, deletes, updated)
				}
			case "canceled":
				if !errors.Is(err, context.Canceled) || attempts != 0 || deleteAttempts != 0 || deletes != 0 {
					t.Fatalf("canceled cleanup = %v, updates=%d, delete attempts=%d, deletes=%d",
						err, attempts, deleteAttempts, deletes)
				}
			case "exhausted":
				if !apierrors.IsConflict(err) || attempts != 5 || deleteAttempts != 0 || deletes != 0 {
					t.Fatalf("bounded cleanup = %v, updates=%d, delete attempts=%d, deletes=%d",
						err, attempts, deleteAttempts, deletes)
				}
			default:
				wantDeleteAttempts := 0
				if mode == "post-update labels" || mode == "post-update subjects" {
					wantDeleteAttempts = 1
				}
				if err == nil || attempts != 1 || deleteAttempts != wantDeleteAttempts ||
					deletes != 0 || current == nil || current.Annotations["foreign"] != "preserved" {
					t.Fatalf("foreign binding cleanup = %v, updates=%d, delete attempts=%d, deletes=%d, current=%+v",
						err, attempts, deleteAttempts, deletes, current)
				}
				switch mode {
				case "UID":
					if current.UID != "replacement" {
						t.Fatal("lost replacement UID")
					}
				case "labels":
					if current.Labels["owner"] != "foreign" {
						t.Fatal("lost foreign labels")
					}
				case "subjects":
					if len(current.Subjects) != 2 || current.Subjects[1].Name != "foreign" {
						t.Fatal("lost foreign subjects")
					}
				case "post-update labels":
					if current.Labels["owner"] != "foreign" {
						t.Fatal("lost post-update foreign labels")
					}
				case "post-update subjects":
					if len(current.Subjects) != 1 || current.Subjects[0].Name != "foreign" {
						t.Fatal("lost post-update foreign subjects")
					}
				}
			}
		})
	}
}
