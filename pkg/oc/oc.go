package oc

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
	"gotest.tools/v3/icmd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Create resources using oc command
func Create(path_dir, namespace string) {
	runWithLog("create", "-f", config.Path(path_dir), "-n", namespace)
}

// CreateRemote creates resources from a remote URL using oc command
func CreateRemote(remote_path, namespace string) {
	runWithLog("create", "-f", remote_path, "-n", namespace)
}

func Apply(path_dir, namespace string) {
	runWithLog("apply", "-f", config.Path(path_dir), "-n", namespace)
}

// Delete resources using oc command
func Delete(path_dir, namespace string) {
	// Tekton Results sets a finalizer that prevent resource removal for some time
	// see parameters "store_deadline" and "forward_buffer"
	// by default, it waits at least 150 seconds
	log.Printf("output: %s\n", runIncreasedTimeout(time.Second*300, "delete", "-f", config.Path(path_dir), "-n", namespace).Stdout())
}

// CreateNewProject creates a new OpenShift project
func CreateNewProject(ns string) {
	runWithLog("new-project", ns)
}

// CreateNewNamespace creates a new Kubernetes namespace
func CreateNewNamespace(ns string) {
	runWithLog("create", "ns", ns)
}

// DeleteProject deletes an OpenShift project
func DeleteProject(ns string) {
	runWithLog("delete", "project", ns)
}

// DeleteProjectIgnoreErrors deletes an OpenShift project, ignoring errors
func DeleteProjectIgnoreErrors(ns string) {
	runWithLog("delete", "project", ns)
}

func LinkSecretToSA(secretname, sa, namespace string) {
	runWithLog("secret", "link", "serviceaccount/"+sa, "secrets/"+secretname, "-n", namespace)
}

func CreateSecretWithSecretToken(secretname, namespace string) {
	runWithLog("create", "secret", "generic", secretname, "--from-literal=secretToken="+config.TriggersSecretToken, "-n", namespace)
}

func EnableTLSConfigForEventlisteners(namespace string) {
	runWithLog("label", "namespace", namespace, "operator.tekton.dev/enable-annotation=enabled")
}

func VerifyKubernetesEventsForEventListener(namespace string) {
	result := run("-n", namespace, "get", "events")
	startedEvent := strings.Contains(result.String(), "dev.tekton.event.triggers.started.v1")
	successfulEvent := strings.Contains(result.String(), "dev.tekton.event.triggers.successful.v1")
	doneEvent := strings.Contains(result.String(), "dev.tekton.event.triggers.done.v1")
	all := startedEvent && successfulEvent && doneEvent
	Expect(all).To(BeTrue(), "No events for successful, done and started")
}

func UpdateTektonConfig(patch_data string) {
	runWithLog("patch", "tektonconfig", "config", "-p", patch_data, "--type=merge")
}

func UpdateTektonConfigwithInvalidData(patch_data, errorMessage string) {
	result := run("patch", "tektonconfig", "config", "-p", patch_data, "--type=merge")
	log.Printf("Output: %s\n", result.Stdout())
	Expect(result.ExitCode).To(Equal(1),
		"Expected exit code 1 but got %d", result.ExitCode)

	Expect(result.Stderr()).To(ContainSubstring(errorMessage),
		"Expected stderr to contain %q but got %q", errorMessage, result.Stderr())
}

func AnnotateNamespace(namespace, annotation string) {
	runWithLog("annotate", "namespace", namespace, annotation)
}

func AnnotateNamespaceIgnoreErrors(namespace, annotation string) {
	runWithLog("annotate", "namespace", namespace, annotation)
}

func RemovePrunerConfig() {
	run("patch", "tektonconfig", "config", "-p", "[{ \"op\": \"remove\", \"path\": \"/spec/pruner\" }]", "--type=json")
}

func LabelNamespace(namespace, label string) {
	runWithLog("label", "namespace", namespace, label)
}

func DeleteResource(resourceType, name string) {
	// Tekton Results sets a finalizer that prevent resource removal for some time
	// see parameters "store_deadline" and "forward_buffer"
	// by default, it waits at least 150 seconds
	log.Printf("output: %s\n", runIncreasedTimeout(time.Second*300, "delete", resourceType, name, "-n", store.Namespace()).Stdout())
}

func DeleteResourceInNamespace(resourceType, name, namespace string) {
	runWithLog("delete", resourceType, name, "-n", namespace)
}

func CheckProjectExists(projectName string) bool {
	commandResult := run("project", projectName)
	return commandResult.ExitCode == 0 && !strings.Contains(commandResult.String(), "error")
}

func SecretExists(secretName, namespace string) bool {
	return !strings.Contains(run("get", "secret", secretName, "-n", namespace).String(), "Error")
}

func CreateSecretForGitResolver(secretData string) {
	run("create", "secret", "generic", "github-auth-secret", "--from-literal", "github-auth-key="+secretData, "-n", "openshift-pipelines")
}

func CreateSecretForWebhook(tokenSecretData, webhookSecretData, namespace string) {
	run("create", "secret", "generic", "gitlab-webhook-config", "--from-literal", "provider.token="+tokenSecretData, "--from-literal", "webhook.secret="+webhookSecretData, "-n", namespace)
}

func EnableConsolePlugin() {
	json_output := run("get", "consoles.operator.openshift.io", "cluster", "-o", "jsonpath={.spec.plugins}").Stdout()
	log.Printf("Already enabled console plugins: %s", json_output)
	var plugins []string

	if len(json_output) > 0 {
		err := json.Unmarshal([]byte(json_output), &plugins)

		if err != nil {
			Fail(fmt.Sprintf("Could not parse consoles.operator.openshift.io CR: %v", err))
		}

		if slices.Contains(plugins, config.ConsolePluginDeployment) {
			log.Printf("Pipelines console plugin is already enabled.")
			return
		}
	}

	plugins = append(plugins, config.ConsolePluginDeployment)

	patch_data := "{\"spec\":{\"plugins\":[\"" + strings.Join(plugins, "\",\"") + "\"]}}"
	run("patch", "consoles.operator.openshift.io", "cluster", "-p", patch_data, "--type=merge").Stdout()
}

func GetSecretsData(secretName, namespace string) string {
	return run("get", "secrets", secretName, "-n", namespace, "-o", "jsonpath=\"{.data}\"").Stdout()
}

func CreateChainsImageRegistrySecret(dockerConfig string) {
	run("create", "secret", "generic", "chains-image-registry-credentials", "--from-literal=.dockerconfigjson="+dockerConfig, "--from-literal=config.json="+dockerConfig, "--type=kubernetes.io/dockerconfigjson")
}

func CopySecret(secretName, sourceNamespace, destNamespace string) {
	secretJson := run("get", "secret", secretName, "-n", sourceNamespace, "-o", "json").Stdout()

	// Process in Go instead of piping through shell to avoid injection
	var secret map[string]any
	Expect(json.Unmarshal([]byte(secretJson), &secret)).To(Succeed(), "failed to parse secret JSON")

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

	cleanedJson, err := json.Marshal(secret)
	Expect(err).NotTo(HaveOccurred(), "failed to marshal cleaned secret")

	tmpFile, err := os.CreateTemp("", "secret-*.json")
	Expect(err).NotTo(HaveOccurred(), "failed to create temp file for secret")
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(cleanedJson)
	Expect(err).NotTo(HaveOccurred(), "failed to write secret to temp file")
	Expect(tmpFile.Close()).To(Succeed())

	run("apply", "-n", destNamespace, "-f", tmpFile.Name())
	log.Printf("Successfully copied secret %s from %s to %s", secretName, sourceNamespace, destNamespace)
}

// ── internal helpers ──────────────────────────────────────────────────────────

func runWithLog(args ...string) {
	log.Printf("output: %s\n", run(args...).Stdout())
}

func run(args ...string) *icmd.Result {
	command := append([]string{"oc"}, args...)
	return cmd.MustSucceed(command...)
}

func runIncreasedTimeout(timeout time.Duration, args ...string) *icmd.Result {
	command := append([]string{"oc"}, args...)
	return cmd.MustSucceedIncreasedTimeout(timeout, command...)
}
