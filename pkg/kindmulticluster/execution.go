package kindmulticluster

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/multicluster"
)

var (
	pods      = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	workloads = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta2", Resource: "workloads"}
)

// Execute proves dispatch, live hub-proxy logs, and status return on the two
// script-owned clusters. It never bootstraps or alters a shared cluster.
func Execute(ctx context.Context, hubPath, spokePath string) (err error) {
	if hubPath == "" || spokePath == "" || hubPath == spokePath {
		return fmt.Errorf("two distinct explicit kubeconfig paths are required")
	}
	hub, err := clients.NewClientsWithContext(hubPath, "", "", namespace)
	if err != nil {
		return err
	}
	spoke, err := clients.NewClientsWithContext(spokePath, "", "", namespace)
	if err != nil {
		return err
	}
	if hub.KubeConfig.Host == spoke.KubeConfig.Host {
		return fmt.Errorf("hub and spoke must have distinct API servers")
	}
	runName := "release-proof-" + strings.ToLower(rand.Text()[:12])
	run, err := hub.PipelineRunClient.Create(ctx, pipelineRun(runName), metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create hub PipelineRun: %w", err)
	}
	// Entire clusters are deleted by the script, even if the suite is killed.
	// Delete the run as well for direct suite invocations on a prepared fixture.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		err = errors.Join(err, hub.PipelineRunClient.Delete(cleanupCtx, run.Name, metav1.DeleteOptions{}))
	}()
	_, err = observe(ctx, hub.Dynamic.Resource(workloads).Namespace(namespace), func(object *unstructured.Unstructured) (bool, error) {
		for _, owner := range object.GetOwnerReferences() {
			if owner.UID == run.UID {
				selected, _, err := unstructured.NestedString(object.Object, "status", "clusterName")
				return selected == "release-spoke", err
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("observe hub Workload selected spoke: %w", err)
	}
	pod, err := observe(ctx, spoke.Dynamic.Resource(pods).Namespace(namespace), func(object *unstructured.Unstructured) (bool, error) {
		if object.GetLabels()["tekton.dev/pipelineRun"] != run.Name {
			return false, nil
		}
		statuses, _, err := unstructured.NestedSlice(object.Object, "status", "containerStatuses")
		if err != nil {
			return false, err
		}
		for _, item := range statuses {
			status, ok := item.(map[string]any)
			if !ok {
				return false, fmt.Errorf("malformed container status")
			}
			_, running, err := unstructured.NestedMap(status, "state", "running")
			if err != nil {
				return false, err
			}
			if status["name"] == container && running {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("observe execution pod on spoke: %w", err)
	}
	workerRun, err := spoke.PipelineRunClient.Get(ctx, run.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read spoke PipelineRun: %w", err)
	}
	if workerRun.UID == run.UID || (workerRun.Spec.ManagedBy != nil && *workerRun.Spec.ManagedBy == "kueue.x-k8s.io/multikueue") {
		return fmt.Errorf("spoke PipelineRun was not independently created for local execution")
	}
	// Subscribe before releasing the FIFO. MultiKueue can delete the worker
	// immediately after copying success; a later GET would race that cleanup.
	spokeWatch, err := spoke.PipelineRunClient.Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + run.Name, ResourceVersion: workerRun.ResourceVersion,
	})
	if err != nil {
		return fmt.Errorf("subscribe to spoke completion: %w", err)
	}
	defer spokeWatch.Stop()
	hubRun, err := hub.PipelineRunClient.Get(ctx, run.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	hubWatch, err := hub.PipelineRunClient.Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + run.Name, ResourceVersion: hubRun.ResourceVersion,
	})
	if err != nil {
		return fmt.Errorf("subscribe to hub completion: %w", err)
	}
	defer hubWatch.Stop()
	if err := assertHubIdle(ctx, hub, run.Name); err != nil {
		return err
	}
	if err := withProxy(ctx, hub, func(ctx context.Context, endpoint proxyEndpoint) error {
		request := multicluster.LogRequest{
			Endpoint: endpoint.address, Token: endpoint.token, TLS: endpoint.tls,
			Namespace: namespace, PipelineRun: run.Name, Pod: pod.GetName(), Container: container,
			ReadyMarker: readyLine, LiveMarker: "live-" + rand.Text(),
		}
		return multicluster.VerifyLiveLogs(ctx, request, func(ctx context.Context) error {
			command := exec.CommandContext(ctx, "kubectl", "--kubeconfig", spokePath, "--request-timeout=60s",
				"-n", namespace, "exec", pod.GetName(), "-c", container, "--",
				"sh", "-c", `printf '%s\n' "$1" > /tmp/release-gate`, "sh", request.LiveMarker)
			output, err := command.CombinedOutput()
			if err != nil {
				return fmt.Errorf("release spoke log producer (%s): %w", output, err)
			}
			return nil
		})
	}); err != nil {
		return err
	}
	workerDone, err := completed(ctx, spokeWatch)
	if err != nil {
		return fmt.Errorf("spoke completion: %w", err)
	}
	hubDone, err := completed(ctx, hubWatch)
	if err != nil {
		return fmt.Errorf("hub status return: %w", err)
	}
	if len(workerDone.Status.ChildReferences) != 1 ||
		!reflect.DeepEqual(hubDone.Status.ChildReferences, workerDone.Status.ChildReferences) {
		return fmt.Errorf("hub status did not return the spoke TaskRun reference")
	}
	return assertHubIdle(ctx, hub, run.Name)
}

func assertHubIdle(ctx context.Context, hub *clients.Clients, runName string) error {
	selector := metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun=" + runName}
	tasks, err := hub.TaskRunClient.List(ctx, selector)
	if err != nil {
		return err
	}
	hubPods, err := hub.KubeClient.Kube.CoreV1().Pods(namespace).List(ctx, selector)
	if err != nil {
		return err
	}
	if len(tasks.Items) != 0 || len(hubPods.Items) != 0 {
		return fmt.Errorf("hub executed locally: %d TaskRuns and %d pods", len(tasks.Items), len(hubPods.Items))
	}
	return nil
}
