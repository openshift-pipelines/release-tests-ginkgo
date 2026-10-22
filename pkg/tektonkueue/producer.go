package tektonkueue

import (
	"context"
	"fmt"
	"io"
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

const producerScript = `#!/bin/sh
set -eu
mkfifo /tmp/rtg-gate1 /tmp/rtg-gate2
printf '%s\n' multi-cluster-execution-ok
read -r marker < /tmp/rtg-gate1
printf '%s\n' "$marker"
read -r finish < /tmp/rtg-gate2
`

func (e *Environment) createPipelineRun(ctx context.Context) (*pipelinev1.PipelineRun, error) {
	run := e.producerRun()
	created, err := e.Hub.Clients.PipelineRunClient.Create(ctx, run, metav1.CreateOptions{})
	if err != nil {
		return nil, requestError(ctx, "create hub PipelineRun")
	}
	return created, nil
}

func (e *Environment) producerRun() *pipelinev1.PipelineRun {
	timeout := metav1.Duration{Duration: 10 * time.Minute}
	labels := e.ownedLabels()
	labels[queueLabel] = e.Prefix
	run := &pipelinev1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{Name: e.Prefix + "-run", Namespace: e.Namespace, Labels: labels},
		Spec: pipelinev1.PipelineRunSpec{
			ManagedBy:       ptr(multiKueueController),
			Timeouts:        &pipelinev1.TimeoutFields{Pipeline: &timeout},
			TaskRunTemplate: pipelinev1.PipelineTaskRunTemplate{ServiceAccountName: "default"},
			PipelineSpec: &pipelinev1.PipelineSpec{Tasks: []pipelinev1.PipelineTask{{
				Name: "execute-on-spoke",
				TaskSpec: &pipelinev1.EmbeddedTask{TaskSpec: pipelinev1.TaskSpec{Steps: []pipelinev1.Step{{
					Name: "prove-execution", Image: workloadImage, Script: producerScript,
				}}}},
			}}},
		},
	}
	return run
}

type gateTarget struct {
	cluster   Cluster
	namespace string
	pod       string
	podUID    types.UID
}

func (g gateTarget) release(ctx context.Context, gate, value string) error {
	pod, err := g.cluster.Clients.KubeClient.Kube.CoreV1().Pods(g.namespace).Get(ctx, g.pod, metav1.GetOptions{})
	if err != nil {
		return requestError(ctx, "verify producer Pod identity")
	}
	if pod.UID != g.podUID {
		return fmt.Errorf("producer Pod was replaced")
	}
	// Values are positional arguments, never interpolated into shell source.
	request := g.cluster.Clients.KubeClient.Kube.CoreV1().RESTClient().Post().
		Namespace(g.namespace).Resource("pods").Name(g.pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: workloadLogContainer,
			Command:   []string{"sh", "-c", `printf '%s\n' "$2" > "/tmp/rtg-$1"`, "release", gate, value},
			Stderr:    true,
		}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(g.cluster.Clients.KubeConfig, "POST", request.URL())
	if err != nil {
		return requestError(ctx, "create gate executor")
	}
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stderr: io.Discard}); err != nil {
		return requestError(ctx, "release producer gate")
	}
	return nil
}
