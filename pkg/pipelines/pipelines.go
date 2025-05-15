package pipelines

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/wait"
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	w "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"

	. "github.com/onsi/ginkgo/v2"
)

var prGroupResource = schema.GroupVersionResource{Group: "tekton.dev", Resource: "pipelineruns", Version: "v1"}

func validatePipelineRunForSuccessStatus(c *clients.Clients, prname, namespace string) {
	// Update client namespace context for this validation
	// Verify status of PipelineRun (wait for it)
	err := wait.WaitForPipelineRunState(c, prname, wait.PipelineRunSucceed(c, prname), "PipelineRunCompleted")
	if err != nil {
		buf, logsErr := getPipelinerunLogs(c, prname, namespace)
		events, eventError := k8s.GetWarningEvents(c, namespace)
		errorMsg := fmt.Sprintf("error waiting for pipeline run %s to finish", prname)
		if logsErr != nil {
			if eventError != nil {
				Fail(fmt.Sprintf("%s \n%v \npipelinerun logs error: \n%v \npipelinerun events error: \n%v", errorMsg, err, logsErr, eventError))
			} else {
				Fail(fmt.Sprintf("%s \n%v \npipelinerun logs error: \n%v \npipelinerun events: \n%v", errorMsg, err, logsErr, events))
			}
		} else {
			if eventError != nil {
				Fail(fmt.Sprintf("%s \n%v \npipelinerun logs: \n%v \npipelinerun events error: \n%v", errorMsg, err, buf.String(), eventError))
			} else {
				Fail(fmt.Sprintf("%s \n%v \npipelinerun logs: \n%v \npipelinerun events: \n%v", errorMsg, err, buf.String(), events))
			}
		}
	}

	log.Printf("pipelineRun: %s is successful under namespace : %s", prname, namespace)
}

func validatePipelineRunForFailedStatus(c *clients.Clients, prname, namespace string) {
	var err error
	log.Printf("Waiting for PipelineRun in namespace %s to fail", namespace)
	err = wait.WaitForPipelineRunState(c, prname, wait.PipelineRunFailed(c, prname), "BuildValidationFailed")
	if err != nil {
		buf, logsErr := getPipelinerunLogs(c, prname, namespace)
		events, eventError := k8s.GetWarningEvents(c, namespace)
		errorMsg := fmt.Sprintf("error waiting for pipeline run %s to finish", prname)
		if logsErr != nil {
			if eventError != nil {
				Fail(fmt.Sprintf("%s \n%v \npipelinerun logs error: \n%v \npipelinerun events error: \n%v", errorMsg, err, logsErr, eventError))
			} else {
				Fail(fmt.Sprintf("%s \n%v \npipelinerun logs error: \n%v \npipelinerun events: \n%v", errorMsg, err, logsErr, events))
			}
		} else {
			if eventError != nil {
				Fail(fmt.Sprintf("%s \n%v \npipelinerun logs: \n%v \npipelinerun events error: \n%v", errorMsg, err, buf.String(), eventError))
			} else {
				Fail(fmt.Sprintf("%s \n%v \npipelinerun logs: \n%v \npipelinerun events: \n%v", errorMsg, err, buf.String(), events))
			}
		}
	}
}

func validatePipelineRunTimeoutFailure(c *clients.Clients, prname, namespace string) {
	var err error
	pipelineRun, err := c.PipelineRunClient.Get(c.Ctx, prname, metav1.GetOptions{})
	if err != nil {
		Fail(fmt.Sprintf("failed to get pipeline run %s in namespaces %s \n %v", prname, namespace, err))
	}

	log.Printf("Waiting for Pipelinerun %s in namespace %s to be started", pipelineRun.Name, namespace)
	if err := wait.WaitForPipelineRunState(c, pipelineRun.Name, wait.Running(c, pipelineRun.Name), "PipelineRunRunning"); err != nil {
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
			err := wait.WaitForTaskRunState(c, name, wait.TaskRunRunning(c, name), "TaskRunRunning")
			errChan <- err
		}(taskrunItem.Name)
	}

	for i := 1; i <= len(taskrunList.Items); i++ {
		if err := <-errChan; err != nil {
			Fail(fmt.Sprintf("Error waiting for TaskRun %s to be running: %v", taskrunList.Items[i-1].Name, err))
		}
	}

	if _, err := c.PipelineRunClient.Get(c.Ctx, pipelineRun.Name, metav1.GetOptions{}); err != nil {
		Fail(fmt.Sprintf("Failed to get PipelineRun `%s`: %s", pipelineRun.Name, err))
	}

	log.Printf("Waiting for PipelineRun %s in namespace %s to be timed out", pipelineRun.Name, namespace)
	if err := wait.WaitForPipelineRunState(c, pipelineRun.Name, wait.FailedWithReason(c, v1.PipelineRunReasonTimedOut.String(), pipelineRun.Name), "PipelineRunTimedOut"); err != nil {
		Fail(fmt.Sprintf("Error waiting for PipelineRun %s to finish: %s", pipelineRun.Name, err))
	}

	log.Printf("Waiting for TaskRuns from PipelineRun %s in namespace %s to be cancelled", pipelineRun.Name, namespace)
	var wg sync.WaitGroup
	for _, taskrunItem := range taskrunList.Items {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			err := wait.WaitForTaskRunState(c, name, wait.TaskRunFailedWithReason(c, v1.TaskRunReasonCancelled.String(), name), v1.TaskRunReasonCancelled.String())
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
	var err error

	log.Printf("Waiting for Pipelinerun %s in namespace %s to be started", prname, namespace)
	if err := wait.WaitForPipelineRunState(c, prname, wait.Running(c, prname), "PipelineRunRunning"); err != nil {
		Fail(fmt.Sprintf("Error waiting for PipelineRun %s to be running: %s", prname, err))
	}

	taskrunList, err := c.TaskRunClient.List(c.Ctx, metav1.ListOptions{LabelSelector: fmt.Sprintf("tekton.dev/pipelineRun=%s", prname)})
	if err != nil {
		Fail(fmt.Sprintf("Error listing TaskRuns for PipelineRun %s: %s", prname, err))
	}

	var wg sync.WaitGroup
	log.Printf("Canceling pipeline run: %s\n", cmd.MustSucceed("opc", "pipelinerun", "cancel", prname, "-n", namespace).Stdout())

	if err := wait.WaitForPipelineRunState(c, prname, wait.FailedWithReason(c, "Cancelled", prname), "Cancelled"); err != nil {
		Fail(fmt.Sprintf("Error waiting for PipelineRun `%s` to finished: %s", prname, err))
	}

	log.Printf("Waiting for TaskRuns in PipelineRun %s in namespace %s to be cancelled", prname, namespace)
	for _, taskrunItem := range taskrunList.Items {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			err := wait.WaitForTaskRunState(c, name, wait.TaskRunFailedWithReason(c, v1.TaskRunReasonCancelled.String(), name), "TaskRunCancelled")
			if err != nil {
				Fail(fmt.Sprintf("task run %s failed to finish \n %v", name, err))
			}
		}(taskrunItem.Name)
	}
	wg.Wait()
}

func ValidatePipelineRun(c *clients.Clients, prname, status, namespace string) {
	var err error
	// Ensure we use the correct namespace-scoped client
	prClient := c.Tekton.TektonV1().PipelineRuns(namespace)
	pr, err := prClient.Get(c.Ctx, prname, metav1.GetOptions{})
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
		Fail(fmt.Sprintf("Error: %s ", "Not valid input"))
	}
}

func WatchForPipelineRun(c *clients.Clients, namespace string) {
	var prnames = []string{}
	watchRun, err := k8s.Watch(c.Ctx, prGroupResource, c, namespace, metav1.ListOptions{})
	if err != nil {
		Fail(fmt.Sprintf("failed to watch pipeline runs in namespace %s \n %v", namespace, err))
	}

	ch := watchRun.ResultChan()
	go func() {
		for event := range ch {
			run, err := cast2pipelinerun(event.Object)
			if err != nil {
				Fail(fmt.Sprintf("failed to convert pipeline run to v1 in namespace %s \n %v", namespace, err))
			}
			if event.Type == watch.Added {
				log.Printf("pipeline run : %s", run.Name)
				prnames = append(prnames, run.Name)
			}
		}
	}()
	time.Sleep(5 * time.Minute)
	store.PutScenarioData("prcount", strconv.Itoa(len(prnames)))
	log.Printf("Pipeline runs found: %+v", prnames)
}

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

func AssertForNoNewPipelineRunCreation(c *clients.Clients, namespace string) {
	count := 0
	expectedCountStr := store.GetScenarioData("prcount")
	if expectedCountStr == "" {
		Fail("prcount not found in scenario data")
	}
	expectedCount, err := strconv.Atoi(expectedCountStr)
	if err != nil {
		Fail(fmt.Sprintf("failed to parse prcount: %v", err))
	}
	watchRun, err := k8s.Watch(c.Ctx, prGroupResource, c, namespace, metav1.ListOptions{})
	if err != nil {
		Fail(fmt.Sprintf("failed to watch pipeline runs in namespace %s \n %v", namespace, err))
	}
	ch := watchRun.ResultChan()
	go func() {
		for event := range ch {
			if event.Type == watch.Added {
				count++
			}
		}
	}()
	time.Sleep(1 * time.Minute)
	if count < expectedCount {
		Fail(fmt.Sprintf("Error:  Expected: %+v (tekton resources add newly in namespace %s), \n Actual: %+v ", expectedCount, namespace, count))
	}
}

func AssertNumberOfPipelineruns(c *clients.Clients, namespace, numberOfPr, timeoutSeconds string) {
	log.Printf("Verifying if %s pipelineruns are present", numberOfPr)
	timeoutSecondsInt, _ := strconv.Atoi(timeoutSeconds)
	err := w.PollUntilContextTimeout(c.Ctx, config.APIRetry, time.Second*time.Duration(timeoutSecondsInt), false, func(context.Context) (bool, error) {
		prlist, err := c.PipelineRunClient.List(c.Ctx, metav1.ListOptions{})
		numberOfPrInt, _ := strconv.Atoi(numberOfPr)
		if len(prlist.Items) == numberOfPrInt {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		prlist, _ := c.PipelineRunClient.List(c.Ctx, metav1.ListOptions{})
		Fail(fmt.Sprintf("error: Expected %v pipelineruns but found %v pipelineruns: %s", numberOfPr, len(prlist.Items), err))
	}
}

func AssertNumberOfTaskruns(c *clients.Clients, namespace, numberOfTr, timeoutSeconds string) {
	log.Printf("Verifying if %s taskruns are present", numberOfTr)
	timeoutSecondsInt, _ := strconv.Atoi(timeoutSeconds)
	err := w.PollUntilContextTimeout(c.Ctx, config.APIRetry, time.Second*time.Duration(timeoutSecondsInt), false, func(context.Context) (bool, error) {
		trlist, err := c.TaskRunClient.List(c.Ctx, metav1.ListOptions{})
		numberOfTrInt, _ := strconv.Atoi(numberOfTr)
		if len(trlist.Items) == numberOfTrInt {
			return true, nil
		}
		return false, err
	})
	if err != nil {
		trlist, _ := c.TaskRunClient.List(c.Ctx, metav1.ListOptions{})
		Fail(fmt.Sprintf("error: Expected %v taskruns but found %v taskruns: %s", numberOfTr, len(trlist.Items), err))
	}
}

func AssertPipelinesPresent(c *clients.Clients, namespace string) {
	pclient := c.Tekton.TektonV1beta1().Pipelines(namespace)
	expectedNumberOfPipelines := len(config.PrefixesOfDefaultPipelines)
	if config.Flags.ClusterArch == "arm64" {
		expectedNumberOfPipelines *= 2
	} else {
		expectedNumberOfPipelines *= 3
	}

	err := w.PollUntilContextTimeout(c.Ctx, config.APIRetry, config.ResourceTimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying that %v pipelines are present in namespace %v", expectedNumberOfPipelines, namespace)
		p, _ := pclient.List(c.Ctx, metav1.ListOptions{})
		if len(p.Items) == expectedNumberOfPipelines {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		p, _ := pclient.List(c.Ctx, metav1.ListOptions{})
		Fail(fmt.Sprintf("expected: %v pipelines present in namespace %v, Actual: %v pipelines present in namespace %v , Error: %v", expectedNumberOfPipelines, namespace, len(p.Items), namespace, err))
	}
	log.Printf("Pipelines are present in namespace %v", namespace)
}

func AssertPipelinesNotPresent(c *clients.Clients, namespace string) {
	pclient := c.Tekton.TektonV1beta1().Pipelines(namespace)
	err := w.PollUntilContextTimeout(c.Ctx, config.APIRetry, config.ResourceTimeout, false, func(context.Context) (bool, error) {
		log.Printf("Verifying if 0 pipelines are not present in namespace %v", namespace)
		p, _ := pclient.List(c.Ctx, metav1.ListOptions{})
		if len(p.Items) == 0 {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		p, _ := pclient.List(c.Ctx, metav1.ListOptions{})
		Fail(fmt.Sprintf("expected: %v number of pipelines present in namespace %v, Actual: %v number of pipelines present in namespace %v , Error: %v", 0, namespace, len(p.Items), namespace, err))
	}
	log.Printf("Pipelines are not present in namespace %v", namespace)
}

func getPipelinerunLogs(c *clients.Clients, prname, namespace string) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	
	// Use opc command to get logs
	result := cmd.Run("opc", "pipelinerun", "logs", prname, "-n", namespace)
	buf.WriteString(result.Stdout())
	buf.WriteString(result.Stderr())
	
	if result.ExitCode != 0 {
		return buf, fmt.Errorf("failed to get logs: exit code %d", result.ExitCode)
	}
	return buf, nil
}

func GetLatestPipelinerun(c *clients.Clients, namespace string) (string, error) {
	// Use namespace-scoped client
	prClient := c.Tekton.TektonV1().PipelineRuns(namespace)
	prs, err := prClient.List(c.Ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list PipelineRuns: %v", err)
	}
	if len(prs.Items) == 0 {
		return "", fmt.Errorf("no pipelineruns found in the namespace %s", namespace)
	}
	// Sort by start time (most recent first)
	sort.Slice(prs.Items, func(i, j int) bool {
		if prs.Items[i].Status.StartTime == nil {
			return false
		}
		if prs.Items[j].Status.StartTime == nil {
			return true
		}
		return prs.Items[i].Status.StartTime.After(prs.Items[j].Status.StartTime.Time)
	})
	return prs.Items[0].Name, nil
}

func CheckLogVersion(c *clients.Clients, binary, namespace string) {
	prname, err := GetLatestPipelinerun(store.Clients(), store.Namespace())
	if err != nil {
		Fail(fmt.Sprintf("failed to get PipelineRun: %v", err))
		return
	}
	// Get PipelineRun logs
	logsBuffer, err := getPipelinerunLogs(c, prname, namespace)
	if err != nil {
		Fail(fmt.Sprintf("failed to get PipelineRun logs: %v", err))
		return
	}

	switch binary {
	case "tkn-pac":
		expectedVersion := os.Getenv("PAC_VERSION")
		if expectedVersion != "" && !strings.Contains(logsBuffer.String(), expectedVersion) {
			Fail(fmt.Sprintf("tkn-pac Version %s not found in logs:\n%s ", expectedVersion, logsBuffer))
		}
	case "tkn":
		expectedVersion := os.Getenv("TKN_CLIENT_VERSION")
		if !strings.Contains(logsBuffer.String(), "Client version:") {
			Fail(fmt.Sprintf("tkn client version not found! \nlogs:%s", logsBuffer))
			return
		}
		if expectedVersion != "" && !strings.Contains(logsBuffer.String(), expectedVersion) {
			Fail(fmt.Sprintf("tkn Version %s not found in logs:\n%s ", expectedVersion, logsBuffer))
		}
	default:
		Fail(fmt.Sprintf("unknown binary or client"))
	}
}
