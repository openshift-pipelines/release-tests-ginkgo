package config

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestKubeconfigDefaultIsEmpty(t *testing.T) {
	if os.Getenv("CONFIG_EMPTY_KUBECONFIG_HELPER") == "1" {
		if Flags.Kubeconfig != "" {
			t.Fatalf("Kubeconfig = %q, want empty standard-loading override", Flags.Kubeconfig)
		}
		return
	}

	command := exec.Command(os.Args[0], "-test.run=^TestKubeconfigDefaultIsEmpty$")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "KUBECONFIG=") {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, "CONFIG_EMPTY_KUBECONFIG_HELPER=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("helper process failed: %v\n%s", err, output)
	}
}

func TestCommandLineFlags(t *testing.T) {
	if os.Getenv("CONFIG_FLAG_HELPER") == "1" {
		if Flags.Kubeconfig != "/tmp/hub" {
			t.Fatalf("Kubeconfig = %q", Flags.Kubeconfig)
		}
		if Flags.Context != "hub-context" {
			t.Fatalf("Context = %q", Flags.Context)
		}
		if Flags.Cluster != "hub-cluster" {
			t.Fatalf("Cluster = %q", Flags.Cluster)
		}
		wantSpokes := []string{"spoke-1", "spoke-2", "spoke-3"}
		if !reflect.DeepEqual([]string(Flags.SpokeKubeconfigs), wantSpokes) {
			t.Fatalf("SpokeKubeconfigs = %#v, want %#v", Flags.SpokeKubeconfigs, wantSpokes)
		}
		wantContexts := []string{"context-1", "context-2", "context-3"}
		if !reflect.DeepEqual([]string(Flags.SpokeContexts), wantContexts) {
			t.Fatalf("SpokeContexts = %#v, want %#v", Flags.SpokeContexts, wantContexts)
		}
		return
	}

	command := exec.Command(os.Args[0],
		"-test.run=^TestCommandLineFlags$",
		"-kubeconfig=/tmp/hub",
		"-context=hub-context",
		"-cluster=hub-cluster",
		"-spoke-kubeconfig=spoke-1",
		"-spoke-kubeconfig=spoke-2,spoke-3",
		"-spoke-context=context-1",
		"-spoke-context=context-2,context-3",
	)
	command.Env = append(os.Environ(), "CONFIG_FLAG_HELPER=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("helper process failed: %v\n%s", err, output)
	}
}
