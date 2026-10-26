package tektonkueue_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	olmpkg "github.com/openshift-pipelines/release-tests-ginkgo/pkg/olm"
	workload "github.com/openshift-pipelines/release-tests-ginkgo/pkg/tektonkueue"
)

type clusterTarget struct {
	name   string
	config clientConfig
}

func clusterTargets(cfg suiteConfig) []clusterTarget {
	targets := make([]clusterTarget, 0, 1+len(cfg.Spokes))
	targets = append(targets, clusterTarget{name: "hub", config: cfg.Hub})
	for i, spoke := range cfg.Spokes {
		targets = append(targets, clusterTarget{name: fmt.Sprintf("spoke-%d", i+1), config: spoke})
	}
	return targets
}

func configuredClusters(ctx SpecContext, namespace string) []workload.Cluster {
	targets := clusterTargets(suiteConfig{Hub: hubConfig, Spokes: spokeConfigs})
	configured := make([]workload.Cluster, 0, len(targets))
	for _, target := range targets {
		By(fmt.Sprintf("installing or reusing prerequisites on %s", target.name))
		cs, err := clients.NewClientsWithContext(
			target.config.Kubeconfig, target.config.Cluster, target.config.Context, namespace)
		Expect(err).NotTo(HaveOccurred(), "failed to create clients for %s", target.name)
		controllerClient, err := cs.NewClientFromKubeconfig(
			target.config.Kubeconfig, target.config.Cluster, target.config.Context)
		Expect(err).NotTo(HaveOccurred(), "failed to create bootstrap client for %s", target.name)
		prerequisites := append([]olmpkg.OperatorPrerequisite{olmpkg.PipelinePrerequisite()}, olmpkg.MulticlusterPrerequisites()...)
		Expect((&olmpkg.ClusterBootstrap{Client: controllerClient}).EnsurePrerequisites(
			ctx, cs.KubeClient.Kube.Discovery(), prerequisites)).To(Succeed(), "failed to bootstrap %s", target.name)
		configured = append(configured, workload.Cluster{Name: target.name, Clients: cs})
	}
	return configured
}

func TestClusterTargetsPreservesExplicitConfiguration(t *testing.T) {
	// Given distinct credentials, contexts, clusters, and namespaces for every target.
	cfg := suiteConfig{
		Hub: clientConfig{Kubeconfig: "hub", Cluster: "hub-cluster", Context: "hub-context", TargetNamespace: "hub-ns"},
		Spokes: []clientConfig{
			{Kubeconfig: "spoke-a", Cluster: "a", Context: "context-a", TargetNamespace: "ns-a"},
			{Kubeconfig: "spoke-b", Cluster: "b", Context: "context-b", TargetNamespace: "ns-b"},
		},
	}
	// When
	targets := clusterTargets(cfg)
	// Then
	if len(targets) != 3 || targets[0].name != "hub" || targets[0].config != cfg.Hub {
		t.Fatalf("hub target was lost: %+v", targets)
	}
	for i, spoke := range cfg.Spokes {
		if targets[i+1].name != fmt.Sprintf("spoke-%d", i+1) || targets[i+1].config != spoke {
			t.Fatalf("spoke configuration was lost: %+v", targets[i+1])
		}
	}
}
