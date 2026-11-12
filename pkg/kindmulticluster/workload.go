package kindmulticluster

import (
	"fmt"
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	namespace  = "release-multicluster"
	queueLabel = "kueue.x-k8s.io/queue-name"
	managedBy  = "kueue.x-k8s.io/multikueue"
	readyLine  = "waiting-for-admission-proof"
	container  = "step-prove-execution"
)

func pipelineRun(name string, prelabelled bool) (*pipelinev1.PipelineRun, string) {
	queue := namespace
	labels := map[string]string{}
	if prelabelled {
		queue += "-explicit"
		labels[queueLabel] = queue
	}
	timeout := metav1.Duration{Duration: 10 * time.Minute}
	return &pipelinev1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: pipelinev1.PipelineRunSpec{
			Timeouts:        &pipelinev1.TimeoutFields{Pipeline: &timeout},
			TaskRunTemplate: pipelinev1.PipelineTaskRunTemplate{ServiceAccountName: "default"},
			PipelineSpec: &pipelinev1.PipelineSpec{Tasks: []pipelinev1.PipelineTask{{
				Name: "execute-on-spoke",
				TaskSpec: &pipelinev1.EmbeddedTask{TaskSpec: pipelinev1.TaskSpec{Steps: []pipelinev1.Step{{
					Name:  "prove-execution",
					Image: "registry.access.redhat.com/ubi9/ubi-minimal@sha256:34880b64c07f28f64d95737f82f891516de9a3b43583f39970f7bf8e4cfa48b7",
					// Completion is released only after logs and hub Running status are observed.
					Script: "#!/bin/sh\nset -eu\nmkfifo /tmp/release-gate\nprintf '%s\\n' " + readyLine +
						"\nIFS= read -r marker < /tmp/release-gate\nprintf '%s\\n' \"$marker\"\n",
				}}}},
			}}},
		},
	}, queue
}

func validateAdmission(run *pipelinev1.PipelineRun, queue string) error {
	if run.Labels[queueLabel] != queue {
		return fmt.Errorf("admission queue = %q, want %q", run.Labels[queueLabel], queue)
	}
	if run.Spec.ManagedBy == nil || *run.Spec.ManagedBy != managedBy {
		return fmt.Errorf("admission did not set managedBy to %q", managedBy)
	}
	if run.Spec.Status != pipelinev1.PipelineRunSpecStatusPending {
		return fmt.Errorf("admission status = %q, want Pending", run.Spec.Status)
	}
	return nil
}
