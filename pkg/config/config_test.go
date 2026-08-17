package config

import (
	"os"
	"os/exec"
	"reflect"
	"testing"
)

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
		return
	}

	command := exec.Command(os.Args[0],
		"-test.run=^TestCommandLineFlags$",
		"-kubeconfig=/tmp/hub",
		"-context=hub-context",
		"-cluster=hub-cluster",
		"-spoke-kubeconfig=spoke-1",
		"-spoke-kubeconfig=spoke-2,spoke-3",
	)
	command.Env = append(os.Environ(), "CONFIG_FLAG_HELPER=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("helper process failed: %v\n%s", err, output)
	}
}
