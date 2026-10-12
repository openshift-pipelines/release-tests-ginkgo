package operator_test

import (
	"fmt"
	"log"

	. "github.com/onsi/ginkgo/v2"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// knativeWebhookDeployments lists deployments that use the knative webhook library and
// therefore receive WEBHOOK_TLS_MIN_VERSION / WEBHOOK_TLS_CURVE_PREFERENCES (not the
// unprefixed TLS_* vars) when the cluster TLS profile changes (SRVKP-11926 / SRVKP-12915).
var knativeWebhookDeployments = []string{
	config.PipelineWebhookName, // tekton-pipelines-webhook
	config.TriggerWebhookName,  // tekton-triggers-webhook
	config.PacWebhookName,      // pipelines-as-code-webhook
}

// tlsEnvDeployments lists non-knative Go deployments that receive TLS_MIN_VERSION /
// TLS_CURVE_PREFERENCES (without the WEBHOOK_ prefix) from the Tekton Operator.
//
// Deliberately excluded from both lists:
//   - tekton-operator-proxy-webhook (openshift-pipelines): reads APIServer TLS profile
//     at process startup and sets WEBHOOK_TLS_* via os.Setenv — this is a runtime
//     process env var, not a Deployment spec env var, so it cannot be asserted here.
//   - tekton-operator-webhook (openshift-operators namespace): same runtime os.Setenv
//     mechanism; also lives in a different namespace.
var tlsEnvDeployments = []string{
	config.TriggersInterceptorsName, // tekton-triggers-core-interceptors
	config.ResultsAPIName,           // tekton-results-api
}

var _ = Describe("SRVKP-11926: Central TLS profile propagation to Pipelines components",
	Serial, Ordered, ContinueOnFailure, Label("e2e", "operator", "admin", "tls-profile"), func() {

		var originalProfile string

		BeforeAll(func() {
			if operator.IsHostedCluster(sharedClients) {
				Skip("Skipping TLS profile propagation tests: APIServer/cluster is immutable on HyperShift hosted clusters")
			}

			lastNamespace = config.TargetNamespace
			operator.EnsureTektonConfigStatusInstalled(
				sharedClients.TektonConfig(), store.GetCRNames())

			originalProfile = operator.GetClusterTLSProfileType(sharedClients)
			log.Printf("Saved original cluster TLS profile: %q", originalProfile)

			DeferCleanup(func() {
				log.Printf("Restoring cluster TLS profile to %q", originalProfile)
				operator.PatchClusterTLSProfile(sharedClients, originalProfile)
				operator.EnsureTektonConfigStatusInstalled(
					sharedClients.TektonConfig(), store.GetCRNames())
			})
		})

		// ── Intermediate profile ──────────────────────────────────────────────
		// "Intermediate" is supported on all OCP versions (4.x+).
		// It sets TLS_MIN_VERSION=VersionTLS12 on Go-based components.

		It("SRVKP-11926-TC01: Intermediate profile propagates correct TLS env vars to all Go-based deployments",
			Label("tls-profile", "intermediate"), func() {

				operator.PatchClusterTLSProfile(sharedClients, config.TLSProfileIntermediate)
				operator.EnsureTektonConfigStatusInstalled(
					sharedClients.TektonConfig(), store.GetCRNames())

				for _, deployment := range knativeWebhookDeployments {
					log.Printf("Asserting %s has %s=%s",
						deployment, config.WebhookTLSMinVersionEnvVar, config.TLSVersionTLS12)
					operator.AssertDeploymentHasEnvVar(
						sharedClients,
						config.TargetNamespace,
						deployment,
						config.WebhookTLSMinVersionEnvVar,
						config.TLSVersionTLS12,
					)
				}
				for _, deployment := range tlsEnvDeployments {
					log.Printf("Asserting %s has %s=%s",
						deployment, config.TLSMinVersionEnvVar, config.TLSVersionTLS12)
					operator.AssertDeploymentHasEnvVar(
						sharedClients,
						config.TargetNamespace,
						deployment,
						config.TLSMinVersionEnvVar,
						config.TLSVersionTLS12,
					)
				}
			})

		It("SRVKP-11926-TC02: Intermediate profile propagates TLSv1.2+TLSv1.3 to nginx console plugin ConfigMap",
			Label("tls-profile", "intermediate", "nginx"), func() {

				// Cluster is already in Intermediate profile from TC01 (Ordered suite).
				operator.AssertNginxConfigMapHasTLSProfile(
					sharedClients,
					config.TargetNamespace,
					config.NginxConsolePluginConfigMap,
					config.TLSProfileIntermediate,
				)
			})

		// ── Curve preference propagation (SRVKP-12915 / operator#4157) ────────
		// Intermediate groups from the OpenShift API are converted IANA→Knative
		// (secp256r1 → P-256) before injection into Deployment env vars and nginx.

		It("SRVKP-12915-TC01: Intermediate profile propagates Knative curve prefs to knative webhooks",
			Label("tls-profile", "intermediate", "curves"), func() {

				for _, deployment := range knativeWebhookDeployments {
					log.Printf("Asserting %s has %s containing %v",
						deployment, config.WebhookTLSCurvePreferencesEnvVar,
						config.IntermediateTLSCurvePreferences)
					operator.AssertDeploymentEnvVarHasAllSubstrings(
						sharedClients,
						config.TargetNamespace,
						deployment,
						config.WebhookTLSCurvePreferencesEnvVar,
						config.IntermediateTLSCurvePreferences,
					)
					operator.AssertDeploymentEnvVarDoesNotContain(
						sharedClients,
						config.TargetNamespace,
						deployment,
						config.WebhookTLSCurvePreferencesEnvVar,
						config.TLSCurveIANAP256,
					)
				}
			})

		It("SRVKP-12915-TC02: Intermediate profile propagates Knative curve prefs to tlsEnvDeployments",
			Label("tls-profile", "intermediate", "curves"), func() {

				for _, deployment := range tlsEnvDeployments {
					log.Printf("Asserting %s has %s containing %v",
						deployment, config.TLSCurvePreferencesEnvVar,
						config.IntermediateTLSCurvePreferences)
					operator.AssertDeploymentEnvVarHasAllSubstrings(
						sharedClients,
						config.TargetNamespace,
						deployment,
						config.TLSCurvePreferencesEnvVar,
						config.IntermediateTLSCurvePreferences,
					)
					operator.AssertDeploymentEnvVarDoesNotContain(
						sharedClients,
						config.TargetNamespace,
						deployment,
						config.TLSCurvePreferencesEnvVar,
						config.TLSCurveIANAP256,
					)
				}
			})

		It("SRVKP-12915-TC03: Intermediate profile propagates ssl_ecdh_curve with Knative group names to nginx",
			Label("tls-profile", "intermediate", "nginx", "curves"), func() {

				operator.AssertNginxConfigMapContains(
					sharedClients,
					config.TargetNamespace,
					config.NginxConsolePluginConfigMap,
					append([]string{"ssl_ecdh_curve"}, config.IntermediateNginxECDHCurves...),
				)
				operator.AssertNginxConfigMapDoesNotContain(
					sharedClients,
					config.TargetNamespace,
					config.NginxConsolePluginConfigMap,
					"ssl_ecdh_curve",
					config.TLSCurveIANAP256,
				)
			})

		// ── Modern profile (OCP 4.17+) ────────────────────────────────────────
		// Skip cleanly when the cluster rejects the Modern type.

		It("SRVKP-12915-TC04: Modern profile propagates TLS 1.3 min version, curves, and nginx settings",
			Label("tls-profile", "modern", "curves"), func() {

				if err := operator.TryPatchClusterTLSProfile(sharedClients, config.TLSProfileModern); err != nil {
					Skip(fmt.Sprintf(
						"Modern TLS profile unsupported on this cluster (requires OCP 4.17+): %v", err))
				}
				operator.EnsureTektonConfigStatusInstalled(
					sharedClients.TektonConfig(), store.GetCRNames())

				for _, deployment := range knativeWebhookDeployments {
					log.Printf("Asserting %s has %s=%s",
						deployment, config.WebhookTLSMinVersionEnvVar, config.TLSVersionTLS13)
					operator.AssertDeploymentHasEnvVar(
						sharedClients,
						config.TargetNamespace,
						deployment,
						config.WebhookTLSMinVersionEnvVar,
						config.TLSVersionTLS13,
					)
					operator.AssertDeploymentEnvVarHasAllSubstrings(
						sharedClients,
						config.TargetNamespace,
						deployment,
						config.WebhookTLSCurvePreferencesEnvVar,
						config.IntermediateTLSCurvePreferences,
					)
					operator.AssertDeploymentEnvVarDoesNotContain(
						sharedClients,
						config.TargetNamespace,
						deployment,
						config.WebhookTLSCurvePreferencesEnvVar,
						config.TLSCurveIANAP256,
					)
				}
				for _, deployment := range tlsEnvDeployments {
					operator.AssertDeploymentHasEnvVar(
						sharedClients,
						config.TargetNamespace,
						deployment,
						config.TLSMinVersionEnvVar,
						config.TLSVersionTLS13,
					)
					operator.AssertDeploymentEnvVarHasAllSubstrings(
						sharedClients,
						config.TargetNamespace,
						deployment,
						config.TLSCurvePreferencesEnvVar,
						config.IntermediateTLSCurvePreferences,
					)
				}

				operator.AssertNginxConfigMapHasTLSProfile(
					sharedClients,
					config.TargetNamespace,
					config.NginxConsolePluginConfigMap,
					config.TLSProfileModern,
				)
				operator.AssertNginxConfigMapContains(
					sharedClients,
					config.TargetNamespace,
					config.NginxConsolePluginConfigMap,
					append([]string{"ssl_ecdh_curve"}, config.IntermediateNginxECDHCurves...),
				)
			})
	})
