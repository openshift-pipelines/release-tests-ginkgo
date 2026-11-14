package pipelines_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // Ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // Gomega convention
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

var _ = AfterEach(func(ctx SpecContext) {
	report := CurrentSpecReport()
	if !report.Failed() {
		return
	}
	nested := false
	for _, labels := range report.ContainerHierarchyLabels {
		if slices.Contains(labels, "no-auto-namespace") {
			return
		}
		nested = nested || slices.Contains(labels, "pipelines-in-pipelines")
	}
	if nested && lastNamespace != "" {
		By("Stop active nested tasks while preserving the failed namespace and diagnostic resources")
		stopNestedWorkload(ctx)
	}
}, NodeTimeout(5*time.Minute))

type nestedFailureRecord struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
}

var _ = Describe("Pipelines-in-Pipelines failure cleanup: PIPELINES-51", Ordered, Serial,
	Label("e2e", "pipelines", "pipelines-in-pipelines", "admin"), func() {
		if os.Getenv("PIP_FAILURE_PROBE") == "1" {
			It("PIPELINES-51-TC02 induces a failure with a live descendant", SpecTimeout(4*time.Minute), func(ctx SpecContext) {
				pods := sharedClients.KubeClient.Kube.CoreV1().Pods(lastNamespace)
				options := metav1.ListOptions{LabelSelector: "tekton.dev/pipelineTask=leaf"}
				list, err := pods.List(ctx, options)
				Expect(err).NotTo(HaveOccurred())
				Expect(list.Items).To(BeEmpty())
				options.ResourceVersion = list.ResourceVersion
				stream, err := pods.Watch(ctx, options)
				Expect(err).NotTo(HaveOccurred())
				defer stream.Stop()
				oc.Create("testdata/pipelines-in-pipelines/negative/lifecycle.yaml", lastNamespace)
				pod := awaitNestedRunningPod(ctx, stream)
				record, err := json.Marshal(nestedFailureRecord{Namespace: lastNamespace, Pod: pod.Name})
				Expect(err).NotTo(HaveOccurred())
				fmt.Printf("PIP_FAILURE_PROBE %s\n", record)
				Fail("induced assertion failure with an executing nested leaf")
			})
			return
		}
		BeforeAll(func(ctx SpecContext) { requireNestedPipelineFeature(ctx) })
		It("PIPELINES-51-TC01 stops descendants after an assertion failure and retains diagnostics", SpecTimeout(12*time.Minute), func(ctx SpecContext) {
			directory, err := config.TempDir()
			Expect(err).NotTo(HaveOccurred())
			sentinel, err := os.CreateTemp(directory, "pip-probe-parent-")
			Expect(err).NotTo(HaveOccurred())
			Expect(sentinel.Close()).To(Succeed())
			DeferCleanup(func() { Expect(os.Remove(sentinel.Name())).To(Succeed()) })
			executable, err := os.Executable()
			Expect(err).NotTo(HaveOccurred())
			command := exec.CommandContext(ctx, executable,
				"-test.run=^TestPipelines$", "-ginkgo.focus=PIPELINES-51-TC02", "-ginkgo.no-color=true",
				"-kubeconfig="+config.Flags.Kubeconfig, "-context="+config.Flags.Context, "-cluster="+config.Flags.Cluster)
			command.Env = append(os.Environ(), "PIP_FAILURE_PROBE=1")
			output, err := command.CombinedOutput()
			Expect(ctx.Err()).NotTo(HaveOccurred(), "failure probe exceeded its deadline: %s", output)
			_, statErr := os.Stat(sentinel.Name())
			Expect(statErr).NotTo(HaveOccurred(), "the probe must not delete its parent's temporary files")
			var exit *exec.ExitError
			Expect(errors.As(err, &exit)).To(BeTrue(), "probe must finish with the expected failed spec: %v\n%s", err, output)
			Expect(exit.ExitCode()).To(Equal(1), "%s", output)
			var record nestedFailureRecord
			for _, line := range strings.Split(string(output), "\n") {
				if data, ok := strings.CutPrefix(strings.TrimSpace(line), "PIP_FAILURE_PROBE "); ok {
					Expect(json.Unmarshal([]byte(data), &record)).To(Succeed())
				}
			}
			Expect(record.Namespace).NotTo(BeEmpty(), "probe failed before a live leaf was observed:\n%s", output)
			Expect(record.Pod).NotTo(BeEmpty())
			namespaces := sharedClients.KubeClient.Kube.CoreV1().Namespaces()
			namespace, err := namespaces.Get(ctx, record.Namespace, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "the expected failure must preserve its diagnostic namespace")
			DeferCleanup(func(cleanupCtx SpecContext) {
				if !CurrentSpecReport().Failed() {
					Expect(namespaces.Delete(cleanupCtx, namespace.Name, metav1.DeleteOptions{
						Preconditions: &metav1.Preconditions{UID: &namespace.UID},
					})).To(Succeed())
				}
			}, NodeTimeout(time.Minute))
			runs, err := sharedClients.Tekton.TektonV1().PipelineRuns(record.Namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(runs.Items).To(HaveLen(2), "retain parent and child PipelineRuns for diagnosis")
			tasks, err := sharedClients.Tekton.TektonV1().TaskRuns(record.Namespace).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks.Items).To(HaveLen(1))
			leaf := &tasks.Items[0]
			Expect(leaf.Spec.Status).To(Equal(v1.TaskRunSpecStatusCancelled))
			Expect(leaf.Status.GetCondition(apis.ConditionSucceeded)).NotTo(BeNil())
			Expect(leaf.Status.GetCondition(apis.ConditionSucceeded).Status).To(Equal(corev1.ConditionFalse))
			pod, err := sharedClients.KubeClient.Kube.CoreV1().Pods(record.Namespace).Get(ctx, record.Pod, metav1.GetOptions{})
			if !apierrors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Status.Phase).To(BeElementOf(corev1.PodSucceeded, corev1.PodFailed))
			}
		})
	})
