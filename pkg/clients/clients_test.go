package clients

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestBuildClientConfigLoadsPathListAndContext(t *testing.T) {
	first := writeKubeconfig(t, "first", "https://first.example.test", "first-token")
	second := writeKubeconfig(t, "second", "https://second.example.test", "second-token")
	t.Setenv(clientcmd.RecommendedConfigPathEnvVar, strings.Join([]string{first, second}, string(os.PathListSeparator)))

	cfg, err := buildClientConfig("", "", "second")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "https://second.example.test" || cfg.BearerToken != "second-token" {
		t.Fatalf("selected host/token = %q/%q", cfg.Host, cfg.BearerToken)
	}
}

func TestBuildClientConfigExplicitPathOverridesEnvironment(t *testing.T) {
	fromEnv := writeKubeconfig(t, "env", "https://env.example.test", "env-token")
	explicit := writeKubeconfig(t, "explicit", "https://explicit.example.test", "explicit-token")
	t.Setenv(clientcmd.RecommendedConfigPathEnvVar, fromEnv)

	cfg, err := BuildClientConfig(explicit, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "https://explicit.example.test" || cfg.BearerToken != "explicit-token" {
		t.Fatalf("selected host/token = %q/%q", cfg.Host, cfg.BearerToken)
	}
}

func TestBuildClientConfigUsesContextCredentialsWithClusterOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	config := clientcmdapi.NewConfig()
	config.Clusters["context-cluster"] = &clientcmdapi.Cluster{Server: "https://context.example.test"}
	config.Clusters["override-cluster"] = &clientcmdapi.Cluster{Server: "https://override.example.test"}
	config.AuthInfos["context-user"] = &clientcmdapi.AuthInfo{Token: "context-token"}
	config.Contexts["selected-context"] = &clientcmdapi.Context{Cluster: "context-cluster", AuthInfo: "context-user"}
	config.CurrentContext = "selected-context"
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		t.Fatal(err)
	}

	cfg, err := BuildClientConfigWithContext(path, "override-cluster", "selected-context")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "https://override.example.test" || cfg.BearerToken != "context-token" {
		t.Fatalf("selected host/token = %q/%q", cfg.Host, cfg.BearerToken)
	}
}

func TestNewClientFromKubeconfigRequiresScheme(t *testing.T) {
	path := writeKubeconfig(t, "spoke", "https://spoke.example.test", "spoke-token")

	_, err := (&Clients{}).NewClientFromKubeconfig(path, "", "")
	if err == nil || !strings.Contains(err.Error(), "configured scheme") {
		t.Fatalf("NewClientFromKubeconfig() error = %v, want missing scheme error", err)
	}

	controllerClient, err := (&Clients{Scheme: createScheme()}).NewClientFromKubeconfig(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if controllerClient == nil {
		t.Fatal("controller-runtime client was not initialized")
	}
}

func TestNewClientsErrorDescribesConnection(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "missing")
	_, err := NewClientsWithContext(configPath, "test-cluster", "test-context", "default")
	if err == nil {
		t.Fatal("NewClientsWithContext() succeeded with a missing kubeconfig")
	}
	for _, detail := range []string{
		fmt.Sprintf("kubeconfig %q", configPath),
		`context "test-context"`,
		`cluster "test-cluster"`,
	} {
		if !strings.Contains(err.Error(), detail) {
			t.Errorf("error %q does not include %q", err, detail)
		}
	}
}

func writeKubeconfig(t *testing.T, name, server, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	config := clientcmdapi.NewConfig()
	config.Clusters[name] = &clientcmdapi.Cluster{Server: server}
	config.AuthInfos[name] = &clientcmdapi.AuthInfo{Token: token}
	config.Contexts[name] = &clientcmdapi.Context{Cluster: name, AuthInfo: name}
	config.CurrentContext = name
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		t.Fatal(err)
	}
	return path
}
