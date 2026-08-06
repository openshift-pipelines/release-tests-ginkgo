package operator_test

import (
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// operandNS is the primary operand namespace for most Tekton components.
const operandNS = config.TargetNamespace

// operatorNS is the namespace where the Operator controller itself runs.
const operatorNS = "openshift-operators"

// pacNS is the namespace for OpenShift Pipelines as Code workloads.
const pacNS = config.TargetNamespace

// resultsNS is the namespace for Tekton Results workloads.
const resultsNS = config.TargetNamespace

var _ = Describe("NetworkPolicy — TektonTrigger", Serial, Ordered,
	Label("e2e", "admin", "networkpolicy", "triggers"), func() {
		var crNames = store.GetCRNames

		BeforeAll(func() {
			lastNamespace = operandNS
			operator.ValidateOperatorInstallStatus(sharedClients, crNames())
			DeferCleanup(func() {
				operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			})
		})

		It("[id:TRGR-NP-01] default NetworkPolicies exist after install", Label("sanity"), func() {
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"tekton-default-deny",
				"triggers-controller",
				"triggers-webhook",
				"triggers-core-interceptors",
				"triggers-core-interceptors-egress-internet",
			)
		})

		It("[id:TRGR-NP-02] disabling networkPolicy removes Triggers policies", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), true)
			operator.AssertNetworkPoliciesAbsent(sharedClients, operandNS,
				"triggers-controller",
				"triggers-webhook",
				"triggers-core-interceptors",
				"triggers-core-interceptors-egress-internet",
			)
		})

		It("[id:TRGR-NP-03] re-enabling restores Triggers policies and workloads are Ready", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"tekton-default-deny",
				"triggers-controller",
				"triggers-webhook",
				"triggers-core-interceptors",
				"triggers-core-interceptors-egress-internet",
			)
			operator.AssertPodsReady(sharedClients, operandNS,
				"app.kubernetes.io/name=tekton-triggers-controller")
			operator.AssertPodsReady(sharedClients, operandNS,
				"app.kubernetes.io/name=tekton-triggers-webhook")
			operator.AssertPodsReady(sharedClients, operandNS,
				"app.kubernetes.io/name=tekton-triggers-core-interceptors")
		})
	})

var _ = Describe("NetworkPolicy — TektonPipeline", Serial, Ordered,
	Label("e2e", "admin", "networkpolicy", "pipelines"), func() {
		var crNames = store.GetCRNames

		BeforeAll(func() {
			lastNamespace = operandNS
			operator.ValidateOperatorInstallStatus(sharedClients, crNames())
			DeferCleanup(func() {
				operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			})
		})

		It("[id:PIPE-NP-01] default NetworkPolicies exist after install", Label("sanity"), func() {
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"tekton-pipelines-default-deny",
				"pipeline-controller",
				"pipeline-webhook",
				"pipeline-events-controller",
				"pipeline-resolvers",
				"tekton-proxy-webhook-default-deny",
				"proxy-webhook",
			)
		})

		It("[id:PIPE-NP-02] disabling networkPolicy removes Pipeline policies", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), true)
			operator.AssertNetworkPoliciesAbsent(sharedClients, operandNS,
				"pipeline-controller",
				"pipeline-webhook",
				"pipeline-events-controller",
				"pipeline-resolvers",
				"proxy-webhook",
			)
		})

		It("[id:PIPE-NP-03] re-enabling restores Pipeline policies and workloads are Ready", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"tekton-pipelines-default-deny",
				"pipeline-controller",
				"pipeline-webhook",
				"pipeline-events-controller",
				"pipeline-resolvers",
				"tekton-proxy-webhook-default-deny",
				"proxy-webhook",
			)
			operator.AssertPodsReady(sharedClients, operandNS,
				"app.kubernetes.io/name=tekton-pipelines-controller")
			operator.AssertPodsReady(sharedClients, operandNS,
				"app.kubernetes.io/name=tekton-pipelines-webhook")
			operator.AssertPodsReady(sharedClients, operandNS,
				"app=tekton-operator-proxy-webhook")
		})
	})

var _ = Describe("NetworkPolicy — Operator (static)", Serial, Ordered,
	Label("e2e", "admin", "networkpolicy", "operator"), func() {

		BeforeAll(func() {
			lastNamespace = operatorNS
			operator.ValidateOperatorInstallStatus(sharedClients, store.GetCRNames())
		})

		It("[id:OP-NP-01] static operator NetworkPolicies exist in openshift-operators", Label("sanity"), func() {
			operator.AssertNetworkPoliciesExist(sharedClients, operatorNS,
				"tekton-operator",
				"tekton-operator-proxy-webhook",
			)
		})

		// Note: openshift-operators ships a platform-level default-allow-all NetworkPolicy that
		// supersedes these static policies on a fresh cluster. The test above only asserts the
		// policies exist; traffic enforcement requires manual removal of the platform policy.
	})

var _ = Describe("NetworkPolicy — TektonChains", Serial, Ordered,
	Label("e2e", "admin", "networkpolicy", "chains"), func() {
		var crNames = store.GetCRNames

		BeforeAll(func() {
			lastNamespace = operandNS
			operator.ValidateOperatorInstallStatus(sharedClients, crNames())
			DeferCleanup(func() {
				operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			})
		})

		It("[id:CHN-NP-01] default NetworkPolicies exist after install", Label("sanity"), func() {
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"chains-default-deny",
				"tekton-chains-controller",
			)
		})

		It("[id:CHN-NP-02] disabling networkPolicy removes Chains policies", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), true)
			operator.AssertNetworkPoliciesAbsent(sharedClients, operandNS,
				"chains-default-deny",
				"tekton-chains-controller",
			)
		})

		It("[id:CHN-NP-03] re-enabling restores Chains policies and controller is Ready", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"chains-default-deny",
				"tekton-chains-controller",
			)
			operator.AssertPodsReady(sharedClients, operandNS,
				"app.kubernetes.io/name=tekton-chains-controller")
		})
	})

var _ = Describe("NetworkPolicy — ManualApprovalGate", Serial, Ordered,
	Label("e2e", "admin", "networkpolicy", "mag"), func() {
		var crNames = store.GetCRNames

		BeforeAll(func() {
			lastNamespace = operandNS
			operator.ValidateOperatorInstallStatus(sharedClients, crNames())
			DeferCleanup(func() {
				operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			})
		})

		It("[id:MAG-NP-01] default NetworkPolicies exist after install", Label("sanity"), func() {
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"mag-default-deny",
				"manual-approval-gate-controller",
				"manual-approval-gate-webhook",
			)
		})

		It("[id:MAG-NP-02] disabling networkPolicy removes MAG policies", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), true)
			operator.AssertNetworkPoliciesAbsent(sharedClients, operandNS,
				"mag-default-deny",
				"manual-approval-gate-controller",
				"manual-approval-gate-webhook",
			)
		})

		It("[id:MAG-NP-03] re-enabling restores MAG policies and workloads are Ready", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"mag-default-deny",
				"manual-approval-gate-controller",
				"manual-approval-gate-webhook",
			)
			operator.AssertPodsReady(sharedClients, operandNS,
				"app.kubernetes.io/name=manual-approval-gate-controller")
			operator.AssertPodsReady(sharedClients, operandNS,
				"app.kubernetes.io/name=manual-approval-gate-webhook")
		})
	})

var _ = Describe("NetworkPolicy — OpenShift Pipelines as Code", Serial, Ordered,
	Label("e2e", "admin", "networkpolicy", "pac"), func() {
		var crNames = store.GetCRNames

		BeforeAll(func() {
			lastNamespace = pacNS
			operator.ValidateOperatorInstallStatus(sharedClients, crNames())
			DeferCleanup(func() {
				operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			})
		})

		It("[id:PAC-NP-01] default NetworkPolicies exist after install", Label("sanity"), func() {
			operator.AssertNetworkPoliciesExist(sharedClients, pacNS,
				"pac-default-deny",
				"pac-controller",
				"pac-watcher",
				"pac-webhook",
			)
		})

		It("[id:PAC-NP-02] disabling networkPolicy removes PAC policies", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), true)
			operator.AssertNetworkPoliciesAbsent(sharedClients, pacNS,
				"pac-default-deny",
				"pac-controller",
				"pac-watcher",
				"pac-webhook",
			)
		})

		It("[id:PAC-NP-03] re-enabling restores PAC policies and workloads are Ready", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			operator.AssertNetworkPoliciesExist(sharedClients, pacNS,
				"pac-default-deny",
				"pac-controller",
				"pac-watcher",
				"pac-webhook",
			)
			operator.AssertPodsReady(sharedClients, pacNS,
				"app.kubernetes.io/name=pipelines-as-code-controller")
			operator.AssertPodsReady(sharedClients, pacNS,
				"app.kubernetes.io/name=pipelines-as-code-watcher")
			operator.AssertPodsReady(sharedClients, pacNS,
				"app.kubernetes.io/name=pipelines-as-code-webhook")
		})
	})

var _ = Describe("NetworkPolicy — TektonPruner", Serial, Ordered,
	Label("e2e", "admin", "networkpolicy", "pruner"), func() {
		var crNames = store.GetCRNames

		BeforeAll(func() {
			lastNamespace = operandNS
			operator.ValidateOperatorInstallStatus(sharedClients, crNames())
			DeferCleanup(func() {
				operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			})
		})

		It("[id:PRN-NP-01] default NetworkPolicies exist after install", Label("sanity"), func() {
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"pruner-default-deny",
				"tekton-pruner-controller",
				"tekton-pruner-webhook",
			)
		})

		It("[id:PRN-NP-02] disabling networkPolicy removes Pruner policies", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), true)
			operator.AssertNetworkPoliciesAbsent(sharedClients, operandNS,
				"pruner-default-deny",
				"tekton-pruner-controller",
				"tekton-pruner-webhook",
			)
		})

		It("[id:PRN-NP-03] re-enabling restores Pruner policies and workloads are Ready", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"pruner-default-deny",
				"tekton-pruner-controller",
				"tekton-pruner-webhook",
			)
			operator.AssertPodsReady(sharedClients, operandNS,
				"app.kubernetes.io/name=tekton-pruner-controller")
			operator.AssertPodsReady(sharedClients, operandNS,
				"app.kubernetes.io/name=tekton-pruner-webhook")
		})
	})

var _ = Describe("NetworkPolicy — Console Plugin", Serial, Ordered,
	Label("e2e", "admin", "networkpolicy", "console"), func() {
		var crNames = store.GetCRNames

		BeforeAll(func() {
			lastNamespace = operandNS
			operator.ValidateOperatorInstallStatus(sharedClients, crNames())
			DeferCleanup(func() {
				operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			})
		})

		It("[id:CON-NP-01] default NetworkPolicies exist after install", Label("sanity"), func() {
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"console-plugin-default-deny",
				"console-plugin",
			)
		})

		It("[id:CON-NP-02] disabling networkPolicy removes Console Plugin policies", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), true)
			operator.AssertNetworkPoliciesAbsent(sharedClients, operandNS,
				"console-plugin-default-deny",
				"console-plugin",
			)
		})

		It("[id:CON-NP-03] re-enabling restores Console Plugin policies and pod is Ready", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			operator.AssertNetworkPoliciesExist(sharedClients, operandNS,
				"console-plugin-default-deny",
				"console-plugin",
			)
			operator.AssertPodsReady(sharedClients, operandNS,
				"app.kubernetes.io/name=pipelines-console-plugin")
		})
	})

var _ = Describe("NetworkPolicy — TektonResults", Serial, Ordered,
	Label("e2e", "admin", "networkpolicy", "results"), func() {
		var crNames = store.GetCRNames

		BeforeAll(func() {
			lastNamespace = resultsNS
			operator.ValidateOperatorInstallStatus(sharedClients, crNames())
			DeferCleanup(func() {
				operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			})
		})

		// TODO: update policy names and namespace once tektoncd/operator#3808 is merged.
		It("[id:RES-NP-01] default NetworkPolicies exist after install", Label("sanity"), func() {
			operator.AssertNetworkPoliciesExist(sharedClients, resultsNS,
				"results-default-deny",
				"results-api",
				"results-watcher",
				"results-retention-policy-agent",
				"results-postgres",
			)
		})

		It("[id:RES-NP-02] disabling networkPolicy removes Results policies", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), true)
			operator.AssertNetworkPoliciesAbsent(sharedClients, resultsNS,
				"results-default-deny",
				"results-api",
				"results-watcher",
				"results-retention-policy-agent",
				"results-postgres",
			)
		})

		It("[id:RES-NP-03] re-enabling restores Results policies and API pod is Ready", func() {
			operator.PatchNetworkPolicyDisabled(sharedClients, crNames(), false)
			operator.AssertNetworkPoliciesExist(sharedClients, resultsNS,
				"results-default-deny",
				"results-api",
				"results-watcher",
				"results-retention-policy-agent",
				"results-postgres",
			)
			operator.AssertPodsReady(sharedClients, resultsNS,
				"app.kubernetes.io/name=tekton-results-api")
		})
	})
