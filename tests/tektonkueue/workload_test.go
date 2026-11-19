package tektonkueue_test

import (
	"context"
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega
	olm "github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	olmpkg "github.com/openshift-pipelines/release-tests-ginkgo/pkg/olm"
	workload "github.com/openshift-pipelines/release-tests-ginkgo/pkg/tektonkueue"
)

var _ = Describe("Multi-cluster PipelineRun execution: PIPELINES-42", Serial, Label("tekton-kueue", "workload", "webhook", "admin", "e2e"), func() {
	It("defaults and preserves queue labels while executing hub PipelineRuns on a spoke: PIPELINES-42-TC01", NodeTimeout(2*time.Hour), func(specCtx SpecContext) {
		prefix := "rtg-mk-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		type target struct {
			name   string
			config clientConfig
		}
		targets := make([]target, 0, 1+len(spokeConfigs))
		targets = append(targets, target{name: "hub", config: hubConfig})
		for i, cfg := range spokeConfigs {
			targets = append(targets, target{name: fmt.Sprintf("spoke-%d", i+1), config: cfg})
		}

		configured := make([]workload.Cluster, 0, len(targets))
		for _, target := range targets {
			By(fmt.Sprintf("[%s] bootstrapping operators: Pipelines catalog=%s channel=%s",
				target.name, config.Flags.CatalogSource, config.Flags.PipelineOperatorChannel))
			clusterClients, err := clients.NewClientsWithContext(
				target.config.Kubeconfig,
				target.config.Cluster,
				target.config.Context,
				prefix,
			)
			Expect(err).NotTo(HaveOccurred(), "failed to create clients for %s", target.name)

			controllerClient, err := clusterClients.NewClientFromKubeconfig(
				target.config.Kubeconfig,
				target.config.Cluster,
				target.config.Context,
			)
			Expect(err).NotTo(HaveOccurred(), "failed to create bootstrap client for %s", target.name)
			// EnsureOperator deliberately reuses existing subscriptions. Reject a
			// different build rather than reporting a nightly/stage test against it.
			subscriptions := &olm.SubscriptionList{}
			Expect(controllerClient.List(specCtx, subscriptions)).To(Succeed(), "[%s] list subscriptions", target.name)
			for _, sub := range subscriptions.Items {
				if sub.Spec != nil && sub.Spec.Package == config.PipelineOperatorPackageName {
					Expect(sub.Spec.CatalogSource).To(Equal(config.Flags.CatalogSource),
						"[%s] existing Pipelines subscription uses a different catalog; install the requested build first", target.name)
					Expect(sub.Spec.Channel).To(Equal(config.Flags.PipelineOperatorChannel),
						"[%s] existing Pipelines subscription uses a different channel; install the requested build first", target.name)
				}
			}
			prerequisites := append([]olmpkg.OperatorPrerequisite{olmpkg.PipelinePrerequisite()}, olmpkg.MulticlusterPrerequisites()...)
			Expect((&olmpkg.ClusterBootstrap{Client: controllerClient}).EnsurePrerequisites(
				specCtx, clusterClients.KubeClient.Kube.Discovery(), prerequisites)).To(Succeed(),
				"failed to bootstrap %s", target.name)
			GinkgoWriter.Printf("[%s] required operators and APIs are ready\n", target.name)
			configured = append(configured, workload.Cluster{Name: target.name, Clients: clusterClients})
		}

		environment := workload.NewEnvironment(configured[0], configured[1:], prefix)
		DeferCleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			if err := environment.Cleanup(cleanupCtx); err != nil {
				GinkgoWriter.Printf("retrying failed multi-cluster cleanup: %v\n", err)
				Expect(environment.Cleanup(cleanupCtx)).To(Succeed(), "failed to restore multi-cluster test environment")
			}
		})

		Expect(environment.Setup(specCtx)).To(Succeed(), "failed to configure multi-cluster workload environment")
		GinkgoWriter.Printf("[hub] multicluster setup configured successfully; all queues, admission checks, and worker connections are Active\n")
		for _, scenario := range []struct {
			name        string
			prelabelled bool
		}{
			{name: "defaulted"},
			{name: "prelabelled", prelabelled: true},
		} {
			By(fmt.Sprintf("executing the %s queue-label scenario", scenario.name))
			result, err := environment.Execute(specCtx, scenario.name, scenario.prelabelled)
			Expect(err).NotTo(HaveOccurred(), "multi-cluster PipelineRun %s execution failed", scenario.name)
			Expect(result.Logs).To(ContainSubstring("multi-cluster-execution-ok"),
				"selected spoke %s did not return the expected workload log", result.Spoke)
			GinkgoWriter.Printf("PipelineRun %s executed on %s and completed on the hub; spoke logs:\n%s",
				result.PipelineRun, result.Spoke, result.Logs)
		}
		GinkgoWriter.Printf("[hub] multicluster workload validation completed successfully on %d spoke(s)\n", len(spokeConfigs))
	})
})
