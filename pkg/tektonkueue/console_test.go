package tektonkueue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	consolev1 "github.com/openshift/api/console/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

func consoleObjects() (*routev1.Route, *operatorv1.Console, *consolev1.ConsolePlugin) {
	route := &routev1.Route{
		TypeMeta:   metav1.TypeMeta{APIVersion: "route.openshift.io/v1", Kind: "Route"},
		ObjectMeta: metav1.ObjectMeta{Name: "console", Namespace: "openshift-console"},
		Spec:       routev1.RouteSpec{Host: "console.example.test", TLS: &routev1.TLSConfig{Termination: routev1.TLSTerminationReencrypt}},
		Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
			Host: "console.example.test", Conditions: []routev1.RouteIngressCondition{{Type: routev1.RouteAdmitted, Status: "True"}},
		}}},
	}
	console := &operatorv1.Console{
		TypeMeta:   metav1.TypeMeta{APIVersion: "operator.openshift.io/v1", Kind: "Console"},
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: operatorv1.ConsoleSpec{
			OperatorSpec: operatorv1.OperatorSpec{ManagementState: operatorv1.Managed}, Plugins: []string{"pipelines-console-plugin"},
		},
	}
	plugin := &consolev1.ConsolePlugin{
		TypeMeta:   metav1.TypeMeta{APIVersion: "console.openshift.io/v1", Kind: "ConsolePlugin"},
		ObjectMeta: metav1.ObjectMeta{Name: "pipelines-console-plugin"},
		Spec: consolev1.ConsolePluginSpec{Proxy: []consolev1.ConsolePluginProxy{{
			Alias: "multicluster-proxy", Authorization: consolev1.UserToken,
			Endpoint: consolev1.ConsolePluginProxyEndpoint{
				Type:    consolev1.ProxyTypeService,
				Service: &consolev1.ConsolePluginProxyServiceConfig{Name: "proxy-aae", Namespace: "openshift-pipelines", Port: 8080},
			},
		}}},
	}
	return route, console, plugin
}

func TestConsoleRegistrationRequiresAdmittedTLSEnabledPluginAndTokenForwarding(t *testing.T) {
	for _, scenario := range []string{"valid", "host", "unadmitted", "no TLS", "disabled", "no alias", "duplicate alias", "no forwarding", "no service", "terminating"} {
		t.Run(scenario, func(t *testing.T) {
			// Given an otherwise valid OpenShift Console registration.
			route, console, plugin := consoleObjects()
			switch scenario {
			case "host":
				route.Spec.Host = "console.example.test/auth?login=true"
			case "unadmitted":
				route.Status.Ingress = nil
			case "no TLS":
				route.Spec.TLS = nil
			case "disabled":
				console.Spec.Plugins = nil
			case "no alias":
				plugin.Spec.Proxy = nil
			case "duplicate alias":
				plugin.Spec.Proxy = append(plugin.Spec.Proxy, plugin.Spec.Proxy[0])
			case "no forwarding":
				plugin.Spec.Proxy[0].Authorization = consolev1.None
			case "no service":
				plugin.Spec.Proxy[0].Endpoint.Service = nil
			case "terminating":
				plugin.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			}
			// When
			err := consoleRegistration(route, console, plugin)
			// Then only the registered, authenticated TLS console is accepted.
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("registration = %v", err)
			}
		})
	}
}

func TestConsoleCredentialLifetimeIsServerBounded(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"valid", "unbounded", "too long", "expired", "future", "no timestamp", "not user"} {
		t.Run(scenario, func(t *testing.T) {
			// Given server-side token metadata, not caller-declared expiration.
			token := &oauthv1.OAuthAccessToken{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(now.Add(-time.Minute))},
				UserName:   "console-user", ExpiresIn: 3600,
			}
			switch scenario {
			case "unbounded":
				token.ExpiresIn = 0
			case "too long":
				token.ExpiresIn = 10801
			case "expired":
				token.ExpiresIn = 10
			case "future":
				token.CreationTimestamp = metav1.NewTime(now.Add(time.Minute))
			case "no timestamp":
				token.CreationTimestamp = metav1.Time{}
			case "not user":
				token.UserName = ""
			}
			// When
			expires, err := consoleTokenExpiry(token, now)
			// Then a nonexpired user token with a bounded lifetime is required.
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("token expiry = %v", err)
			}
			if err == nil && !expires.Equal(now.Add(59*time.Minute)) {
				t.Fatalf("incorrect expiry: %v", expires)
			}
		})
	}
}

func TestDiscoverConsoleUsesExplicitOAuthFileAndHubAPIs(t *testing.T) {
	// Given explicit OAuth bytes and real API resource identities in a dynamic client.
	token := "sha256~an-explicit-oauth-token"
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICLUSTER_CONSOLE_TOKEN_FILE", path)
	access := &oauthv1.OAuthAccessToken{
		TypeMeta: metav1.TypeMeta{APIVersion: "oauth.openshift.io/v1", Kind: "OAuthAccessToken"},
		// Independently specified OAuth payload hash from the OpenShift
		// AuthenticateToken contract, not calculated by the production helper.
		ObjectMeta: metav1.ObjectMeta{Name: "sha256~TCyKoveDNoDGxOPukm3WTupDrrlaZZkzXm_oatIBv-k", CreationTimestamp: metav1.NewTime(time.Now())},
		UserName:   "console-user", ExpiresIn: 3600,
	}
	route, console, plugin := consoleObjects()
	objects := make([]runtime.Object, 0, 4)
	for _, typed := range []any{access, route, console, plugin} {
		value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(typed)
		if err != nil {
			t.Fatal(err)
		}
		objects = append(objects, &unstructured.Unstructured{Object: value})
	}
	cs := &clients.Clients{Dynamic: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)}
	session := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(session, []byte(`[{"Name":"openshift-session-token","Value":"opaque-session","Path":"/","Domain":"console.example.test","Secure":true}]`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICLUSTER_CONSOLE_SESSION_FILE", session)
	// When
	proxy, err := discoverConsoleProxy(t.Context(), cs)
	// Then discovery keeps the console host and exact plugin proxy base.
	if err != nil {
		t.Fatal(err)
	}
	if proxy.base != "https://console.example.test"+consoleProxyBase || proxy.user != access.UserName || proxy.client.Jar == nil {
		t.Fatal("discovery did not use the explicit credential and registered console")
	}
}

func TestMissingConsoleCredentialDoesNotUseAmbientKubeconfig(t *testing.T) {
	// Given no explicit credential, even though a caller could have ambient auth.
	t.Setenv("MULTICLUSTER_CONSOLE_TOKEN_FILE", "")
	// When
	_, err := discoverConsoleProxy(t.Context(), &clients.Clients{})
	// Then fail before making API calls, without exposing credential bytes.
	if err == nil || !strings.Contains(err.Error(), "MULTICLUSTER_CONSOLE_TOKEN_FILE") {
		t.Fatalf("missing credential = %v", err)
	}
}
