package operator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func CreateSecretsForTektonResults() {
	var password = cmd.MustSucceed("openssl", "rand", "-base64", "20").Stdout()
	password = strings.ReplaceAll(password, "\n", "")
	cmd.MustSucceed("oc", "create", "secret", "-n", "openshift-pipelines", "generic", "tekton-results-postgres", "--from-literal=POSTGRES_USER=result", "--from-literal=POSTGRES_PASSWORD="+password)
	// generating tls certificate
	cmd.MustSucceed("openssl", "req", "-x509", "-newkey", "rsa:4096", "-keyout", "key.pem", "-out", "cert.pem", "-days", "365", "-nodes", "-subj", "/CN=tekton-results-api-service.openshift-pipelines.svc.cluster.local", "-addext", "subjectAltName=DNS:tekton-results-api-service.openshift-pipelines.svc.cluster.local")
	// creating secret with generated certificate
	cmd.MustSucceed("oc", "create", "secret", "tls", "-n", "openshift-pipelines", "tekton-results-tls", "--cert=cert.pem", "--key=key.pem")
}

func EnsureResultsReady() {
	cmd.MustSucceedIncreasedTimeout(time.Minute*5, "oc", "wait", "--for=condition=Ready", "tektoninstallerset", "-l", "operator.tekton.dev/type=result", "--timeout=120s")
}

func CreateResultsRoute() {
	cmd.Run("oc", "create", "route", "-n", "openshift-pipelines", "passthrough", "tekton-results-api-service", "--service=tekton-results-api-service", "--port=8080")
}

func GetResultsApi() string {
	var resultsAPI = cmd.MustSucceed("oc", "get", "route", "tekton-results-api-service", "-n", "openshift-pipelines", "--no-headers", "-o", "custom-columns=:spec.host").Stdout() + ":443"
	resultsAPI = strings.ReplaceAll(resultsAPI, "\n", "")
	return resultsAPI
}

func GetResultsAnnotations(resourceType string) (string, string, string) {
	var resultUUID = cmd.MustSucceed("opc", resourceType, "describe", "--last", "-o", "jsonpath='{.metadata.annotations.results\\.tekton\\.dev/result}'").Stdout()
	var recordUUID = cmd.MustSucceed("opc", resourceType, "describe", "--last", "-o", "jsonpath='{.metadata.annotations.results\\.tekton\\.dev/record}'").Stdout()
	var stored = cmd.MustSucceed("opc", resourceType, "describe", "--last", "-o", "jsonpath='{.metadata.annotations.results\\.tekton\\.dev/stored}'").Stdout()
	recordUUID = strings.ReplaceAll(recordUUID, "'", "")
	resultUUID = strings.ReplaceAll(resultUUID, "'", "")
	stored = strings.ReplaceAll(stored, "'", "")
	return resultUUID, recordUUID, stored
}

func getRunsAnnotations(cs *clients.Clients, resourceType, name string) (map[string]string, error) {
	switch resourceType {
	case "taskrun":
		taskRun, err := cs.TaskRunClient.Get(cs.Ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return taskRun.GetAnnotations(), nil
	case "pipelinerun":
		pipelineRuns, err := cs.PipelineRunClient.Get(cs.Ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return pipelineRuns.GetAnnotations(), nil
	default:
		return nil, fmt.Errorf("invalid resource type: %s", resourceType)
	}
}

func VerifyResultsAnnotationStored(cs *clients.Clients, resourceType string) error {
	resourceName := cmd.MustSucceed("tkn", resourceType, "describe", "--last", "-o", "jsonpath='{.metadata.name}'").Stdout()
	resourceName = strings.ReplaceAll(resourceName, "'", "")

	log.Printf("Waiting for annotation 'results.tekton.dev/stored' to be true \n")
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, config.APITimeout, true, func(context.Context) (done bool, err error) {
		annotations, err := getRunsAnnotations(cs, resourceType, resourceName)
		if err != nil {
			return false, err
		}
		if annotations == nil || annotations["results.tekton.dev/stored"] == "" {
			log.Printf("Annotation 'results.tekton.dev/stored' is not set yet\n")
			return false, nil
		}
		if annotations["results.tekton.dev/stored"] == "true" {
			return true, nil
		}
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("annotation 'results.tekton.dev/stored' is not true: %v", err)
	}
	return nil
}

func VerifyResultsLogs(resourceType string) error {
	var recordUUID string
	var resultsAPI string
	_, recordUUID, _ = GetResultsAnnotations(resourceType)
	resultsAPI = GetResultsApi()

	if recordUUID == "" {
		return fmt.Errorf("annotation results.tekton.dev/record is not set")
	}

	var resultsJsonData = cmd.MustSucceed("opc", "results", "logs", "get", "--insecure", "--addr", resultsAPI, recordUUID).Stdout()
	if strings.Contains(resultsJsonData, "record not found") {
		return fmt.Errorf("results log not found")
	}

	type ResultLogs struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	var resultLogs ResultLogs
	err := json.Unmarshal([]byte(resultsJsonData), &resultLogs)
	if err != nil {
		return fmt.Errorf("error parsing JSON: %w", err)
	}
	decodedResultsLogs, err := base64.StdEncoding.Strict().DecodeString(resultLogs.Data)
	if err != nil {
		return fmt.Errorf("error decoding base64 data: %w", err)
	}
	if !strings.Contains(string(decodedResultsLogs), "Hello, Results!") || !strings.Contains(string(decodedResultsLogs), "Goodbye, Results!") {
		return fmt.Errorf("logs are incorrect: expected 'Hello, Results!' and 'Goodbye, Results!'")
	}
	return nil
}

func VerifyResultsRecords(resourceType string) error {
	var recordUUID string
	var resultsAPI string
	_, recordUUID, _ = GetResultsAnnotations(resourceType)
	resultsAPI = GetResultsApi()
	var resultsRecord = cmd.MustSucceed("opc", "results", "records", "get", "--insecure", "--addr", resultsAPI, recordUUID).Stdout()
	if strings.Contains(resultsRecord, "record not found") {
		return fmt.Errorf("results record not found")
	}

	type ResultRecords struct {
		Data struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"data"`
	}
	resultsJsonData := cmd.MustSucceed("opc", "results", "records", "get", "--insecure", "--addr", resultsAPI, recordUUID, "-o", "json").Stdout()
	var resultRecords ResultRecords
	err := json.Unmarshal([]byte(resultsJsonData), &resultRecords)
	if err != nil {
		return fmt.Errorf("error parsing JSON: %w", err)
	}
	decodedResultsLogs, err := base64.StdEncoding.Strict().DecodeString(resultRecords.Data.Value)
	if err != nil {
		return fmt.Errorf("error decoding base64 data: %w", err)
	}
	if !strings.Contains(string(decodedResultsLogs), "Hello, Results!") || !strings.Contains(string(decodedResultsLogs), "Goodbye, Results!") {
		return fmt.Errorf("records are incorrect: expected 'Hello, Results!' and 'Goodbye, Results!'")
	}
	return nil
}
