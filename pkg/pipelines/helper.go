package pipelines

import (
	"bytes"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline"
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// GetPodForTaskRun returns the Pod associated with the given TaskRun.
func GetPodForTaskRun(c *clients.Clients, namespace string, tr *v1.TaskRun) *corev1.Pod {
	// The Pod name has a random suffix, so we filter by label to find the one we care about.
	pods, err := c.KubeClient.Kube.CoreV1().Pods(namespace).List(c.Ctx, metav1.ListOptions{
		LabelSelector: pipeline.TaskRunLabelKey + " = " + tr.Name,
	})
	Expect(err).NotTo(HaveOccurred(), "failed to get pod for task run %s", tr.Name)
	Expect(pods.Items).To(HaveLen(1), "Expected 1 pod for task run %s, but got %d pods", tr.Name, len(pods.Items))

	return &pods.Items[0]
}

// AssertLabelsMatch verifies that all expected labels are present with expected values.
func AssertLabelsMatch(expectedLabels, actualLabels map[string]string) {
	for key, expectedVal := range expectedLabels {
		Expect(actualLabels).To(HaveKeyWithValue(key, expectedVal),
			"Expected labels containing %s=%s but labels were %v", key, expectedVal, actualLabels)
	}
}

// AssertAnnotationsMatch verifies that all expected annotations are present with expected values.
func AssertAnnotationsMatch(expectedAnnotations, actualAnnotations map[string]string) {
	for key, expectedVal := range expectedAnnotations {
		Expect(actualAnnotations).To(HaveKeyWithValue(key, expectedVal),
			"Expected annotations containing %s=%s but annotations were %v", key, expectedVal, actualAnnotations)
	}
}

// Cast2pipelinerun converts a runtime.Object to a PipelineRun.
func Cast2pipelinerun(obj runtime.Object) (*v1.PipelineRun, error) {
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

// createKeyValuePairs formats a map as key-value pairs for logging.
func createKeyValuePairs(m map[string]string) string {
	b := new(bytes.Buffer)
	for key, value := range m {
		fmt.Fprintf(b, "%s = %s\n", key, value)
	}
	return b.String()
}

// Ensure unused helper functions are satisfied at compile time.
var _ = createKeyValuePairs
var _ = GinkgoWriter
