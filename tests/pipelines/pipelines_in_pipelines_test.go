package pipelines_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // Ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // Gomega convention
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"

	"k8s.io/apimachinery/pkg/types"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

func runNestedPipeline(ctx context.Context, fixture string) *v1.PipelineRun {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	options := metav1.ListOptions{FieldSelector: "metadata.name=pip-parent"}
	list, err := sharedClients.PipelineRunClient.List(ctx, options)
	Expect(err).NotTo(HaveOccurred())
	Expect(list.Items).To(BeEmpty(), "each scenario requires its own namespace")
	options.ResourceVersion = list.ResourceVersion
	stream, err := sharedClients.PipelineRunClient.Watch(ctx, options)
	Expect(err).NotTo(HaveOccurred())
	defer stream.Stop()
	By(fmt.Sprintf("Create %s in namespace %s and wait for pip-parent", fixture, lastNamespace))
	oc.Create(fixture, lastNamespace)
	run, err := pipelines.ReadNestedWatch(ctx, stream.ResultChan(), "pip-parent")
	if errors.Is(err, pipelines.ErrNestedWatchClosed) || apierrors.IsResourceExpired(err) {
		run, err = pipelines.ResumeNestedPipelineRun(ctx, sharedClients.PipelineRunClient, "pip-parent")
	}
	Expect(err).NotTo(HaveOccurred())
	By(fmt.Sprintf("PipelineRun %s completed: %+v", run.Name, run.Status.Conditions))
	return run
}

func nestedChild(ctx context.Context, parent *v1.PipelineRun, task string) *v1.PipelineRun {
	child, err := pipelines.NestedChild(ctx, sharedClients.PipelineRunClient, parent, task)
	Expect(err).NotTo(HaveOccurred())
	return child
}

func expectNestedState(run *v1.PipelineRun, status corev1.ConditionStatus) {
	Expect(run).NotTo(BeNil())
	By(fmt.Sprintf("Check PipelineRun %s state=%s owner=%v children=%v", run.Name, status, metav1.GetControllerOf(run), run.Status.ChildReferences))
	Expect(run.Status.GetCondition(apis.ConditionSucceeded)).NotTo(BeNil())
	Expect(run.Status.GetCondition(apis.ConditionSucceeded).Status).To(Equal(status), "PipelineRun %s: %+v", run.Name, run.Status.Conditions)
	if status == corev1.ConditionTrue {
		Expect(run.Status.StartTime).NotTo(BeNil())
		Expect(run.Status.CompletionTime).NotTo(BeNil())
	}
}

func nestedTask(ctx context.Context, parent *v1.PipelineRun, task string) *v1.TaskRun {
	run, err := pipelines.NestedTask(ctx, sharedClients.TaskRunClient, parent, task)
	Expect(err).NotTo(HaveOccurred())
	return run
}

func expectNestedRuns(ctx context.Context, expected ...*v1.PipelineRun) {
	runs, err := sharedClients.PipelineRunClient.List(ctx, metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	uids := make([]types.UID, len(expected))
	for i, run := range expected {
		uids[i] = run.UID
	}
	actual := make([]types.UID, len(runs.Items))
	for i, run := range runs.Items {
		actual[i] = run.UID
	}
	Expect(actual).To(ConsistOf(uids), "unexpected descendant PipelineRuns")
}

var _ = Describe("Pipelines-in-Pipelines: PIPELINES-44", Ordered, Serial, Label("e2e", "pipelines", "pipelines-in-pipelines", "admin"), func() {
	BeforeAll(func(ctx SpecContext) { requireNestedPipelineFeature(ctx) })
	for _, tc := range []struct {
		id, fixture string
		check       func(context.Context, *v1.PipelineRun)
	}{
		{"PIPELINES-44-TC01", "inline", func(ctx context.Context, parent *v1.PipelineRun) {
			fixture, err := os.ReadFile(config.Path("testdata/pipelines-in-pipelines/positive/inline.yaml"))
			Expect(err).NotTo(HaveOccurred())
			var submitted v1.PipelineRun
			Expect(yaml.Unmarshal(fixture, &submitted)).To(Succeed())
			child := nestedChild(ctx, parent, "child")
			expectNestedState(child, corev1.ConditionTrue)
			Expect(child.Spec.PipelineSpec).To(Equal(submitted.Spec.PipelineSpec.Tasks[0].PipelineSpec))
			Expect(child.Spec.Params).To(Equal(submitted.Spec.PipelineSpec.Tasks[0].Params))
			Expect(child.Labels).To(HaveKeyWithValue("pip.example.test/scenario", "inline"))
			Expect(child.Annotations).To(HaveKeyWithValue("pip.example.test/metadata", "propagated"))
			nestedTask(ctx, child, "leaf")
			expectNestedRuns(ctx, parent, child)
		}},
		{"PIPELINES-44-TC02", "named", func(ctx context.Context, parent *v1.PipelineRun) {
			child := nestedChild(ctx, parent, "child")
			expectNestedState(child, corev1.ConditionTrue)
			Expect(child.Spec.PipelineRef).NotTo(BeNil())
			Expect(child.Spec.PipelineRef.Name).To(Equal("pip-child"))
			Expect(child.Spec.TaskRunTemplate.ServiceAccountName).To(Equal("pip-runner"))
			leaf := nestedTask(ctx, child, "leaf")
			Expect(leaf.Spec.ServiceAccountName).To(Equal("pip-runner"))
			pod, err := sharedClients.KubeClient.Kube.CoreV1().Pods(parent.Namespace).Get(ctx, leaf.Status.PodName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.ServiceAccountName).To(Equal("pip-runner"))
			Expect(metav1.GetControllerOf(pod)).NotTo(BeNil())
			Expect(metav1.GetControllerOf(pod).UID).To(Equal(leaf.UID))
			expectNestedRuns(ctx, parent, child)
		}},
		{"PIPELINES-44-TC03", "mixed", func(ctx context.Context, parent *v1.PipelineRun) {
			referenced, inline := nestedChild(ctx, parent, "referenced"), nestedChild(ctx, parent, "inline")
			expectNestedState(referenced, corev1.ConditionTrue)
			expectNestedState(inline, corev1.ConditionTrue)
			grandchild := nestedChild(ctx, inline, "grandchild")
			expectNestedState(grandchild, corev1.ConditionTrue)
			for _, tc := range []struct {
				run     *v1.PipelineRun
				message string
			}{
				{referenced, "referenced-value"},
				{inline, "inline-value"},
				{grandchild, "inline-value"},
			} {
				Expect(tc.run.Spec.Params).To(Equal([]v1.Param{{Name: "message", Value: *v1.NewStructuredValues(tc.message)}}))
			}
			for _, tc := range []struct {
				run     *v1.PipelineRun
				task    string
				message string
			}{
				{parent, "regular", "regular-value"},
				{referenced, "leaf", "referenced-value"},
				{grandchild, "leaf", "inline-value"},
				{parent, "join", "joined-value"},
			} {
				task := nestedTask(ctx, tc.run, tc.task)
				Expect(task.Status.Results).To(ContainElement(v1.TaskRunResult{Name: "observed", Type: v1.ResultsTypeString, Value: *v1.NewStructuredValues(tc.message)}))
			}
			regular := nestedTask(ctx, parent, "regular")
			join := nestedTask(ctx, parent, "join")
			Expect(join.Status.StartTime).NotTo(BeNil())
			for _, sibling := range []*v1.PipelineRun{referenced, inline} {
				Expect(sibling.Status.StartTime.Time.Before(regular.Status.CompletionTime.Time)).To(BeFalse(),
					"sibling %s must start after regular completes", sibling.Name)
				Expect(join.Status.StartTime.Time.Before(sibling.Status.CompletionTime.Time)).To(BeFalse(),
					"join must start after sibling %s completes", sibling.Name)
			}
			expectNestedRuns(ctx, parent, referenced, inline, grandchild)
		}},
		{"PIPELINES-44-TC04", "workspace", func(ctx context.Context, parent *v1.PipelineRun) {
			child := nestedChild(ctx, parent, "child")
			expectNestedState(child, corev1.ConditionTrue)
			for _, run := range []*v1.PipelineRun{parent, child} {
				Expect(run.Spec.Workspaces).To(ConsistOf(HaveField("PersistentVolumeClaim.ClaimName", "pip-data")))
			}
			writer := nestedTask(ctx, parent, "writer")
			childReader := nestedTask(ctx, child, "reader")
			reader := nestedTask(ctx, parent, "reader")
			for _, run := range []*v1.TaskRun{writer, childReader, reader} {
				Expect(run.Spec.Workspaces).To(ConsistOf(HaveField("PersistentVolumeClaim.ClaimName", "pip-data")))
			}
			Expect(reader.Status.Results).To(ContainElement(v1.TaskRunResult{
				Name: "bytes", Type: v1.ResultsTypeString,
				Value: *v1.NewStructuredValues("6e657374656420706970656c696e650a7365636f6e64206c696e650a6368696c642077726974652d6261636b0a"),
			}))
			expectNestedRuns(ctx, parent, child)
		}},
		{"PIPELINES-44-TC05", "when", func(ctx context.Context, parent *v1.PipelineRun) {
			Expect(parent.Status.SkippedTasks).To(ConsistOf(HaveField("Name", "child")))
			Expect(nestedChild(ctx, parent, "child")).To(BeNil())
			child := nestedChild(ctx, parent, "true-child")
			expectNestedState(child, corev1.ConditionTrue)
			nestedTask(ctx, child, "leaf")
			continuation := nestedTask(ctx, parent, "continuation")
			Expect(continuation.Status.Results).To(ContainElement(v1.TaskRunResult{
				Name: "continued", Type: v1.ResultsTypeString, Value: *v1.NewStructuredValues("continued"),
			}))
			expectNestedRuns(ctx, parent, child)
		}},
		{"PIPELINES-44-TC06", "finally", func(ctx context.Context, parent *v1.PipelineRun) {
			child := nestedChild(ctx, parent, "child")
			expectNestedState(child, corev1.ConditionTrue)
			nestedTask(ctx, child, "leaf")
			main := nestedTask(ctx, parent, "main")
			Expect(child.Status.StartTime.Time.Before(main.Status.CompletionTime.Time)).To(BeFalse())
		}},
	} {
		Describe(tc.id, func() {
			It("runs "+tc.fixture, SpecTimeout(12*time.Minute), func(ctx SpecContext) {
				bounded, cancel := context.WithTimeout(ctx, 10*time.Minute)
				defer cancel()
				parent := runNestedPipeline(bounded, "testdata/pipelines-in-pipelines/positive/"+tc.fixture+".yaml")
				expectNestedState(parent, corev1.ConditionTrue)
				tc.check(bounded, parent)
			})
		})
	}
})
