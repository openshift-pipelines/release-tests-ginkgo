package multicluster

import (
	"testing"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

func TestOwnsRejectsForeignOrUnclaimedResources(t *testing.T) {
	env := NewEnvironment(&clients.Clients{})
	env.prefix = "rtg-np-test"

	if !env.owns(env.labels()) {
		t.Fatal("owns() rejected this run's own labels")
	}
	for _, labels := range []map[string]string{
		nil,
		{},
		{"app.kubernetes.io/managed-by": "release-tests-ginkgo"},
		{
			"app.kubernetes.io/managed-by": "release-tests-ginkgo",
			"app.kubernetes.io/instance":   "rtg-np-other",
		},
	} {
		if env.owns(labels) {
			t.Fatalf("owns() claimed foreign labels: %v", labels)
		}
	}
}

func TestRunPrefixIsUniquePerEnvironment(t *testing.T) {
	first := NewEnvironment(&clients.Clients{})
	second := NewEnvironment(&clients.Clients{})
	if first.prefix == "" || first.prefix == second.prefix {
		t.Fatalf("run prefixes are not unique: %q vs %q", first.prefix, second.prefix)
	}
	if first.workerName == second.workerName {
		t.Fatalf("worker names are not unique: %q", first.workerName)
	}
	if !first.owns(first.labels()) || second.owns(first.labels()) {
		t.Fatal("ownership labels leak across runs")
	}
}
