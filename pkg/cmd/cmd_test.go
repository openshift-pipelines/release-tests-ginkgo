package cmd

import (
	"reflect"
	"testing"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

func TestCommandAddsKubernetesConnectionFlags(t *testing.T) {
	original := *config.Flags
	defer func() { *config.Flags = original }()
	config.Flags.Kubeconfig = "/tmp/config"
	config.Flags.Context = "test-context"
	config.Flags.Cluster = "test-cluster"

	want := []string{
		"oc",
		"--kubeconfig", "/tmp/config",
		"--context", "test-context",
		"--cluster", "test-cluster",
		"get", "pods",
	}
	if got := Command("oc", "get", "pods"); !reflect.DeepEqual(got, want) {
		t.Fatalf("Command() = %#v, want %#v", got, want)
	}
}

func TestCommandPreservesExplicitOverrides(t *testing.T) {
	original := *config.Flags
	defer func() { *config.Flags = original }()
	config.Flags.Kubeconfig = "/tmp/default"
	config.Flags.Context = "default-context"
	config.Flags.Cluster = "default-cluster"

	want := []string{
		"kubectl",
		"--kubeconfig=/tmp/explicit",
		"--context", "explicit-context",
		"--cluster=explicit-cluster",
		"get", "pods",
	}
	if got := Command(want...); !reflect.DeepEqual(got, want) {
		t.Fatalf("Command() = %#v, want %#v", got, want)
	}
}

func TestCommandLeavesOtherCommandsUnchanged(t *testing.T) {
	original := *config.Flags
	defer func() { *config.Flags = original }()
	config.Flags.Kubeconfig = "/tmp/config"
	config.Flags.Context = "test-context"
	config.Flags.Cluster = "test-cluster"

	want := []string{"echo", "oc get pods"}
	if got := Command(want...); !reflect.DeepEqual(got, want) {
		t.Fatalf("Command() = %#v, want %#v", got, want)
	}
}

func TestCommandHandlesNoArguments(t *testing.T) {
	if got := Command(); len(got) != 0 {
		t.Fatalf("Command() = %#v, want no arguments", got)
	}
}
