package kindmulticluster

import (
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	namespace = "release-multicluster"
	readyLine = "waiting-for-live-log-proof"
	container = "step-prove-execution"
)

func pipelineRun(name string) *pipelinev1.PipelineRun {
	managedBy := "kueue.x-k8s.io/multikueue"
	timeout := metav1.Duration{Duration: 10 * time.Minute}
	return &pipelinev1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace,
			Labels: map[string]string{"kueue.x-k8s.io/queue-name": namespace},
		},
		Spec: pipelinev1.PipelineRunSpec{
			ManagedBy: &managedBy,
			Timeouts:  &pipelinev1.TimeoutFields{Pipeline: &timeout},
			PipelineSpec: &pipelinev1.PipelineSpec{Tasks: []pipelinev1.PipelineTask{{
				Name: "execute-on-spoke",
				TaskSpec: &pipelinev1.EmbeddedTask{TaskSpec: pipelinev1.TaskSpec{Steps: []pipelinev1.Step{{
					Name:  "prove-execution",
					Image: "registry.access.redhat.com/ubi9/ubi-minimal@sha256:34880b64c07f28f64d95737f82f891516de9a3b43583f39970f7bf8e4cfa48b7",
					// A FIFO provides a causal gate, not a timing window. The
					// verifier writes its unpredictable marker only after the
					// hub WebSocket has delivered the readiness line.
					Script: "#!/bin/sh\nset -eu\nmkfifo /tmp/release-gate\nprintf '%s\\n' " + readyLine +
						"\nIFS= read -r marker < /tmp/release-gate\nprintf '%s\\n' \"$marker\"\n",
				}}}},
			}}},
		},
	}
}
