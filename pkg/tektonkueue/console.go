package tektonkueue

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	consolev1 "github.com/openshift/api/console/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

const consoleProxyBase = "/api/proxy/plugin/pipelines-console-plugin/multicluster-proxy"

type consoleProxy struct {
	base    string
	user    string
	expires time.Time
	client  *http.Client
}

// The OAuth token is deliberately separate from the administrator kubeconfig.
// Inspect its server-side lifetime rather than accepting a caller-asserted expiry.
func discoverConsoleProxy(ctx context.Context, cs *clients.Clients) (*consoleProxy, error) {
	path := os.Getenv("MULTICLUSTER_CONSOLE_TOKEN_FILE")
	if path == "" {
		return nil, fmt.Errorf("MULTICLUSTER_CONSOLE_TOKEN_FILE is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read console OAuth credential file")
	}
	token := strings.TrimSpace(string(data))
	payload, supported := strings.CutPrefix(token, "sha256~")
	if !supported || payload == "" || strings.ContainsAny(payload, " \t\r\n") {
		return nil, fmt.Errorf("console OAuth credential must use the supported sha256~payload form")
	}
	hash := sha256.Sum256([]byte(payload))
	name := "sha256~" + base64.RawURLEncoding.EncodeToString(hash[:])
	gvr := schema.GroupVersionResource{Group: "oauth.openshift.io", Version: "v1", Resource: "oauthaccesstokens"}
	object, err := cs.Dynamic.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		// Do not include the API error: the token object's name is credential metadata.
		return nil, requestError(ctx, "inspect hub OAuthAccessToken lifetime")
	}
	var access oauthv1.OAuthAccessToken
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &access); err != nil {
		return nil, fmt.Errorf("decode OAuthAccessToken lifetime")
	}
	expires, err := consoleTokenExpiry(&access, time.Now())
	if err != nil {
		return nil, err
	}
	read := func(group, resource, namespace, name string, into any) error {
		object, err := cs.Dynamic.Resource(schema.GroupVersionResource{
			Group: group, Version: "v1", Resource: resource,
		}).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return requestError(ctx, "discover console registration")
		}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, into); err != nil {
			return fmt.Errorf("decode console registration")
		}
		return nil
	}
	var route routev1.Route
	if err := read("route.openshift.io", "routes", "openshift-console", "console", &route); err != nil {
		return nil, err
	}
	var console operatorv1.Console
	if err := read("operator.openshift.io", "consoles", "", "cluster", &console); err != nil {
		return nil, err
	}
	var plugin consolev1.ConsolePlugin
	if err := read("console.openshift.io", "consoleplugins", "", "pipelines-console-plugin", &plugin); err != nil {
		return nil, err
	}
	if err := consoleRegistration(&route, &console, &plugin); err != nil {
		return nil, err
	}
	proxy := &consoleProxy{
		base: "https://" + route.Spec.Host + consoleProxyBase, user: access.UserName, expires: expires,
		client: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	if err := proxy.loadSession(os.Getenv("MULTICLUSTER_CONSOLE_SESSION_FILE")); err != nil {
		return nil, err
	}
	return proxy, nil
}

// Session cookies are exported out-of-band. The OAuth token above is only for
// lifetime/identity validation, never an alternative to console authentication.
func (p *consoleProxy) loadSession(path string) error {
	if path == "" {
		return fmt.Errorf("MULTICLUSTER_CONSOLE_SESSION_FILE is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read console session file")
	}
	var cookies []*http.Cookie
	if len(data) > 64<<10 || json.Unmarshal(data, &cookies) != nil || len(cookies) == 0 {
		return fmt.Errorf("invalid console session cookie document")
	}
	origin, err := url.Parse(strings.TrimSuffix(p.base, consoleProxyBase))
	if err != nil {
		return fmt.Errorf("invalid console session origin")
	}
	for _, cookie := range cookies {
		if cookie == nil || cookie.Valid() != nil || cookie.Name == "" || cookie.Value == "" || !cookie.Secure ||
			(cookie.Domain != "" && strings.TrimPrefix(cookie.Domain, ".") != origin.Hostname()) ||
			cookie.Path != "/" || cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && !cookie.Expires.After(time.Now())) {
			return fmt.Errorf("console session cookies must be secure and scoped to the discovered console host")
		}
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return fmt.Errorf("create console session jar")
	}
	jar.SetCookies(origin, cookies)
	p.client.Jar = jar
	return nil
}

func (p *consoleProxy) authenticate(ctx context.Context, username string) error {
	var user struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	endpoint := strings.TrimSuffix(p.base, consoleProxyBase) + "/api/kubernetes/apis/user.openshift.io/v1/users/~"
	if _, err := p.json(ctx, endpoint, &user); err != nil {
		return err
	}
	if user.Kind != "User" || user.Metadata.Name != username {
		return fmt.Errorf("authenticated console identity does not match the bounded OAuth user")
	}
	return nil
}

func consoleTokenExpiry(token *oauthv1.OAuthAccessToken, now time.Time) (time.Time, error) {
	if token.ExpiresIn <= 0 || token.ExpiresIn > int64((3*time.Hour)/time.Second) || token.CreationTimestamp.IsZero() || token.UserName == "" {
		return time.Time{}, fmt.Errorf("console credential must be a user OAuth token with a lifetime of at most three hours")
	}
	expires := token.CreationTimestamp.Add(time.Duration(token.ExpiresIn) * time.Second)
	if !expires.After(now) || token.CreationTimestamp.After(now) {
		return time.Time{}, fmt.Errorf("console OAuth credential is expired or has a future creation time")
	}
	return expires, nil
}

func consoleRegistration(route *routev1.Route, console *operatorv1.Console, plugin *consolev1.ConsolePlugin) error {
	host, err := url.Parse("https://" + route.Spec.Host)
	if err != nil || host.Hostname() == "" || host.Host != route.Spec.Host || host.Path != "" || host.RawQuery != "" || host.Fragment != "" || host.User != nil || host.Port() != "" {
		return fmt.Errorf("hub Console Route has an invalid HTTPS host")
	}
	admitted := false
	for _, ingress := range route.Status.Ingress {
		for _, condition := range ingress.Conditions {
			if ingress.Host == route.Spec.Host && condition.Type == routev1.RouteAdmitted && condition.Status == "True" {
				admitted = true
			}
		}
	}
	if route.DeletionTimestamp != nil || route.Spec.TLS == nil || !admitted {
		return fmt.Errorf("hub Console Route is not admitted with TLS")
	}
	if console.Spec.ManagementState != operatorv1.Managed || !slices.Contains(console.Spec.Plugins, "pipelines-console-plugin") {
		return fmt.Errorf("pipelines-console-plugin is not enabled in the hub Console")
	}
	matches := 0
	for _, proxy := range plugin.Spec.Proxy {
		if proxy.Alias != "multicluster-proxy" {
			continue
		}
		matches++
		service := proxy.Endpoint.Service
		if proxy.Authorization != consolev1.UserToken || proxy.Endpoint.Type != consolev1.ProxyTypeService ||
			service == nil || service.Name == "" || service.Namespace == "" || service.Port <= 0 || service.Port > 65535 {
			return fmt.Errorf("multicluster-proxy must register an HTTPS Service with UserToken authorization")
		}
	}
	if plugin.DeletionTimestamp != nil || matches != 1 {
		return fmt.Errorf("pipelines ConsolePlugin must register exactly one multicluster-proxy")
	}
	return nil
}
