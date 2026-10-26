package tektonkueue

import (
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

func TestWorkerScopeChecksProxyReadsAndDeniesSecretsAndOtherNamespaces(t *testing.T) {
	for _, scenario := range []string{"valid", "missing log read", "global pod read", "secret read"} {
		t.Run(scenario, func(t *testing.T) {
			// Given a real TLS access-review endpoint reporting propagated RBAC.
			var mu sync.Mutex
			checked := map[string]bool{}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews" || r.Method != "POST" {
					t.Errorf("unexpected scope request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				var review authorizationv1.SelfSubjectAccessReview
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if _, _, err := scheme.Codecs.UniversalDeserializer().Decode(data, nil, &review); err != nil {
					t.Error(err)
					return
				}
				a := review.Spec.ResourceAttributes
				key := a.Namespace + "/" + a.Verb + "/" + a.Resource + "/" + a.Subresource
				mu.Lock()
				checked[key] = true
				mu.Unlock()
				review.Status.Allowed = a.Namespace == "test-ns" && a.Resource != "secrets"
				if scenario == "missing log read" && a.Subresource == "log" {
					review.Status.Allowed = false
				}
				if scenario == "global pod read" && a.Resource == "pods" {
					review.Status.Allowed = true
				}
				if scenario == "secret read" && a.Resource == "secrets" {
					review.Status.Allowed = true
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(review); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			kubeconfig, err := scopedKubeconfig(&rest.Config{Host: server.URL}, "worker-token", ca)
			if err != nil {
				t.Fatal(err)
			}
			// When the real worker-scope verifier reviews required and forbidden actions.
			err = verifyWorkerCredentialScope(t.Context(), kubeconfig, "test-ns")
			// Then only the strictly namespaced, Secret-denying credential is accepted.
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("scope verifier = %v", err)
			}
			if scenario == "valid" {
				mu.Lock()
				defer mu.Unlock()
				for _, key := range []string{"test-ns/get/secrets/", "test-ns/list/taskruns/", "default/list/taskruns/",
					"test-ns/get/pods/log", "default/get/pods/log"} {
					if !checked[key] {
						t.Errorf("scope verifier did not check %s", key)
					}
				}
			}
		})
	}
}
