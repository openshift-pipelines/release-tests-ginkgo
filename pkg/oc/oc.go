// Package oc provides wrappers around the oc/kubectl CLI for use in integration tests.
package oc

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"gotest.tools/v3/icmd"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega
)

// OC struct holds  params which can be used to customize the oc commands.
type OC struct {
	Context string
}

// Create creates resources from a local file using oc command.
// If namespace is not provided, it uses store.Namespace() (set by hooks).
// Usage:
//
//	oc.Create("testdata/foo.yaml")              // uses store.Namespace()
//	oc.Create("testdata/foo.yaml", "my-ns")     // uses explicit namespace
func (oc *OC) Create(pathDir string, namespace ...string) {
	var ns string
	if len(namespace) > 0 {
		ns = namespace[0]
	} else {
		ns = store.Namespace()
		if ns == "" {
			panic("oc.Create: namespace not provided and store.Namespace() is empty - ensure hooks are configured or pass namespace explicitly")
		}
	}
	oc.runWithLog("create", "-f", config.Path(pathDir), "-n", ns)
}

// CreateRemote creates resources from a remote URL using oc command.
// If namespace is not provided, it uses store.Namespace() (set by hooks).
func (oc *OC) CreateRemote(remotePath string, namespace ...string) {
	var ns string
	if len(namespace) > 0 {
		ns = namespace[0]
	} else {
		ns = store.Namespace()
		if ns == "" {
			panic("oc.CreateRemote: namespace not provided and store.Namespace() is empty - ensure hooks are configured or pass namespace explicitly")
		}
	}
	oc.runWithLog("create", "-f", remotePath, "-n", ns)
}

// Apply applies resources using oc command.
// If namespace is not provided, it uses store.Namespace() (set by hooks).
// Usage:
//
//	oc.Apply("testdata/foo.yaml")              // uses store.Namespace()
//	oc.Apply("testdata/foo.yaml", "my-ns")     // uses explicit namespace
func (oc *OC) Apply(pathDir string, namespace ...string) {
	var ns string
	if len(namespace) > 0 {
		ns = namespace[0]
	} else {
		ns = store.Namespace()
		if ns == "" {
			panic("oc.Apply: namespace not provided and store.Namespace() is empty - ensure hooks are configured or pass namespace explicitly")
		}
	}
	oc.runWithLog("apply", "-f", config.Path(pathDir), "-n", ns)
}

// Delete deletes resources from a local file using oc command.
func (oc *OC) Delete(pathDir, namespace string) {
	// Tekton Results sets a finalizer that prevent resource removal for some time
	// see parameters "store_deadline" and "forward_buffer"
	// by default, it waits at least 150 seconds
	log.Printf("output: %s\n", oc.runIncreasedTimeout(time.Second*300, "delete", "-f", config.Path(pathDir), "-n", namespace).Stdout())
}

// CreateNewProject creates a new OpenShift project
func (oc *OC) CreateNewProject(ns string) {
	// oc.runWithLog("new-project", ns)
	// Run with log was too chatty hence following the below approach
	oc.run("new-project", ns)
	log.Printf("Created project %q\n", ns)
}

// CreateNewProjectIgnoreErrors creates a new OpenShift project, ignoring errors (e.g., if it already exists)
func (oc *OC) CreateNewProjectIgnoreErrors(ns string) {
	result := oc.runIgnoreErrors("new-project", ns)
	if result.ExitCode == 0 {
		log.Printf("output: %s\n", result.Stdout())
	} else {
		log.Printf("output: %s\n", result.Combined())
	}
}

// CreateNewNamespace creates a new Kubernetes namespace
func (oc *OC) CreateNewNamespace(ns string) {
	oc.runWithLog("create", "ns", ns)
}

// DeleteProject deletes an OpenShift project
func (oc *OC) DeleteProject(ns string) {
	oc.runWithLog("delete", "project", ns)
}

// DeleteProjectIgnoreErrors deletes an OpenShift project, ignoring errors (e.g., if it doesn't exist)
func (oc *OC) DeleteProjectIgnoreErrors(ns string) {
	result := oc.runIgnoreErrors("delete", "project", ns)
	if result.ExitCode == 0 {
		log.Printf("output: %s\n", result.Stdout())
	} else {
		log.Printf("output (non-zero exit %d): %s\n", result.ExitCode, result.Combined())
	}
}

// LinkSecretToSA links a secret to a service account in the given namespace.
func (oc *OC) LinkSecretToSA(secretname, sa, namespace string) {
	oc.runWithLog("secret", "link", "serviceaccount/"+sa, "secrets/"+secretname, "-n", namespace)
}

// CreateSecretWithSecretToken creates a generic secret containing the triggers secret token.
func (oc *OC) CreateSecretWithSecretToken(secretname, namespace string) {
	oc.runWithLog("create", "secret", "generic", secretname, "--from-literal=secretToken="+config.TriggersSecretToken, "-n", namespace)
}

// EnableTLSConfigForEventlisteners labels the namespace to enable TLS for EventListeners.
func (oc *OC) EnableTLSConfigForEventlisteners(namespace string) {
	oc.runWithLog("label", "namespace", namespace, "operator.tekton.dev/enable-annotation=enabled")
}

// VerifyKubernetesEventsForEventListener asserts that the expected Tekton trigger events exist in the namespace.
func (oc *OC) VerifyKubernetesEventsForEventListener(namespace string) {
	result := oc.run("-n", namespace, "get", "events")
	startedEvent := strings.Contains(result.String(), "dev.tekton.event.triggers.started.v1")
	successfulEvent := strings.Contains(result.String(), "dev.tekton.event.triggers.successful.v1")
	doneEvent := strings.Contains(result.String(), "dev.tekton.event.triggers.done.v1")
	all := startedEvent && successfulEvent && doneEvent
	Expect(all).To(BeTrue(), "No events for successful, done and started")
}

// UpdateTektonConfig patches the TektonConfig CR with the provided JSON patch data.
func (oc *OC) UpdateTektonConfig(patchData string) {
	oc.runWithLog("patch", "tektonconfig", "config", "-p", patchData, "--type=merge")
}

// UpdateTektonConfigwithInvalidData patches TektonConfig with invalid data and asserts the expected error message.
func (oc *OC) UpdateTektonConfigwithInvalidData(patchData, errorMessage string) {
	result := oc.run("patch", "tektonconfig", "config", "-p", patchData, "--type=merge")
	log.Printf("Output: %s\n", result.Stdout())
	Expect(result.ExitCode).To(Equal(1),
		"Expected exit code 1 but got %d", result.ExitCode)

	Expect(result.Stderr()).To(ContainSubstring(errorMessage),
		"Expected stderr to contain %q but got %q", errorMessage, result.Stderr())
}

// AnnotateNamespace annotates the given namespace with the provided annotation.
func (oc *OC) AnnotateNamespace(namespace, annotation string) {
	oc.runWithLog("annotate", "namespace", namespace, annotation)
}

// AnnotateNamespaceIgnoreErrors annotates the given namespace, ignoring any errors.
func (oc *OC) AnnotateNamespaceIgnoreErrors(namespace, annotation string) {
	oc.runWithLog("annotate", "namespace", namespace, annotation)
}

// RemovePrunerConfig removes the pruner spec from TektonConfig.
func (oc *OC) RemovePrunerConfig() {
	oc.run("patch", "tektonconfig", "config", "-p", "[{ \"op\": \"remove\", \"path\": \"/spec/pruner\" }]", "--type=json")
}

// LabelNamespace adds a label to the given namespace.
func (oc *OC) LabelNamespace(namespace, label string) {
	oc.runWithLog("label", "namespace", namespace, label)
}

// DeleteResource deletes a resource by type and name from the current namespace.
func (oc *OC) DeleteResource(resourceType, name string) {
	// Tekton Results sets a finalizer that prevent resource removal for some time
	// see parameters "store_deadline" and "forward_buffer"
	// by default, it waits at least 150 seconds
	log.Printf("output: %s\n", oc.runIncreasedTimeout(time.Second*300, "delete", resourceType, name, "-n", store.Namespace()).Stdout())
}

// DeleteResourceInNamespace deletes a resource by type and name from the given namespace.
func (oc *OC) DeleteResourceInNamespace(resourceType, name, namespace string) {
	oc.runWithLog("delete", resourceType, name, "-n", namespace)
}

// CheckProjectExists returns true if the given OpenShift project exists.
func (oc *OC) CheckProjectExists(projectName string) bool {
	commandResult := oc.run("project", projectName)
	return commandResult.ExitCode == 0 && !strings.Contains(commandResult.String(), "error")
}

// SecretExists returns true if the named secret exists in the given namespace.
func (oc *OC) SecretExists(secretName, namespace string) bool {
	return !strings.Contains(oc.run("get", "secret", secretName, "-n", namespace).String(), "Error")
}

// CreateSecretForGitResolver creates the github-auth-secret used by the git resolver.
func (oc *OC) CreateSecretForGitResolver(secretData string) {
	oc.run("create", "secret", "generic", "github-auth-secret", "--from-literal", "github-auth-key="+secretData, "-n", "openshift-pipelines")
}

// CreateSecretForWebhook creates the gitlab-webhook-config secret in the given namespace.
func (oc *OC) CreateSecretForWebhook(tokenSecretData, webhookSecretData, namespace string) {
	oc.run("create", "secret", "generic", "gitlab-webhook-config", "--from-literal", "provider.token="+tokenSecretData, "--from-literal", "webhook.secret="+webhookSecretData, "-n", namespace)
}

// EnableConsolePlugin enables the Pipelines console plugin in the cluster console.
func (oc *OC) EnableConsolePlugin() {
	jsonOutput := oc.run("get", "consoles.operator.openshift.io", "cluster", "-o", "jsonpath={.spec.plugins}").Stdout()
	log.Printf("Already enabled console plugins: %s", jsonOutput)
	var plugins = make([]string, 0, 1)
	if len(jsonOutput) > 0 {
		err := json.Unmarshal([]byte(jsonOutput), &plugins)

		if err != nil {
			Fail(fmt.Sprintf("Could not parse consoles.operator.openshift.io CR: %v", err))
		}

		if slices.Contains(plugins, config.ConsolePluginDeployment) {
			log.Printf("Pipelines console plugin is already enabled.")
			return
		}
	}

	plugins = append(plugins, config.ConsolePluginDeployment)

	patchData := "{\"spec\":{\"plugins\":[\"" + strings.Join(plugins, "\",\"") + "\"]}}"
	oc.run("patch", "consoles.operator.openshift.io", "cluster", "-p", patchData, "--type=merge").Stdout()
}

// GetSecretsData returns the data field of the named secret in the given namespace.
func (oc *OC) GetSecretsData(secretName, namespace string) string {
	return oc.run("get", "secrets", secretName, "-n", namespace, "-o", "jsonpath=\"{.data}\"").Stdout()
}

// CreateChainsImageRegistrySecret creates the chains image registry credentials secret.
func (oc *OC) CreateChainsImageRegistrySecret(dockerConfig string) {
	ns := store.Namespace()
	if ns == "" {
		panic("CreateChainsImageRegistrySecret: store.Namespace() is empty - ensure hooks are configured")
	}
	oc.run("create", "secret", "generic", "chains-image-registry-credentials", "--from-literal=.dockerconfigjson="+dockerConfig, "--from-literal=config.json="+dockerConfig, "--type=kubernetes.io/dockerconfigjson", "-n", ns)
}

// ValidateAndCreateJibMavenSecret validates required environment variables and creates
// the jib-maven registry credentials secret, then links it to the pipeline service account.
// Skips the test if required environment variables are not set.
func (oc *OC) ValidateAndCreateJibMavenSecret(namespace string) {
	repo := os.Getenv("JIB_MAVEN_REPOSITORY")
	if repo == "" {
		Skip("JIB_MAVEN_REPOSITORY not set -- skipping jib-maven test")
	}

	dockerConfig := os.Getenv("JIB_MAVEN_DOCKER_CONFIG_JSON")
	if dockerConfig == "" {
		Skip("JIB_MAVEN_DOCKER_CONFIG_JSON not set -- skipping jib-maven test")
	}

	// Create secret with docker config
	oc.run("create", "secret", "generic", "jib-maven-image-registry-credentials",
		"--from-literal=.dockerconfigjson="+dockerConfig,
		"--from-literal=config.json="+dockerConfig,
		"--type=kubernetes.io/dockerconfigjson",
		"-n", namespace)

	// Link secret to pipeline service account
	oc.LinkSecretToSA("jib-maven-image-registry-credentials", "pipeline", namespace)
}

// CopySecret copies a secret from one namespace to another, transforming metadata and data keys.
func (oc *OC) CopySecret(secretName, sourceNamespace, destNamespace string) {
	secretJSON := oc.run("get", "secret", secretName, "-n", sourceNamespace, "-o", "json").Stdout()

	// Process in Go instead of piping through shell to avoid injection
	var secret map[string]any
	Expect(json.Unmarshal([]byte(secretJSON), &secret)).To(Succeed(), "failed to parse secret JSON")

	// Remove metadata fields
	if meta, ok := secret["metadata"].(map[string]any); ok {
		for _, key := range []string{"namespace", "creationTimestamp", "resourceVersion", "selfLink", "uid", "annotations"} {
			delete(meta, key)
		}
	}

	// Rename "github-auth-key" to "token" in data
	if data, ok := secret["data"].(map[string]any); ok {
		if val, exists := data["github-auth-key"]; exists {
			data["token"] = val
			delete(data, "github-auth-key")
		}
	}

	cleanedJSON, err := json.Marshal(secret)
	Expect(err).NotTo(HaveOccurred(), "failed to marshal cleaned secret")

	tmpFile, err := os.CreateTemp("", "secret-*.json")
	Expect(err).NotTo(HaveOccurred(), "failed to create temp file for secret")
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	_, err = tmpFile.Write(cleanedJSON)
	Expect(err).NotTo(HaveOccurred(), "failed to write secret to temp file")
	Expect(tmpFile.Close()).To(Succeed())

	oc.run("apply", "-n", destNamespace, "-f", tmpFile.Name())
	log.Printf("Successfully copied secret %s from %s to %s", secretName, sourceNamespace, destNamespace)
}

// ── internal helpers ──────────────────────────────────────────────────────────

func (oc *OC) runWithLog(args ...string) {
	log.Printf("output: %s\n", oc.run(args...).Stdout())
}

func (oc *OC) run(args ...string) *icmd.Result {
	command := oc.getOcCommand(args)
	return cmd.MustSucceed(command...)
}

func (oc *OC) runIgnoreErrors(args ...string) *icmd.Result {
	command := oc.getOcCommand(args)
	return cmd.Run(command...)
}
func (oc *OC) runIncreasedTimeout(timeout time.Duration, args ...string) *icmd.Result {
	command := oc.getOcCommand(args)
	return cmd.MustSucceedIncreasedTimeout(timeout, command...)
}

func (oc *OC) getOcCommand(args []string) []string {
	command := []string{"oc"}
	if oc.Context != "" {
		command = append(command, "--context", oc.Context)
	}
	command = append(command, args...)
	return command
}
