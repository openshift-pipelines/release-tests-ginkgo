package clients

import (
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

	cfg, err := BuildClientConfig("", "", "second")
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

	cfg, err := BuildClientConfig(explicit, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "https://explicit.example.test" || cfg.BearerToken != "explicit-token" {
		t.Fatalf("selected host/token = %q/%q", cfg.Host, cfg.BearerToken)
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
