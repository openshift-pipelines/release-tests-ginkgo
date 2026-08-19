package operator_test

import (
	"log"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/olm"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

var _ = Describe("NetworkPolicy", Serial, Ordered,
	Label("e2e", "admin", "networkpolicy"), func() {

		BeforeAll(func() {
			lastNamespace = config.TargetNamespace
			operator.ValidateOperatorInstallStatus(sharedClients, store.GetCRNames())
			DeferCleanup(func() {
				operator.RestoreNetworkPolicyDisabled(sharedClients)
			})
		})

		// --- TektonTrigger --- namespace: openshift-pipelines
		Context("TektonTrigger", Label("triggers"), func() {
			triggerPolicies := []string{
				"tekton-default-deny",
				"triggers-controller",
				"triggers-webhook",
				"triggers-core-interceptors",
				"triggers-core-interceptors-egress-internet",
			}

			It("policies exist after TektonConfig is Ready", func() {
				operator.AssertNetworkPoliciesExist(sharedClients,
					config.TargetNamespace, triggerPolicies...)
			})

			It("toggle off removes policies", func() {
				operator.PatchNetworkPolicyDisabled(sharedClients,
					store.GetCRNames(), true)
				operator.AssertNetworkPoliciesAbsent(sharedClients,
					config.TargetNamespace, triggerPolicies...)
				operator.AssertTektonConfigReady(sharedClients,
					store.GetCRNames())
			})

			It("toggle on restores policies and pods are Ready",
				func() {
					operator.PatchNetworkPolicyDisabled(sharedClients,
						store.GetCRNames(), false)
					operator.AssertNetworkPoliciesExist(sharedClients,
						config.TargetNamespace, triggerPolicies...)
					operator.AssertPodsReady(sharedClients,
						config.TargetNamespace,
						"app.kubernetes.io/name=controller,app.kubernetes.io/part-of=tekton-triggers")
				})
		})

		// --- TektonPipeline --- namespace: openshift-pipelines
		Context("TektonPipeline", Label("pipelines"), func() {
			pipelinePolicies := []string{
				"tekton-pipelines-default-deny",
				"pipeline-controller",
				"pipeline-webhook",
				"pipeline-events-controller",
				"pipeline-resolvers",
				"tekton-proxy-webhook-default-deny",
				"proxy-webhook",
			}

			It("policies exist after TektonConfig is Ready", func() {
				operator.AssertNetworkPoliciesExist(sharedClients,
					config.TargetNamespace, pipelinePolicies...)
			})

			It("toggle off removes policies", func() {
				operator.PatchNetworkPolicyDisabled(sharedClients,
					store.GetCRNames(), true)
				operator.AssertNetworkPoliciesAbsent(sharedClients,
					config.TargetNamespace, pipelinePolicies...)
				operator.AssertTektonConfigReady(sharedClients,
					store.GetCRNames())
			})

			It("toggle on restores policies and pods are Ready",
				func() {
					operator.PatchNetworkPolicyDisabled(sharedClients,
						store.GetCRNames(), false)
					operator.AssertNetworkPoliciesExist(sharedClients,
						config.TargetNamespace, pipelinePolicies...)
					operator.AssertPodsReady(sharedClients,
						config.TargetNamespace,
						"app.kubernetes.io/name=controller,app.kubernetes.io/part-of=tekton-pipelines")
				})
		})

		// --- Operator (static policies) --- namespace: openshift-operators
		Context("Operator static policies", Label("operator"), func() {
			operatorPolicies := []string{
				"tekton-operator",
				"tekton-operator-proxy-webhook",
			}

			It("policies exist in openshift-operators namespace", func() {
				operator.AssertNetworkPoliciesExist(sharedClients,
					olm.OperatorsNamespace, operatorPolicies...)

				// Check for platform default-allow-all policy that may
				// supersede these.
				npList, err := sharedClients.KubeClient.Kube.NetworkingV1().
					NetworkPolicies(olm.OperatorsNamespace).
					List(sharedClients.Ctx, metav1.ListOptions{})
				if err == nil {
					for _, np := range npList.Items {
						if np.Name == "default-allow-all" {
							log.Printf("Platform default-allow-all "+
								"policy detected in %s; operator "+
								"policies exist but enforcement "+
								"may be superseded",
								olm.OperatorsNamespace)
						}
					}
				}
			})
			// Note: Operator static policies are not toggled by
			// spec.networkPolicy.disabled — no toggle-off/on tests.
		})

		// --- TektonChains --- namespace: openshift-pipelines
		Context("TektonChains", Label("chains"), func() {
			chainsPolicies := []string{
				"chains-default-deny",
				"tekton-chains-controller",
			}

			It("policies exist after TektonConfig is Ready", func() {
				operator.AssertNetworkPoliciesExist(sharedClients,
					config.TargetNamespace, chainsPolicies...)
			})

			It("toggle off removes policies", func() {
				operator.PatchNetworkPolicyDisabled(sharedClients,
					store.GetCRNames(), true)
				operator.AssertNetworkPoliciesAbsent(sharedClients,
					config.TargetNamespace, chainsPolicies...)
				operator.AssertTektonConfigReady(sharedClients,
					store.GetCRNames())
			})

			It("toggle on restores policies and pods are Ready",
				func() {
					operator.PatchNetworkPolicyDisabled(sharedClients,
						store.GetCRNames(), false)
					operator.AssertNetworkPoliciesExist(sharedClients,
						config.TargetNamespace, chainsPolicies...)
					operator.AssertPodsReady(sharedClients,
						config.TargetNamespace,
						"app.kubernetes.io/name=controller,app.kubernetes.io/part-of=tekton-chains")
				})
		})

		// --- ManualApprovalGate --- namespace: openshift-pipelines
		Context("ManualApprovalGate", Label("mag"), func() {
			magPolicies := []string{
				"mag-default-deny",
				"manual-approval-gate-controller",
				"manual-approval-gate-webhook",
			}

			It("policies exist after TektonConfig is Ready", func() {
				operator.AssertNetworkPoliciesExist(sharedClients,
					config.TargetNamespace, magPolicies...)
			})

			It("toggle off removes policies", func() {
				operator.PatchNetworkPolicyDisabled(sharedClients,
					store.GetCRNames(), true)
				operator.AssertNetworkPoliciesAbsent(sharedClients,
					config.TargetNamespace, magPolicies...)
				operator.AssertTektonConfigReady(sharedClients,
					store.GetCRNames())
			})

			It("toggle on restores policies and pods are Ready",
				func() {
					operator.PatchNetworkPolicyDisabled(sharedClients,
						store.GetCRNames(), false)
					operator.AssertNetworkPoliciesExist(sharedClients,
						config.TargetNamespace, magPolicies...)
					operator.AssertPodsReady(sharedClients,
						config.TargetNamespace,
						"app.kubernetes.io/name=controller,app.kubernetes.io/part-of=manual-approval-gate")
				})
		})

		// --- OpenShiftPipelinesAsCode --- namespace: pipelines-as-code
		Context("OpenShiftPipelinesAsCode", Label("pac"), func() {
			pacNamespace := "pipelines-as-code"
			pacPolicies := []string{
				"pac-default-deny",
				"pac-controller",
				"pac-watcher",
				"pac-webhook",
			}

			It("policies exist after TektonConfig is Ready", func() {
				operator.AssertNetworkPoliciesExist(sharedClients,
					pacNamespace, pacPolicies...)
			})

			It("toggle off removes policies", func() {
				operator.PatchNetworkPolicyDisabled(sharedClients,
					store.GetCRNames(), true)
				operator.AssertNetworkPoliciesAbsent(sharedClients,
					pacNamespace, pacPolicies...)
				operator.AssertTektonConfigReady(sharedClients,
					store.GetCRNames())
			})

			It("toggle on restores policies and pods are Ready",
				func() {
					operator.PatchNetworkPolicyDisabled(sharedClients,
						store.GetCRNames(), false)
					operator.AssertNetworkPoliciesExist(sharedClients,
						pacNamespace, pacPolicies...)
					operator.AssertPodsReady(sharedClients,
						pacNamespace,
						"app.kubernetes.io/part-of=pipelines-as-code")
				})
		})

		// --- TektonPruner --- namespace: openshift-pipelines
		Context("TektonPruner", Label("pruner"), func() {
			prunerPolicies := []string{
				"pruner-default-deny",
				"tekton-pruner-controller",
				"tekton-pruner-webhook",
			}

			It("policies exist after TektonConfig is Ready", func() {
				operator.AssertNetworkPoliciesExist(sharedClients,
					config.TargetNamespace, prunerPolicies...)
			})

			It("toggle off removes policies", func() {
				operator.PatchNetworkPolicyDisabled(sharedClients,
					store.GetCRNames(), true)
				operator.AssertNetworkPoliciesAbsent(sharedClients,
					config.TargetNamespace, prunerPolicies...)
				operator.AssertTektonConfigReady(sharedClients,
					store.GetCRNames())
			})

			It("toggle on restores policies and pods are Ready",
				func() {
					operator.PatchNetworkPolicyDisabled(sharedClients,
						store.GetCRNames(), false)
					operator.AssertNetworkPoliciesExist(sharedClients,
						config.TargetNamespace, prunerPolicies...)
					operator.AssertPodsReady(sharedClients,
						config.TargetNamespace,
						"app.kubernetes.io/name=controller,app.kubernetes.io/part-of=tektoncd-pruner")
				})
		})

		// --- Console Plugin --- namespace: openshift-pipelines
		Context("Console Plugin", Label("console"), func() {
			consolePolicies := []string{
				"console-plugin-default-deny",
				"console-plugin",
			}

			It("policies exist after TektonConfig is Ready", func() {
				operator.AssertNetworkPoliciesExist(sharedClients,
					config.TargetNamespace, consolePolicies...)
			})

			It("toggle off removes policies", func() {
				operator.PatchNetworkPolicyDisabled(sharedClients,
					store.GetCRNames(), true)
				operator.AssertNetworkPoliciesAbsent(sharedClients,
					config.TargetNamespace, consolePolicies...)
				operator.AssertTektonConfigReady(sharedClients,
					store.GetCRNames())
			})

			It("toggle on restores policies and pods are Ready",
				func() {
					operator.PatchNetworkPolicyDisabled(sharedClients,
						store.GetCRNames(), false)
					operator.AssertNetworkPoliciesExist(sharedClients,
						config.TargetNamespace, consolePolicies...)
					operator.AssertPodsReady(sharedClients,
						config.TargetNamespace,
						"app.kubernetes.io/name=pipelines-console-plugin")
				})
		})

		// --- TektonResults --- namespace: tekton-results
		// TODO: TektonResults network policies depend on tektoncd/operator#3808 merging.
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
