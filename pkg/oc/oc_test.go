package oc

import (
	"reflect"
	"testing"
)

func TestGetOCCommandUsesExplicitConnectionOverrides(t *testing.T) {
	oc := OC{Kubeconfig: "/tmp/spoke", Context: "spoke-context", Cluster: "spoke-cluster"}
	want := []string{
		"oc",
		"--kubeconfig", "/tmp/spoke",
		"--context", "spoke-context",
		"--cluster", "spoke-cluster",
		"get", "pods",
	}
	if got := oc.getOcCommand([]string{"get", "pods"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("getOcCommand() = %#v, want %#v", got, want)
	}
}
