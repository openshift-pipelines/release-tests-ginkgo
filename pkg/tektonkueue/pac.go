package tektonkueue

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	packeys "github.com/openshift-pipelines/pipelines-as-code/pkg/apis/pipelinesascode/keys"
	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
)

const pacCorrelationLabel = "release-tests.openshift.io/pac-correlation"
const pacManager = "pipelinesascode.tekton.dev"

// PACIdentity identifies the exact GitHub event and commit, not the latest run.
type PACIdentity struct {
	Correlation, Owner, Repository, Commit string
	PullRequest                            int
}

// PACPipelineRun returns a complete caller-owned YAML definition (JSON is YAML).
// It shares the parent's producer and adds only the generated Secret mount/check.
func (e *Environment) PACPipelineRun(correlation string) ([]byte, error) {
	if correlation == "" || len(validation.IsValidLabelValue(correlation)) != 0 {
		return nil, fmt.Errorf("invalid PAC correlation")
	}
	run := e.producerRun()
	run.Name = ""
	run.GenerateName = e.Prefix + "-pac-"
	run.Labels[pacCorrelationLabel] = correlation
	run.Annotations = map[string]string{packeys.OnEvent: `[pull_request]`, packeys.OnTargetBranch: `[main]`}
	task := run.Spec.PipelineSpec.Tasks[0].TaskSpec
	task.Volumes = []corev1.Volume{{Name: "pac-auth", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "{{ git_auth_secret }}"}}}}
	step := &task.Steps[0]
	step.VolumeMounts = []corev1.VolumeMount{{Name: "pac-auth", MountPath: "/pac-auth", ReadOnly: true}}
	step.Script = strings.Replace(producerScript, "set -eu\n", "set -eu\ntest -s /pac-auth/username\ntest -s /pac-auth/password\n", 1)
	return json.Marshal(run)
}

// ArmPAC registers parent failure cleanup, authenticates the console, arms run
// histories and snapshots/subscribes to Secrets before any external event.
func (e *Environment) ArmPAC(ctx context.Context, correlation string) (*ArmedExecution, error) {
	if !e.pacSecretSync {
		return nil, fmt.Errorf("EnablePACSecretSync is required before ArmPAC")
	}
	if correlation == "" || len(validation.IsValidLabelValue(correlation)) != 0 {
		return nil, fmt.Errorf("invalid PAC correlation")
	}
	armed, err := e.Arm(ctx)
	if err != nil {
		return nil, err
	}
	armed.pac = &pacExecution{correlation: correlation, queue: e.Prefix, namespace: e.Namespace, baseline: map[string]map[string]bool{}, secrets: map[string]map[string]*pacSecret{}}
	armed.watches.pac = armed.pac
	if err := armed.armSecrets(); err != nil {
		return nil, err
	}
	return armed, nil
}

// ObservePAC correlates a genuine PAC-created run and delegates its immutable UID
// to the parent's two-gate execution/status/proxy/cleanup observation.
func (s *ArmedExecution) ObservePAC(expected PACIdentity) (*Result, error) {
	if s.pac == nil || expected.Correlation != s.pac.correlation || expected.Owner == "" || expected.Repository == "" || expected.Commit == "" || expected.PullRequest <= 0 {
		return nil, fmt.Errorf("invalid armed PAC event identity")
	}
	s.pac.expected = &expected
	var found *pipelinev1.PipelineRun
	err := s.watches.wait(s.ctx, func() (bool, error) {
		var err error
		found, err = s.findPACRun()
		return found != nil, err
	})
	if err != nil {
		return nil, err
	}
	result, err := s.Observe(found)
	if err == nil {
		s.pac.completed = true
	}
	return result, err
}

func (s *ArmedExecution) findPACRun() (*pipelinev1.PipelineRun, error) {
	var found *pipelinev1.PipelineRun
	for _, event := range s.watches.events {
		if event.cluster != s.environment.Hub.Name || event.gvr != pipelineRunGVR || event.object.GetLabels()[pacCorrelationLabel] != s.pac.correlation {
			continue
		}
		var run pipelinev1.PipelineRun
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(event.object.Object, &run); err != nil {
			return nil, fmt.Errorf("invalid correlated PAC run")
		}
		if !s.owns(run.Labels) || s.pac.matches(&run, s.environment.Prefix) != nil {
			return nil, fmt.Errorf("correlated PAC run has the wrong event identity or admission fields")
		}
		if found != nil && (found.UID != run.UID || found.Name != run.Name) {
			return nil, fmt.Errorf("multiple or replaced correlated PAC runs")
		}
		if event.kind == "DELETED" {
			return nil, fmt.Errorf("correlated PAC run disappeared before binding")
		}
		found = &run
	}
	return found, nil
}

func (p *pacExecution) matches(run *pipelinev1.PipelineRun, queue string) error {
	e := p.expected
	if e == nil {
		return fmt.Errorf("PAC event has not been identified")
	}
	if run.Labels[pacCorrelationLabel] != p.correlation || run.Labels["app.kubernetes.io/managed-by"] != pacManager || run.Labels["app.kubernetes.io/instance"] != queue {
		return fmt.Errorf("PAC ownership labels changed")
	}
	annotations := map[string]string{
		packeys.EventType: "pull_request", packeys.GitProvider: "github", packeys.URLOrg: e.Owner,
		packeys.URLRepository: e.Repository, packeys.Repository: e.Repository,
		packeys.RepoURL: "https://github.com/" + e.Owner + "/" + e.Repository, packeys.SHA: e.Commit,
		packeys.PullRequest: strconv.Itoa(e.PullRequest),
	}
	for key, value := range annotations {
		if run.Annotations[key] != value {
			return fmt.Errorf("PAC event annotation mismatch at %s", key)
		}
	}
	if run.Spec.ManagedBy == nil || *run.Spec.ManagedBy != multiKueueController || run.Labels[queueLabel] != queue {
		return fmt.Errorf("PAC run lacks MultiKueue admission fields")
	}
	if run.Annotations[packeys.GitAuthSecret] == "" {
		return fmt.Errorf("PAC run lacks generated Secret annotation")
	}
	if len(validation.IsDNS1123Subdomain(run.Annotations[packeys.GitAuthSecret])) != 0 {
		return fmt.Errorf("PAC generated Secret annotation is invalid")
	}
	resolved := false
	if run.Spec.PipelineSpec != nil && len(run.Spec.PipelineSpec.Tasks) == 1 && run.Spec.PipelineSpec.Tasks[0].TaskSpec != nil {
		for _, volume := range run.Spec.PipelineSpec.Tasks[0].TaskSpec.Volumes {
			if volume.Name == "pac-auth" && volume.Secret != nil && volume.Secret.SecretName == run.Annotations[packeys.GitAuthSecret] {
				resolved = true
			}
		}
	}
	if !resolved {
		return fmt.Errorf("PAC Secret volume does not match its admitted annotation")
	}
	return nil
}

func (s *ArmedExecution) owns(labels map[string]string) bool {
	if s.pac == nil {
		return s.environment.owns(labels)
	}
	return labels["app.kubernetes.io/managed-by"] == pacManager && labels["app.kubernetes.io/instance"] == s.environment.Prefix && labels[pacCorrelationLabel] == s.pac.correlation
}
