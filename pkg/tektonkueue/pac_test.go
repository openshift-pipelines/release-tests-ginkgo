package tektonkueue

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	packeys "github.com/openshift-pipelines/pipelines-as-code/pkg/apis/pipelinesascode/keys"
	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

var pacEvent = PACIdentity{Correlation: "unique-correlation", Owner: "owner", Repository: "repo", Commit: "exact-head-sha", PullRequest: 7}

const generatedSecret = "pac-generated-auth"
const payloadSentinel = "SENTINEL-must-never-appear-in-an-error"

func validPACRun(t *testing.T, e *Environment) *pipelinev1.PipelineRun {
	t.Helper()
	data, err := e.PACPipelineRun(pacEvent.Correlation)
	if err != nil {
		t.Fatal(err)
	}
	var run pipelinev1.PipelineRun
	if err := json.Unmarshal(data, &run); err != nil {
		t.Fatal(err)
	}
	run.Name, run.GenerateName, run.UID = wireRun.name, "", "hub-uid"
	run.Labels["app.kubernetes.io/managed-by"] = pacManager
	for key, value := range map[string]string{packeys.EventType: "pull_request", packeys.GitProvider: "github", packeys.URLOrg: "owner",
		packeys.URLRepository: "repo", packeys.Repository: "repo", packeys.RepoURL: "https://github.com/owner/repo", packeys.SHA: "exact-head-sha",
		packeys.PullRequest: "7", packeys.GitAuthSecret: generatedSecret} {
		run.Annotations[key] = value
	}
	run.Spec.PipelineSpec.Tasks[0].TaskSpec.Volumes[0].Secret.SecretName = generatedSecret
	return &run
}

func preparePACFixture(t *testing.T, proxy *consoleProxy) (*executionFixture, *ArmedExecution) {
	t.Helper()
	f := executionEnvironment(t)
	if proxy == nil {
		proxy = proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) })
	}
	armed := armFixture(t, f, proxy)
	armed.pac = &pacExecution{correlation: pacEvent.Correlation, queue: "test", namespace: wireRun.namespace,
		baseline: map[string]map[string]bool{}, secrets: map[string]map[string]*pacSecret{}}
	armed.watches.pac = armed.pac
	if err := armed.armSecrets(); err != nil {
		t.Fatal(err)
	}
	*f.run = *validPACRun(t, f.environment)
	f.start() // fake PAC admission occurs only after all parent/Secret watches exist
	workloads := f.dynamic["hub"].Resource(workloadGVR).Namespace(wireRun.namespace)
	workload, err := workloads.Get(armed.ctx, wireRun.workload, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	workload.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Admitted", "status": "True"}},
		"admission": map[string]any{"clusterQueue": "test"}}
	if _, err := workloads.Update(armed.ctx, workload, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	return f, armed
}

func generatedPACSecret(cluster string) *corev1.Secret {
	uid := types.UID("hub-uid")
	if cluster != "hub" {
		uid = wireRun.runUID
	}
	return &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: generatedSecret, Namespace: wireRun.namespace, UID: types.UID(cluster + "-auth-uid"),
			OwnerReferences: []metav1.OwnerReference{{Kind: "PipelineRun", Name: wireRun.name, UID: uid}}},
		Type: corev1.SecretTypeBasicAuth, Data: map[string][]byte{"username": []byte("user"), "password": []byte(payloadSentinel), "extra": []byte("preserve-all-keys")},
	}
}

func publishSecret(t *testing.T, f *executionFixture, cluster string, secret *corev1.Secret) {
	t.Helper()
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.dynamic[cluster].Resource(secretGVR).Namespace(wireRun.namespace).Create(t.Context(), &unstructured.Unstructured{Object: object}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func bindPACFixture(t *testing.T, armed *ArmedExecution, run *pipelinev1.PipelineRun) {
	t.Helper()
	expected := pacEvent
	armed.pac.expected = &expected
	if err := armed.watches.bind(run); err != nil {
		t.Fatal(err)
	}
	if err := armed.watches.drain(armed.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPACFixtureReusesExactProducerAndGeneratedSecretMount(t *testing.T) {
	e := &Environment{Prefix: "test", Namespace: wireRun.namespace}
	data, err := e.PACPipelineRun("unique")
	if err != nil {
		t.Fatal(err)
	}
	var run pipelinev1.PipelineRun
	if err := json.Unmarshal(data, &run); err != nil {
		t.Fatal(err)
	}
	task := run.Spec.PipelineSpec.Tasks[0].TaskSpec
	if run.Name != "" || run.GenerateName == "" || run.Labels[pacCorrelationLabel] != "unique" || run.Labels[queueLabel] != "test" ||
		run.Annotations[packeys.OnEvent] != "[pull_request]" || run.Annotations[packeys.OnTargetBranch] != "[main]" {
		t.Fatal("fixture lacks PAC match/correlation/queue fields")
	}
	if task.Volumes[0].Secret.SecretName != "{{ git_auth_secret }}" || !task.Steps[0].VolumeMounts[0].ReadOnly || task.Steps[0].VolumeMounts[0].MountPath != "/pac-auth" {
		t.Fatal("fixture does not consume the PAC generated Secret")
	}
	checks := "test -s /pac-auth/username\ntest -s /pac-auth/password\n"
	if !strings.Contains(task.Steps[0].Script, checks) || strings.Replace(task.Steps[0].Script, checks, "", 1) != producerScript {
		t.Fatal("fixture changed the causal producer protocol")
	}
}

func TestPACCorrelationRejectsWrongEventRepositoryCommitAdmissionAndAnnotation(t *testing.T) {
	for _, field := range []string{packeys.EventType, packeys.Repository, packeys.RepoURL, packeys.SHA, packeys.PullRequest, packeys.GitAuthSecret, "managedBy", "queue"} {
		t.Run(field, func(t *testing.T) {
			f, armed := preparePACFixture(t, nil)
			run := f.run.DeepCopy()
			switch field {
			case "managedBy":
				run.Spec.ManagedBy = nil
			case "queue":
				run.Labels[queueLabel] = "other"
			default:
				run.Annotations[field] = ""
			}
			p := armed.pac
			p.expected = &pacEvent
			if err := p.matches(run, "test"); err == nil {
				t.Fatal("accepted wrong PAC event or missing admission/Secret field")
			}
		})
	}
}

func TestPACNoMatchingRunAndCancellationKeepContextIdentity(t *testing.T) {
	f := executionEnvironment(t)
	armed := armFixture(t, f, proxyWire(t, func(w http.ResponseWriter, r *http.Request) { proxyJSON(t, w, r) }))
	armed.pac = &pacExecution{correlation: pacEvent.Correlation}
	armed.cancel()
	_, err := armed.ObservePAC(pacEvent)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("missing/canceled correlation = %v", err)
	}
	if strings.Contains(err.Error(), payloadSentinel) {
		t.Fatal("cancellation leaked payload")
	}
}
