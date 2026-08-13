package operator_test

import (
	"log"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// Covers issue #56: E2E for NetworkPolicy missing components
// (Resolvers, MultiCluster, Results).
var _ = Describe("NetworkPolicy — missing components", Serial, Ordered,
	Label("e2e", "admin", "networkpolicy"), func() {

		BeforeAll(func() {
			lastNamespace = config.TargetNamespace
			operator.ValidateOperatorInstallStatus(sharedClients, store.GetCRNames())
			DeferCleanup(func() {
				operator.RestoreNetworkPolicyDisabled(sharedClients)
			})
		})

		// --- Resolvers --- namespace: tekton-pipelines-resolvers
		// Resolvers run in a separate namespace with their own default-deny
		// and allow policies. Ref: issue #56.
		Context("Resolvers", Label("resolvers"), func() {
			resolversNamespace := "tekton-pipelines-resolvers"
			resolverPolicies := []string{
				"resolvers-default-deny",
				"tekton-pipelines-resolvers",
			}

			It("policies exist after TektonConfig is Ready", func() {
				operator.AssertNetworkPoliciesExist(sharedClients,
					resolversNamespace, resolverPolicies...)
			})

			It("toggle off removes policies", func() {
				operator.PatchNetworkPolicyDisabled(sharedClients,
					store.GetCRNames(), true)
				operator.AssertNetworkPoliciesAbsent(sharedClients,
					resolversNamespace, resolverPolicies...)
				operator.AssertTektonConfigReady(sharedClients,
					store.GetCRNames())
			})

			It("toggle on restores policies and pods are Ready",
				func() {
					operator.PatchNetworkPolicyDisabled(sharedClients,
						store.GetCRNames(), false)
					operator.AssertNetworkPoliciesExist(sharedClients,
						resolversNamespace, resolverPolicies...)
					operator.AssertPodsReady(sharedClients,
						resolversNamespace,
						"app.kubernetes.io/part-of=tekton-pipelines-resolvers")
				})
		})

		// --- MultiCluster --- hub↔spoke API-server path
		// MultiCluster network policies guard the hub controller and its
		// egress to spoke API servers. Tests require at least one spoke
		// kubeconfig to be provided (--spoke-kubeconfig). Jira: SRVKP-13325.
		Context("MultiCluster", Label("multicluster"), func() {
			multiclusterPolicies := []string{
				"multicluster-default-deny",
				"multicluster-hub-controller",
				"multicluster-hub-controller-egress-apiserver",
			}

			BeforeEach(func() {
				if len(config.Flags.SpokeKubeconfigs) == 0 {
					Skip("MultiCluster tests require --spoke-kubeconfig; skipping")
				}
			})

			It("hub policies exist after TektonConfig is Ready", func() {
				operator.AssertNetworkPoliciesExist(sharedClients,
					config.TargetNamespace, multiclusterPolicies...)
			})

			It("toggle off removes hub policies", func() {
				operator.PatchNetworkPolicyDisabled(sharedClients,
					store.GetCRNames(), true)
				operator.AssertNetworkPoliciesAbsent(sharedClients,
					config.TargetNamespace, multiclusterPolicies...)
				operator.AssertTektonConfigReady(sharedClients,
					store.GetCRNames())
			})

			It("toggle on restores hub policies and pods are Ready",
				func() {
					operator.PatchNetworkPolicyDisabled(sharedClients,
						store.GetCRNames(), false)
					operator.AssertNetworkPoliciesExist(sharedClients,
						config.TargetNamespace, multiclusterPolicies...)
					operator.AssertPodsReady(sharedClients,
						config.TargetNamespace,
						"app.kubernetes.io/part-of=multicluster-controller")
				})

			It("spoke policies exist on spoke cluster", func() {
				for i, spokeKubeconfig := range config.Flags.SpokeKubeconfigs {
					spokeCtx := ""
					if i < len(config.Flags.SpokeContexts) {
						spokeCtx = config.Flags.SpokeContexts[i]
					}
					spokeClients, err := newSpokeClients(spokeKubeconfig, spokeCtx)
					if err != nil {
						log.Printf("Warning: failed to create spoke clients for %s: %v", spokeKubeconfig, err)
						continue
					}
					spokePolicies := []string{
						"multicluster-spoke-default-deny",
						"multicluster-spoke-agent",
					}
					operator.AssertNetworkPoliciesExist(spokeClients,
						config.TargetNamespace, spokePolicies...)
				}
			})
		})

		// --- TektonResults --- namespace: tekton-results
		// Results network policies. Dependency: tektoncd/operator#3808.
		Context("TektonResults", Label("results"), func() {
			resultsNamespace := "tekton-results"
			resultsPolicies := []string{
				"results-default-deny",
				"results-api",
				"results-watcher",
				"results-retention-policy-agent",
				"results-postgres",
			}

			It("policies exist after TektonConfig is Ready", func() {
				operator.AssertNetworkPoliciesExist(sharedClients,
					resultsNamespace, resultsPolicies...)
			})

			It("toggle off removes policies", func() {
				operator.PatchNetworkPolicyDisabled(sharedClients,
					store.GetCRNames(), true)
				operator.AssertNetworkPoliciesAbsent(sharedClients,
					resultsNamespace, resultsPolicies...)
				operator.AssertTektonConfigReady(sharedClients,
					store.GetCRNames())
			})

			It("toggle on restores policies and pods are Ready",
				func() {
					operator.PatchNetworkPolicyDisabled(sharedClients,
						store.GetCRNames(), false)
					operator.AssertNetworkPoliciesExist(sharedClients,
						resultsNamespace, resultsPolicies...)
					operator.AssertPodsReady(sharedClients,
						resultsNamespace,
						"app.kubernetes.io/part-of=tekton-results")
				})
		})
	})

// newSpokeClients creates a Kubernetes client for a spoke cluster.
func newSpokeClients(kubeconfig, context string) (*clients.Clients, error) {
	return clients.NewClients(kubeconfig, context, config.TargetNamespace)
}
