package operator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// CreateSecretsForTektonResults creates the required secrets for Tekton Results.
func CreateSecretsForTektonResults() {
	var password = cmd.MustSucceed("openssl", "rand", "-base64", "20").Stdout()
	password = strings.ReplaceAll(password, "\n", "")
	cmd.MustSucceed("oc", "create", "secret", "-n", "openshift-pipelines", "generic", "tekton-results-postgres", "--from-literal=POSTGRES_USER=result", "--from-literal=POSTGRES_PASSWORD="+password)
	// generating tls certificate
	cmd.MustSucceed("openssl", "req", "-x509", "-newkey", "rsa:4096", "-keyout", "key.pem", "-out", "cert.pem", "-days", "365", "-nodes", "-subj", "/CN=tekton-results-api-service.openshift-pipelines.svc.cluster.local", "-addext", "subjectAltName=DNS:tekton-results-api-service.openshift-pipelines.svc.cluster.local")
	// creating secret with generated certificate
	cmd.MustSucceed("oc", "create", "secret", "tls", "-n", "openshift-pipelines", "tekton-results-tls", "--cert=cert.pem", "--key=key.pem")
}

// EnsureResultsReady waits until the TektonResults deployment is ready.
func EnsureResultsReady() {
	cmd.MustSucceedIncreasedTimeout(time.Minute*5, "oc", "wait", "--for=condition=Ready", "tektoninstallerset", "-l", "operator.tekton.dev/type=result", "--timeout=120s")
}

// CreateResultsRoute creates the OpenShift route for the Tekton Results API.
func CreateResultsRoute() {
	cmd.Run("oc", "create", "route", "-n", "openshift-pipelines", "passthrough", "tekton-results-api-service", "--service=tekton-results-api-service", "--port=8080")
}

// GetResultsAPI returns the Tekton Results API endpoint URL from the route.
func GetResultsAPI() string {
	var resultsAPI = cmd.MustSucceed("oc", "get", "route", "tekton-results-api-service", "-n", "openshift-pipelines", "--no-headers", "-o", "custom-columns=:spec.host").Stdout() + ":443"
	resultsAPI = strings.ReplaceAll(resultsAPI, "\n", "")
	return resultsAPI
}

// GetResultsAnnotations returns the results name, record UUID, and log URL annotations for the given resource.
func GetResultsAnnotations(resourceType string) (string, string, string) {
	ns := store.Namespace()
	if ns == "" {
		panic("GetResultsAnnotations: store.Namespace() is empty - ensure hooks are configured")
	}
	var resultUUID = cmd.MustSucceed("opc", resourceType, "describe", "--last", "-o", "jsonpath='{.metadata.annotations.results\\.tekton\\.dev/result}'", "-n", ns).Stdout()
	var recordUUID = cmd.MustSucceed("opc", resourceType, "describe", "--last", "-o", "jsonpath='{.metadata.annotations.results\\.tekton\\.dev/record}'", "-n", ns).Stdout()
	var stored = cmd.MustSucceed("opc", resourceType, "describe", "--last", "-o", "jsonpath='{.metadata.annotations.results\\.tekton\\.dev/stored}'", "-n", ns).Stdout()
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

// VerifyResultsAnnotationStored verifies that Results annotations are stored on the resource.
func VerifyResultsAnnotationStored(cs *clients.Clients, resourceType string) error {
	ns := store.Namespace()
	if ns == "" {
		return fmt.Errorf("VerifyResultsAnnotationStored: store.Namespace() is empty - ensure hooks are configured")
	}
	resourceName := cmd.MustSucceed("tkn", resourceType, "describe", "--last", "-o", "jsonpath='{.metadata.name}'", "-n", ns).Stdout()
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
		return fmt.Errorf("annotation 'results.tekton.dev/stored' is not true: %w", err)
	}
	return nil
}

// VerifyResultsLogs verifies that Results logs are available for the given resource type.
func VerifyResultsLogs(resourceType string) error {
	var recordUUID string
	var resultsAPI string
	_, recordUUID, _ = GetResultsAnnotations(resourceType)
	resultsAPI = GetResultsAPI()

	if recordUUID == "" {
		return fmt.Errorf("annotation results.tekton.dev/record is not set")
	}

	// Wait for Results API to finish indexing after annotation is set
	// The annotation=true means data was sent, but API needs time to index it
	log.Printf("Waiting 10 seconds for Results API to index data\n")
	time.Sleep(10 * time.Second)

	var resultsJSONData = cmd.MustSucceed("opc", "results", "logs", "get", "--insecure", "--addr", resultsAPI, recordUUID).Stdout()
	if strings.Contains(resultsJSONData, "record not found") {
		return fmt.Errorf("results log not found")
	}

	type ResultLogs struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	var resultLogs ResultLogs
	err := json.Unmarshal([]byte(resultsJSONData), &resultLogs)
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

// VerifyResultsRecords verifies that the expected result records exist via the Results API.
func VerifyResultsRecords(resourceType string) error {
	var recordUUID string
	var resultsAPI string
	_, recordUUID, _ = GetResultsAnnotations(resourceType)
	resultsAPI = GetResultsAPI()

	// Wait for Results API to finish indexing after annotation is set
	// The annotation=true means data was sent, but API needs time to index it
	log.Printf("Waiting 10 seconds for Results API to index data\n")
	time.Sleep(10 * time.Second)

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
	resultsJSONData := cmd.MustSucceed("opc", "results", "records", "get", "--insecure", "--addr", resultsAPI, recordUUID, "-o", "json").Stdout()
	var resultRecords ResultRecords
	err := json.Unmarshal([]byte(resultsJSONData), &resultRecords)
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
