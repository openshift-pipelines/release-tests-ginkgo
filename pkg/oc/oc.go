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

type OC struct {
	Context string
}

// Create resources using oc command
func (o *OC) CreateResource(path, namespace string) {
	o.runWithLog("create", "-f", path, "-n", namespace)
}

func (o *OC) Create(path_dir, namespace string) {
	o.runWithLog("create", "-f", config.Path(path_dir), "-n", namespace)
}

// Create resources using remote path using oc command
func (o *OC) CreateRemote(remote_path, namespace string) {
	o.runWithLog("create", "-f", remote_path, "-n", namespace)
}

// Apply applies resources using oc command.
// If namespace is not provided, it uses store.Namespace() (set by hooks).
// Usage:
//   oc.Apply("testdata/foo.yaml")              // uses store.Namespace()
//   oc.Apply("testdata/foo.yaml", "my-ns")     // uses explicit namespace
func (o *OC) Apply(path_dir string, namespace ...string) {
	var ns string
	if len(namespace) > 0 {
		ns = namespace[0]
	} else {
		ns = store.Namespace()
		if ns == "" {
			panic("oc.Apply: namespace not provided and store.Namespace() is empty - ensure hooks are configured or pass namespace explicitly")
		}
	}
	o.runWithLog("apply", "-f", config.Path(path_dir), "-n", ns)
}

// Delete resources using oc command
func (o *OC) Delete(path_dir, namespace string) {
	// Tekton Results sets a finalizer that prevent resource removal for some time
	// see parameters "store_deadline" and "forward_buffer"
	// by default, it waits at least 150 seconds
	log.Printf("output: %s\n", o.runIncreasedTimeout(time.Second*300, "delete", "-f", config.Path(path_dir), "-n", namespace).Stdout())
}

// CreateNewProject Helps you to create new project
func (o *OC) CreateNewProject(ns string) {
	o.runWithLog("new-project", ns)
}

// CreateNewProject Helps you to create new project
func (o *OC) CreateNewNamespace(ns string) {
	o.runWithLog("create", "ns", ns)
}

// DeleteProject Helps you to delete new project
func (o *OC) DeleteProject(ns string) {
	o.runWithLog("delete", "project", ns)
}

func (o *OC) DeleteProjectIgnoreErrors(ns string) {
	o.runWithLog("delete", "project", ns)
}

func (o *OC) LinkSecretToSA(secretname, sa, namespace string) {
	o.runWithLog("secret", "link", "serviceaccount/"+sa, "secrets/"+secretname, "-n", namespace)
}

func (o *OC) CreateSecretWithSecretToken(secretname, namespace string) {
	o.runWithLog("create", "secret", "generic", secretname, "--from-literal=secretToken="+config.TriggersSecretToken, "-n", namespace)
}

func (o *OC) EnableTLSConfigForEventlisteners(namespace string) {
	o.runWithLog("label", "namespace", namespace, "operator.tekton.dev/enable-annotation=enabled")
}

func (o *OC) VerifyKubernetesEventsForEventListener(namespace string) {
	result := o.run("-n", namespace, "get", "events")
	startedEvent := strings.Contains(result.String(), "dev.tekton.event.triggers.started.v1")
	successfulEvent := strings.Contains(result.String(), "dev.tekton.event.triggers.successful.v1")
	doneEvent := strings.Contains(result.String(), "dev.tekton.event.triggers.done.v1")
	all := startedEvent && successfulEvent && doneEvent
	Expect(all).To(BeTrue(), "No events for successful, done and started")
}

func (o *OC) UpdateTektonConfig(patch_data string) {
	o.runWithLog("patch", "tektonconfig", "config", "-p", patch_data, "--type=merge")
}

func (o *OC) UpdateTektonConfigwithInvalidData(patch_data, errorMessage string) {
	result := o.run("patch", "tektonconfig", "config", "-p", patch_data, "--type=merge")
	log.Printf("Output: %s\n", result.Stdout())
	Expect(result.ExitCode).To(Equal(1),
		"Expected exit code 1 but got %d", result.ExitCode)

	Expect(result.Stderr()).To(ContainSubstring(errorMessage),
		"Expected stderr to contain %q but got %q", errorMessage, result.Stderr())
}

func (o *OC) AnnotateNamespace(namespace, annotation string) {
	o.runWithLog("annotate", "namespace", namespace, annotation)
}

func (o *OC) AnnotateNamespaceIgnoreErrors(namespace, annotation string) {
	o.runWithLog("annotate", "namespace", namespace, annotation)
}

func (o *OC) RemovePrunerConfig() {
	o.run("patch", "tektonconfig", "config", "-p", "[{ \"op\": \"remove\", \"path\": \"/spec/pruner\" }]", "--type=json")
}

func (o *OC) LabelNamespace(namespace, label string) {
	o.runWithLog("label", "namespace", namespace, label)
}

func (o *OC) DeleteResource(resourceType, name string) {
	// Tekton Results sets a finalizer that prevent resource removal for some time
	// see parameters "store_deadline" and "forward_buffer"
	// by default, it waits at least 150 seconds
	log.Printf("output: %s\n", o.runIncreasedTimeout(time.Second*300, "delete", resourceType, name, "-n", store.Namespace()).Stdout())
}

func (o *OC) DeleteResourceInNamespace(resourceType, name, namespace string) {
	o.runWithLog("delete", resourceType, name, "-n", namespace)
}

func (o *OC) CheckProjectExists(projectName string) bool {
	commandResult := o.run("project", projectName)
	return commandResult.ExitCode == 0 && !strings.Contains(commandResult.String(), "error")
}

func (o *OC) SecretExists(secretName string, namespace string) bool {
	return !strings.Contains(o.run("get", "secret", secretName, "-n", namespace).String(), "Error")
}

func (o *OC) CreateSecretForGitResolver(secretData string) {
	o.run("create", "secret", "generic", "github-auth-secret", "--from-literal", "github-auth-key="+secretData, "-n", "openshift-pipelines")
}

func (o *OC) CreateSecretForWebhook(tokenSecretData, webhookSecretData, namespace string) {
	o.run("create", "secret", "generic", "gitlab-webhook-config", "--from-literal", "provider.token="+tokenSecretData, "--from-literal", "webhook.secret="+webhookSecretData, "-n", namespace)
}

func (o *OC) EnableConsolePlugin() {
	json_output := o.run("get", "consoles.operator.openshift.io", "cluster", "-o", "jsonpath={.spec.plugins}").Stdout()
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
	o.run("patch", "consoles.operator.openshift.io", "cluster", "-p", patch_data, "--type=merge").Stdout()
}

func (o *OC) GetSecretsData(secretName, namespace string) string {
	return o.run("get", "secrets", secretName, "-n", namespace, "-o", "jsonpath=\"{.data}\"").Stdout()
}

func (o *OC) CreateChainsImageRegistrySecret(dockerConfig string) {
	o.run("create", "secret", "generic", "chains-image-registry-credentials", "--from-literal=.dockerconfigjson="+dockerConfig, "--from-literal=config.json="+dockerConfig, "--type=kubernetes.io/dockerconfigjson")
}

func (o *OC) CopySecret(secretName string, sourceNamespace string, destNamespace string) {
	secretJson := o.run("get", "secret", secretName, "-n", sourceNamespace, "-o", "json").Stdout()

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

	o.run("apply", "-n", destNamespace, "-f", tmpFile.Name())
	log.Printf("Successfully copied secret %s from %s to %s", secretName, sourceNamespace, destNamespace)
}

func (o *OC) runWithLog(args ...string) {
	log.Printf("output: %s\n", o.run(args...).Stdout())
}

func (o *OC) run(args ...string) *icmd.Result {
	command := o.getOcCommand(args)
	succeed := cmd.MustSucceed(command...)
	return succeed
}

func (o *OC) getOcCommand(args []string) []string {
	command := []string{"oc"}
	if o.Context != "" {
		command = append(command, "--context", o.Context)
	}
	command = append(command, args...)
	return command
}
func (o *OC) runIncreasedTimeout(timeout time.Duration, args ...string) *icmd.Result {
	command := o.getOcCommand(args)
	return cmd.MustSucceedIncreasedTimeout(timeout, command...)
}
