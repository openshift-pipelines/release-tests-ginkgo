package pipelines

import (
	"fmt"
	"log"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/wait"
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var prGroupResource = schema.GroupVersionResource{Group: "tekton.dev", Resource: "pipelineruns"}

func validatePipelineRunForSuccessStatus(c *clients.Clients, prname, namespace string) {
	// Verify status of PipelineRun (wait for it)
	err := wait.WaitForPipelineRunState(c, prname, wait.PipelineRunSucceed(prname), "PipelineRunCompleted")
	if err != nil {
		logs := getPipelinerunLogs(prname, namespace)
		events, eventError := k8s.GetWarningEvents(c, namespace)
		if eventError != nil {
			Fail(fmt.Sprintf("error waiting for pipeline run %s to finish \n%v \npipelinerun logs: \n%s \npipelinerun events error: \n%v", prname, err, logs, eventError))
		} else {
			Fail(fmt.Sprintf("error waiting for pipeline run %s to finish \n%v \npipelinerun logs: \n%s \npipelinerun events: \n%v", prname, err, logs, events))
		}
	}

	log.Printf("pipelineRun: %s is successful under namespace : %s", prname, namespace)
}

func validatePipelineRunForFailedStatus(c *clients.Clients, prname, namespace string) {
	log.Printf("Waiting for PipelineRun in namespace %s to fail", namespace)
	err := wait.WaitForPipelineRunState(c, prname, wait.PipelineRunFailed(prname), "BuildValidationFailed")
	if err != nil {
		logs := getPipelinerunLogs(prname, namespace)
		events, eventError := k8s.GetWarningEvents(c, namespace)
		if eventError != nil {
			Fail(fmt.Sprintf("error waiting for pipeline run %s to finish \n%v \npipelinerun logs: \n%s \npipelinerun events error: \n%v", prname, err, logs, eventError))
		} else {
			Fail(fmt.Sprintf("error waiting for pipeline run %s to finish \n%v \npipelinerun logs: \n%s \npipelinerun events: \n%v", prname, err, logs, events))
		}
	}
}

func validatePipelineRunTimeoutFailure(c *clients.Clients, prname, namespace string) {
	pipelineRun, err := c.PipelineRunClient.Get(c.Ctx, prname, metav1.GetOptions{})
	if err != nil {
		Fail(fmt.Sprintf("failed to get pipeline run %s in namespace %s \n %v", prname, namespace, err))
	}

	log.Printf("Waiting for Pipelinerun %s in namespace %s to be started", pipelineRun.Name, namespace)
	if err := wait.WaitForPipelineRunState(c, pipelineRun.Name, wait.Running(pipelineRun.Name), "PipelineRunRunning"); err != nil {
		Fail(fmt.Sprintf("Error waiting for PipelineRun %s to be running: %s", pipelineRun.Name, err))
	}

	taskrunList, err := c.TaskRunClient.List(c.Ctx, metav1.ListOptions{LabelSelector: fmt.Sprintf("tekton.dev/pipelineRun=%s", pipelineRun.Name)})
	if err != nil {
		Fail(fmt.Sprintf("Error listing TaskRuns for PipelineRun %s: %v", pipelineRun.Name, err))
	}

	log.Printf("Waiting for TaskRuns from PipelineRun %s in namespace %s to be running", pipelineRun.Name, namespace)
	errChan := make(chan error, len(taskrunList.Items))
	defer close(errChan)

	for _, taskrunItem := range taskrunList.Items {
		go func(name string) {
			err := wait.WaitForTaskRunState(c, name, wait.Running(name), "TaskRunRunning")
			errChan <- err
		}(taskrunItem.Name)
	}

	for i := 1; i <= len(taskrunList.Items); i++ {
		if <-errChan != nil {
			Fail(fmt.Sprintf("Error waiting for TaskRun %s to be running: %v", taskrunList.Items[i-1].Name, err))
		}
	}

	if _, err := c.PipelineRunClient.Get(c.Ctx, pipelineRun.Name, metav1.GetOptions{}); err != nil {
		Fail(fmt.Sprintf("Failed to get PipelineRun `%s`: %s", pipelineRun.Name, err))
	}

	log.Printf("Waiting for PipelineRun %s in namespace %s to be timed out", pipelineRun.Name, namespace)
	if err := wait.WaitForPipelineRunState(c, pipelineRun.Name, wait.FailedWithReason(v1.PipelineRunReasonTimedOut.String(), pipelineRun.Name), "PipelineRunTimedOut"); err != nil {
		Fail(fmt.Sprintf("Error waiting for PipelineRun %s to finish: %s", pipelineRun.Name, err))
	}

	log.Printf("Waiting for TaskRuns from PipelineRun %s in namespace %s to be cancelled", pipelineRun.Name, namespace)
	var wg sync.WaitGroup
	for _, taskrunItem := range taskrunList.Items {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			err := wait.WaitForTaskRunState(c, name, wait.FailedWithReason(v1.TaskRunReasonCancelled.String(), name), v1.TaskRunReasonCancelled.String())
			if err != nil {
				Fail(fmt.Sprintf("error waiting for task run %s to be cancelled on pipeline timeout \n %v", name, err))
			}
		}(taskrunItem.Name)
	}
	wg.Wait()

	if _, err := c.PipelineRunClient.Get(c.Ctx, pipelineRun.Name, metav1.GetOptions{}); err != nil {
		Fail(fmt.Sprintf("Failed to get PipelineRun `%s`: %s", pipelineRun.Name, err))
	}
}

func validatePipelineRunCancel(c *clients.Clients, prname, namespace string) {
	log.Printf("Waiting for Pipelinerun %s in namespace %s to be started", prname, namespace)
	if err := wait.WaitForPipelineRunState(c, prname, wait.Running(prname), "PipelineRunRunning"); err != nil {
		Fail(fmt.Sprintf("Error waiting for PipelineRun %s to be running: %s", prname, err))
	}

	taskrunList, err := c.TaskRunClient.List(c.Ctx, metav1.ListOptions{LabelSelector: fmt.Sprintf("tekton.dev/pipelineRun=%s", prname)})
	if err != nil {
		Fail(fmt.Sprintf("Error listing TaskRuns for PipelineRun %s: %s", prname, err))
	}

	var wg sync.WaitGroup
	log.Printf("Canceling pipeline run: %s\n", cmd.MustSucceed("opc", "pipelinerun", "cancel", prname, "-n", namespace).Stdout())

	if err := wait.WaitForPipelineRunState(c, prname, wait.FailedWithReason("Cancelled", prname), "Cancelled"); err != nil {
		Fail(fmt.Sprintf("Error waiting for PipelineRun `%s` to finished: %s", prname, err))
	}

	log.Printf("Waiting for TaskRuns in PipelineRun %s in namespace %s to be cancelled", prname, namespace)
	for _, taskrunItem := range taskrunList.Items {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			err := wait.WaitForTaskRunState(c, name, wait.FailedWithReason(v1.TaskRunReasonCancelled.String(), name), "TaskRunCancelled")
			if err != nil {
				Fail(fmt.Sprintf("task run %s failed to finish \n %v", name, err))
			}
		}(taskrunItem.Name)
	}
	wg.Wait()
}

// ValidatePipelineRun dispatches to the correct validation function based on the expected status.
func ValidatePipelineRun(c *clients.Clients, prname, status, namespace string) {
	pr, err := c.PipelineRunClient.Get(c.Ctx, prname, metav1.GetOptions{})
	if err != nil {
		Fail(fmt.Sprintf("failed to get pipeline run %s in namespace %s \n %v", prname, namespace, err))
	}

	// Verify status of PipelineRun (wait for it)
	switch {
	case strings.Contains(strings.ToLower(status), "success"):
		log.Printf("validating pipeline run %s for success state...", prname)
		validatePipelineRunForSuccessStatus(c, pr.GetName(), namespace)
	case strings.Contains(strings.ToLower(status), "fail"):
		log.Printf("validating pipeline run %s for failure state...", prname)
		validatePipelineRunForFailedStatus(c, pr.GetName(), namespace)
	case strings.Contains(strings.ToLower(status), "timeout"):
		log.Printf("validating pipeline run %s to time out...", prname)
		validatePipelineRunTimeoutFailure(c, pr.GetName(), namespace)
	case strings.Contains(strings.ToLower(status), "cancel"):
		log.Printf("validating pipeline run %s to be cancelled...", prname)
		validatePipelineRunCancel(c, pr.GetName(), namespace)
	default:
		Fail(fmt.Sprintf("Not valid status input: %s", status))
	}
}

// cast2pipelinerun converts a runtime.Object to a PipelineRun
func cast2pipelinerun(obj runtime.Object) (*v1.PipelineRun, error) {
	var run *v1.PipelineRun
	unstruct, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct, &run); err != nil {
		return nil, err
	}
	return run, nil
}

// getPipelinerunLogs retrieves pipeline run logs using the oc CLI.
// This avoids adding tektoncd/cli as a dependency.
func getPipelinerunLogs(prname, namespace string) string {
	result := cmd.Run("oc", "logs", "--selector=tekton.dev/pipelineRun="+prname, "-n", namespace, "--all-containers", "--ignore-errors=true")
	return result.Stdout()
}

// Ensure unused imports are satisfied
var _ = prGroupResource
var _ = cast2pipelinerun
