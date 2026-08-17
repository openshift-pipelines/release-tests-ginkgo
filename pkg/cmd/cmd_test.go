package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/onsi/gomega"

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

func TestCommandIgnoresConnectionFlagsAfterSeparator(t *testing.T) {
	original := *config.Flags
	defer func() { *config.Flags = original }()
	config.Flags.Kubeconfig = ""
	config.Flags.Context = "selected-context"
	config.Flags.Cluster = ""

	want := []string{
		"oc", "--context", "selected-context",
		"exec", "pod", "--", "command", "--context=command-argument",
	}
	if got := Command("oc", "exec", "pod", "--", "command", "--context=command-argument"); !reflect.DeepEqual(got, want) {
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

func TestRunWithEnvUsesConnectionFlags(t *testing.T) {
	original := *config.Flags
	defer func() { *config.Flags = original }()
	config.Flags.Kubeconfig = ""
	config.Flags.Context = "selected-context"
	config.Flags.Cluster = ""
	addFakeOC(t)

	result := RunWithEnv([]string{"TEST_ENV=value"}, "oc", "get", "pods")
	if result.ExitCode != 0 {
		t.Fatalf("command failed: %s", result.Stderr())
	}
	if !strings.Contains(result.Stdout(), "--context\nselected-context\n") {
		t.Fatalf("command output %q does not contain the selected context", result.Stdout())
	}
}

func TestMustSucceedWithStdinUsesConnectionFlags(t *testing.T) {
	gomega.RegisterTestingT(t)
	original := *config.Flags
	defer func() { *config.Flags = original }()
	config.Flags.Kubeconfig = ""
	config.Flags.Context = "selected-context"
	config.Flags.Cluster = ""
	addFakeOC(t)

	result := MustSucceedWithStdin(strings.NewReader("input"), "oc", "create", "-f", "-")
	if !strings.Contains(result.Stdout(), "--context\nselected-context\n") {
		t.Fatalf("command output %q does not contain the selected context", result.Stdout())
	}
}

func addFakeOC(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "oc"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n/bin/cat >/dev/null\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
