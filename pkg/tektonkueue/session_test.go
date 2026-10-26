package tektonkueue

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

func TestConsoleSessionBoundaryRejectsBearerOnlyHTTPAndWebSocket(t *testing.T) {
	proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unauthenticated request crossed console middleware")
	})
	if err := proxy.authenticate(t.Context(), "console-user"); err != nil {
		t.Fatal(err)
	}
	proxy.client.Jar = nil
	request, err := http.NewRequestWithContext(t.Context(), "GET", proxy.base+wireRun.path("resolve"), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+wireBearer)
	response, err := proxy.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("bearer-only HTTP crossed console middleware")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	dialer := websocket.Dialer{TLSClientConfig: proxy.client.Transport.(*http.Transport).TLSClientConfig}
	conn, response, err := dialer.DialContext(t.Context(), "wss"+strings.TrimPrefix(proxy.base, "https")+wireRun.logPath(wireRun.pod),
		http.Header{"Authorization": {"Bearer " + wireBearer}})
	if conn != nil {
		_ = conn.Close()
		t.Fatal("bearer-only socket crossed console middleware")
	}
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatal("missing console session did not reject upgrade")
	}
}

func TestArmingRequiresSessionIdentityMatchingOAuthUserBeforeAnyTrigger(t *testing.T) {
	f := executionEnvironment(t)
	proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) })
	proxy.user = "different-oauth-user"
	if _, err := f.environment.arm(t.Context(), proxy); err == nil {
		t.Fatal("accepted a different console user")
	}
	if f.subscriptions.Load() != 0 {
		t.Fatal("armed execution before session identity was verified")
	}
}

func TestConsoleSessionFileIsAnExplicitSecureCookieJar(t *testing.T) {
	proxy := proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) })
	proxy.client.Jar = nil
	path := filepath.Join(t.TempDir(), "session.json")
	// An opaque out-of-band console session; no token-to-cookie fabrication.
	data := `[{"Name":"openshift-session-token","Value":"opaque-session","Path":"/","Secure":true,"HttpOnly":true}]`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := proxy.loadSession(path); err != nil {
		t.Fatal(err)
	}
	if err := proxy.authenticate(t.Context(), proxy.user); err != nil {
		t.Fatal(err)
	}
	origin, err := url.Parse(strings.TrimSuffix(proxy.base, consoleProxyBase))
	if err != nil {
		t.Fatal(err)
	}
	if len(proxy.client.Jar.Cookies(origin)) == 0 {
		t.Fatal("session jar is empty")
	}
	for _, invalid := range []string{`[]`, `[{"Name":"openshift-session-token","Value":"opaque-session","Path":"/"}]`,
		`[{"Name":"openshift-session-token","Value":"opaque-session","Path":"/","Domain":"other.example","Secure":true}]`} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if err := proxy.loadSession(path); err == nil {
			t.Fatal("accepted an invalid or cross-host session cookie")
		}
	}
}

func TestOAuthLookupRejectsUnsupportedTokenForms(t *testing.T) {
	for _, value := range []string{"unprefixed-token", "sha256~", "sha256~bad payload"} {
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("MULTICLUSTER_CONSOLE_TOKEN_FILE", path)
		if _, err := discoverConsoleProxy(t.Context(), &clients.Clients{}); err == nil {
			t.Fatal("unsupported token reached the API lookup")
		}
	}
}
