package operator

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

// tlsProfilePatch is the JSON merge-patch shape for APIServer/cluster.
type tlsProfilePatch struct {
	Spec tlsProfilePatchSpec `json:"spec"`
}

type tlsProfilePatchSpec struct {
	TLSSecurityProfile *configv1.TLSSecurityProfile `json:"tlsSecurityProfile"`
}

// IsHostedCluster returns true when the cluster is a HyperShift hosted cluster
// (controlPlaneTopology == "External"). On hosted clusters, the APIServer/cluster
// resource is managed via the HostedCluster object and cannot be patched directly,
// so TLS profile propagation tests must be skipped.
//
// Uses the oc CLI rather than a Go API client so that it works in environments
// where the test process network is restricted (e.g. sandboxed runners).
func IsHostedCluster(_ *clients.Clients) bool {
	result := cmd.Run("oc", "get", "infrastructure", "cluster",
		"-o", "jsonpath={.status.controlPlaneTopology}")
	if result.ExitCode != 0 {
		log.Printf("Warning: could not get infrastructure/cluster topology: %s", result.Combined())
		return false
	}
	topology := strings.TrimSpace(result.Stdout())
	log.Printf("Cluster controlPlaneTopology: %q", topology)
	return topology == "External"
}

// GetClusterTLSProfileType reads the current tlsSecurityProfile type from
// APIServer/cluster. Returns "Intermediate" when no explicit profile is set
// (cluster default).
func GetClusterTLSProfileType(cs *clients.Clients) string {
	apiServer, err := cs.ProxyConfig.APIServers().Get(
		context.TODO(), "cluster", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get APIServer/cluster")
	if apiServer.Spec.TLSSecurityProfile == nil {
		return config.TLSProfileIntermediate
	}
	return string(apiServer.Spec.TLSSecurityProfile.Type)
}

// PatchClusterTLSProfile patches APIServer/cluster to the given profileType
// ("Modern" or "Intermediate"). Any other value resets the profile to nil
// (cluster default).
func PatchClusterTLSProfile(cs *clients.Clients, profileType string) {
	Expect(TryPatchClusterTLSProfile(cs, profileType)).NotTo(HaveOccurred(),
		"failed to patch APIServer/cluster TLS profile to %q", profileType)
}

// TryPatchClusterTLSProfile is like PatchClusterTLSProfile but returns the
// API error instead of failing the Ginkgo assertion. Callers that need to
// Skip when a profile type is unsupported (e.g. Modern on OCP < 4.17) should
// use this helper.
func TryPatchClusterTLSProfile(cs *clients.Clients, profileType string) error {
	var profile *configv1.TLSSecurityProfile
	switch profileType {
	case config.TLSProfileModern:
		profile = &configv1.TLSSecurityProfile{
			Type:   configv1.TLSProfileModernType,
			Modern: &configv1.ModernTLSProfile{},
		}
	case config.TLSProfileIntermediate:
		profile = &configv1.TLSSecurityProfile{
			Type:         configv1.TLSProfileIntermediateType,
			Intermediate: &configv1.IntermediateTLSProfile{},
		}
	case config.TLSProfileOld:
		profile = &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileOldType,
			Old:  &configv1.OldTLSProfile{},
		}
	default:
		profile = nil
	}

	patch := tlsProfilePatch{}
	patch.Spec.TLSSecurityProfile = profile
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	log.Printf("Patching APIServer/cluster TLS profile to %q", profileType)
	_, err = cs.ProxyConfig.APIServers().Patch(
		context.TODO(), "cluster",
		types.MergePatchType, patchBytes,
		metav1.PatchOptions{})
	return err
}

// AssertDeploymentHasEnvVar polls a Deployment until all containers carry the
// env var envName=expectedValue, or the APITimeout is reached.
// This is the primary assertion for Go-based components that receive TLS
// settings via environment variables injected by the Tekton Operator.
func AssertDeploymentHasEnvVar(cs *clients.Clients, ns, deploymentName, envName, expectedValue string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true,
		func(context.Context) (bool, error) {
			deployment, err := cs.KubeClient.Kube.AppsV1().Deployments(ns).Get(
				context.TODO(), deploymentName, metav1.GetOptions{})
			if err != nil {
				log.Printf("Waiting for deployment %s: %v", deploymentName, err)
				return false, nil
			}
			for _, container := range deployment.Spec.Template.Spec.Containers {
				found := false
				for _, env := range container.Env {
					if env.Name == envName {
						if env.Value == expectedValue {
							found = true
						} else {
							log.Printf("Deployment %s container %s: %s=%s (want %s)",
								deploymentName, container.Name, envName, env.Value, expectedValue)
							return false, nil
						}
					}
				}
				if !found {
					log.Printf("Deployment %s container %s: env var %s not yet present",
						deploymentName, container.Name, envName)
					return false, nil
				}
			}
			log.Printf("Deployment %s: confirmed %s=%s on all containers", deploymentName, envName, expectedValue)
			return true, nil
		})
	Expect(err).NotTo(HaveOccurred(),
		"deployment %s/%s does not have %s=%s after waiting",
		ns, deploymentName, envName, expectedValue)
}

// AssertNginxConfigMapHasTLSProfile polls a ConfigMap that holds an nginx
// configuration and checks that the ssl_protocols directive matches the
// expected TLS profile.
//
// Modern      → ssl_protocols must include "TLSv1.3" exclusively.
// Intermediate → ssl_protocols must include both "TLSv1.2" and "TLSv1.3".
//
// NOTE: update config.NginxConsolePluginConfigMap with the exact ConfigMap
// name once confirmed from a live cluster.
func AssertNginxConfigMapHasTLSProfile(cs *clients.Clients, ns, configMapName, profileType string) {
	var requiredProtocols []string
	switch profileType {
	case config.TLSProfileModern:
		requiredProtocols = []string{"TLSv1.3"}
	case config.TLSProfileIntermediate:
		requiredProtocols = []string{"TLSv1.2", "TLSv1.3"}
	case config.TLSProfileOld:
		requiredProtocols = []string{"TLSv1", "TLSv1.1", "TLSv1.2", "TLSv1.3"}
	default:
		requiredProtocols = []string{"TLSv1.2", "TLSv1.3"}
	}

	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true,
		func(context.Context) (bool, error) {
			cm, err := cs.KubeClient.Kube.CoreV1().ConfigMaps(ns).Get(
				context.TODO(), configMapName, metav1.GetOptions{})
			if err != nil {
				log.Printf("Waiting for ConfigMap %s: %v", configMapName, err)
				return false, nil
			}
			for key, value := range cm.Data {
				allFound := true
				for _, proto := range requiredProtocols {
					if !strings.Contains(value, proto) {
						allFound = false
						break
					}
				}
				if allFound {
					log.Printf("ConfigMap %s key %q: ssl_protocols contains required protocols %v",
						configMapName, key, requiredProtocols)
					return true, nil
				}
			}
			log.Printf("Waiting for ConfigMap %s to contain ssl_protocols %v for profile %s",
				configMapName, requiredProtocols, profileType)
			return false, nil
		})
	Expect(err).NotTo(HaveOccurred(),
		"nginx ConfigMap %s/%s does not reflect TLS profile %s (required ssl_protocols: %v)",
		ns, configMapName, profileType, requiredProtocols)
}

// AssertDeploymentEnvVarHasAllSubstrings polls a Deployment until all containers
// carry envName whose value contains every substring in required (e.g. comma-
// separated TLS curve preferences: X25519MLKEM768,X25519,P-256,P-384).
func AssertDeploymentEnvVarHasAllSubstrings(cs *clients.Clients, ns, deploymentName, envName string, required []string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true,
		func(context.Context) (bool, error) {
			deployment, getErr := cs.KubeClient.Kube.AppsV1().Deployments(ns).Get(
				context.TODO(), deploymentName, metav1.GetOptions{})
			if getErr != nil {
				log.Printf("Waiting for deployment %s: %v", deploymentName, getErr)
				return false, nil
			}
			for _, container := range deployment.Spec.Template.Spec.Containers {
				found := false
				for _, env := range container.Env {
					if env.Name != envName {
						continue
					}
					found = true
					for _, sub := range required {
						if !strings.Contains(env.Value, sub) {
							log.Printf("Deployment %s container %s: %s=%q missing %q",
								deploymentName, container.Name, envName, env.Value, sub)
							return false, nil
						}
					}
				}
				if !found {
					log.Printf("Deployment %s container %s: env var %s not yet present",
						deploymentName, container.Name, envName)
					return false, nil
				}
			}
			log.Printf("Deployment %s: confirmed %s contains %v on all containers",
				deploymentName, envName, required)
			return true, nil
		})
	Expect(err).NotTo(HaveOccurred(),
		"deployment %s/%s env %s does not contain all of %v after waiting",
		ns, deploymentName, envName, required)
}

// AssertDeploymentEnvVarDoesNotContain polls a Deployment until all containers
// carry envName and none of those values contain forbidden (e.g. IANA secp256r1
// must not appear after Knative curve-name conversion).
func AssertDeploymentEnvVarDoesNotContain(cs *clients.Clients, ns, deploymentName, envName, forbidden string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true,
		func(context.Context) (bool, error) {
			deployment, getErr := cs.KubeClient.Kube.AppsV1().Deployments(ns).Get(
				context.TODO(), deploymentName, metav1.GetOptions{})
			if getErr != nil {
				log.Printf("Waiting for deployment %s: %v", deploymentName, getErr)
				return false, nil
			}
			for _, container := range deployment.Spec.Template.Spec.Containers {
				found := false
				for _, env := range container.Env {
					if env.Name != envName {
						continue
					}
					found = true
					if strings.Contains(env.Value, forbidden) {
						log.Printf("Deployment %s container %s: %s=%q still contains %q",
							deploymentName, container.Name, envName, env.Value, forbidden)
						return false, nil
					}
				}
				if !found {
					log.Printf("Deployment %s container %s: env var %s not yet present",
						deploymentName, container.Name, envName)
					return false, nil
				}
			}
			log.Printf("Deployment %s: confirmed %s does not contain %q on all containers",
				deploymentName, envName, forbidden)
			return true, nil
		})
	Expect(err).NotTo(HaveOccurred(),
		"deployment %s/%s env %s still contains %q (or is missing) after waiting",
		ns, deploymentName, envName, forbidden)
}

// AssertNginxConfigMapContains polls a ConfigMap until any data value contains
// all of the required substrings (e.g. ssl_ecdh_curve groups).
func AssertNginxConfigMapContains(cs *clients.Clients, ns, configMapName string, required []string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true,
		func(context.Context) (bool, error) {
			cm, getErr := cs.KubeClient.Kube.CoreV1().ConfigMaps(ns).Get(
				context.TODO(), configMapName, metav1.GetOptions{})
			if getErr != nil {
				log.Printf("Waiting for ConfigMap %s: %v", configMapName, getErr)
				return false, nil
			}
			for key, value := range cm.Data {
				allFound := true
				for _, sub := range required {
					if !strings.Contains(value, sub) {
						allFound = false
						break
					}
				}
				if allFound {
					log.Printf("ConfigMap %s key %q: contains required substrings %v",
						configMapName, key, required)
					return true, nil
				}
			}
			log.Printf("Waiting for ConfigMap %s to contain %v", configMapName, required)
			return false, nil
		})
	Expect(err).NotTo(HaveOccurred(),
		"nginx ConfigMap %s/%s does not contain all of %v after waiting",
		ns, configMapName, required)
}

// AssertNginxConfigMapDoesNotContain polls until the ConfigMap contains requiredMarker
// (so the config has been reconciled) and no data value containing that marker also
// contains forbidden (e.g. ssl_ecdh_curve present without secp256r1).
func AssertNginxConfigMapDoesNotContain(cs *clients.Clients, ns, configMapName, requiredMarker, forbidden string) {
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true,
		func(context.Context) (bool, error) {
			cm, getErr := cs.KubeClient.Kube.CoreV1().ConfigMaps(ns).Get(
				context.TODO(), configMapName, metav1.GetOptions{})
			if getErr != nil {
				log.Printf("Waiting for ConfigMap %s: %v", configMapName, getErr)
				return false, nil
			}
			for key, value := range cm.Data {
				if !strings.Contains(value, requiredMarker) {
					continue
				}
				if strings.Contains(value, forbidden) {
					log.Printf("ConfigMap %s key %q: still contains %q", configMapName, key, forbidden)
					return false, nil
				}
				log.Printf("ConfigMap %s key %q: has %q and does not contain %q",
					configMapName, key, requiredMarker, forbidden)
				return true, nil
			}
			log.Printf("Waiting for ConfigMap %s to contain marker %q", configMapName, requiredMarker)
			return false, nil
		})
	Expect(err).NotTo(HaveOccurred(),
		"nginx ConfigMap %s/%s still contains %q (or missing %q) after waiting",
		ns, configMapName, forbidden, requiredMarker)
}
