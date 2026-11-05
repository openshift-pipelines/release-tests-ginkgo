package kindmulticluster

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

var (
	pods      = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	runs      = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
	workloads = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta2", Resource: "workloads"}
)

// Execute proves API admission, spoke execution, logs, and status return.
// Both kubeconfigs must refer to the disposable clusters owned by the script.
func Execute(ctx context.Context, hubPath, spokePath string, prelabelled bool) (err error) {
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
	input, queue := pipelineRun("admission-"+strings.ToLower(rand.Text()[:12]), prelabelled)
	run, err := hub.PipelineRunClient.Create(ctx, input, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create hub PipelineRun: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		err = errors.Join(err, hub.PipelineRunClient.Delete(cleanupCtx, run.Name, metav1.DeleteOptions{}))
	}()
	if err := validateAdmission(run, queue); err != nil {
		return err
	}
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
		return err
	}
	if workerRun.UID == run.UID || workerRun.Spec.ManagedBy != nil {
		return fmt.Errorf("spoke PipelineRun must have an independent UID and no managedBy")
	}
	spokeWatch, err := spoke.PipelineRunClient.Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + run.Name, ResourceVersion: workerRun.ResourceVersion,
	})
	if err != nil {
		return err
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
		return err
	}
	defer hubWatch.Stop()
	logs, err := spoke.KubeClient.Kube.CoreV1().Pods(namespace).GetLogs(pod.GetName(),
		&corev1.PodLogOptions{Container: container, Follow: true}).Stream(ctx)
	if err != nil {
		return fmt.Errorf("open spoke logs: %w", err)
	}
	defer func() { err = errors.Join(err, logs.Close()) }()
	scanner := bufio.NewScanner(logs)
	if err := readMarker(scanner, readyLine); err != nil {
		return err
	}
	_, err = observe(ctx, hub.Dynamic.Resource(runs).Namespace(namespace), func(object *unstructured.Unstructured) (bool, error) {
		if object.GetUID() != run.UID {
			return false, nil
		}
		conditions, _, err := unstructured.NestedSlice(object.Object, "status", "conditions")
		if err != nil {
			return false, err
		}
		for _, item := range conditions {
			condition, ok := item.(map[string]any)
			if !ok {
				return false, fmt.Errorf("malformed PipelineRun condition")
			}
			if condition["type"] == "Succeeded" {
				if condition["status"] == "False" {
					return false, fmt.Errorf("hub run failed: %v", condition["message"])
				}
				return condition["status"] == "Unknown" && condition["reason"] == "Running", nil
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("hub did not mirror Running status: %w", err)
	}
	if err := assertHubIdle(ctx, hub, run.Name); err != nil {
		return err
	}
	marker := "execution-" + rand.Text()
	command := exec.CommandContext(ctx, "kubectl", "--kubeconfig", spokePath, "--request-timeout=60s",
		"-n", namespace, "exec", pod.GetName(), "-c", container, "--",
		"sh", "-c", `printf '%s\n' "$1" > /tmp/release-gate`, "sh", marker)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("release spoke producer (%s): %w", output, err)
	}
	if err := readMarker(scanner, marker); err != nil {
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

func readMarker(scanner *bufio.Scanner, marker string) error {
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read spoke marker %q: %w", marker, err)
		}
		return fmt.Errorf("spoke log ended before marker %q", marker)
	}
	if scanner.Text() != marker {
		return fmt.Errorf("spoke log = %q, want marker %q", scanner.Text(), marker)
	}
	return nil
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
