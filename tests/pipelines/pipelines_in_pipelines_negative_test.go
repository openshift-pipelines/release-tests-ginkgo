package pipelines_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
)

var _ = Describe("Pipelines-in-Pipelines failures: PIPELINES-45", Ordered, Serial, Label("e2e", "pipelines", "pipelines-in-pipelines", "admin", "negative"), func() {
	BeforeAll(func(ctx SpecContext) { requireNestedPipelineFeature(ctx) })

	for _, tc := range []struct {
		id, fixture string
		check       func(context.Context, *v1.PipelineRun)
	}{
		{"PIPELINES-45-TC01", "failed-child", func(ctx context.Context, parent *v1.PipelineRun) {
			child := nestedChild(ctx, parent, "child")
			expectNestedState(child, corev1.ConditionFalse)
			tasks, err := sharedClients.TaskRunClient.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun=" + child.Name})
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks.Items).To(HaveLen(1))
			Expect(tasks.Items[0].Status.Steps).To(HaveLen(1))
			Expect(tasks.Items[0].Status.Steps[0].Terminated).NotTo(BeNil())
			Expect(tasks.Items[0].Status.Steps[0].Terminated.ExitCode).To(Equal(int32(7)))
			Expect(parent.Status.SkippedTasks).To(ContainElement(HaveField("Name", "after-child")))
			dependents, err := sharedClients.TaskRunClient.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineTask=after-child"})
			Expect(err).NotTo(HaveOccurred())
			Expect(dependents.Items).To(BeEmpty())
			expectNestedRuns(ctx, parent, child)
		}},
		{"PIPELINES-45-TC02", "missing-child", func(ctx context.Context, parent *v1.PipelineRun) {
			condition := parent.Status.GetCondition(apis.ConditionSucceeded)
			Expect(condition.Reason).To(Equal(v1.PipelineRunReasonCouldntGetPipeline.String()))
			Expect(condition.Message).To(ContainSubstring("pip-missing"))
			Expect(nestedChild(ctx, parent, "child")).To(BeNil())
			expectNestedRuns(ctx, parent)
			tasks, err := sharedClients.TaskRunClient.List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks.Items).To(BeEmpty(), "a missing child must not execute any TaskRuns")
		}},
		{"PIPELINES-45-TC03", "self-cycle", func(ctx context.Context, parent *v1.PipelineRun) {
			Expect(parent.Status.GetCondition(apis.ConditionSucceeded).Message).To(ContainSubstring("detected cycle in pipeline-in-pipeline"))
			runs, err := sharedClients.PipelineRunClient.List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(runs.Items).To(HaveLen(1), "a self-reference must fail before creating a child chain")
			Expect(runs.Items[0].UID).To(Equal(parent.UID))
		}},
		{"PIPELINES-45-TC04", "missing-workspace", func(ctx context.Context, parent *v1.PipelineRun) {
			child := nestedChild(ctx, parent, "child")
			expectNestedState(child, corev1.ConditionFalse)
			Expect(child.Status.GetCondition(apis.ConditionSucceeded).Message).To(ContainSubstring(`workspace with name "required-data"`))
		}},
	} {
		Describe(tc.id, func() {
			It("fails "+tc.fixture, SpecTimeout(12*time.Minute), func(ctx SpecContext) {
				bounded, cancel := context.WithTimeout(ctx, 10*time.Minute)
				defer cancel()
				parent := runNestedPipeline(bounded, "testdata/pipelines-in-pipelines/negative/"+tc.fixture+".yaml")
				expectNestedState(parent, corev1.ConditionFalse)
				tc.check(bounded, parent)
			})
		})
	}
	for _, tc := range []struct {
		id, fixture string
		childStatus corev1.ConditionStatus
		mainStatus  string
		exitCode    int32
	}{
		{"PIPELINES-45-TC05", "parent-main-finally", corev1.ConditionTrue, "Succeeded", 11},
		{"PIPELINES-45-TC06", "child-main-finally", corev1.ConditionFalse, "Failed", 7},
	} {
		Describe(tc.id, func() {
			It("completes child-local cleanup without hiding the original main failure", SpecTimeout(12*time.Minute), func(ctx SpecContext) {
				parent := runNestedPipeline(ctx, "testdata/pipelines-in-pipelines/negative/"+tc.fixture+".yaml")
				expectNestedState(parent, corev1.ConditionFalse)
				Expect(parent.Status.GetCondition(apis.ConditionSucceeded).Reason).To(Equal(v1.PipelineRunReasonFailed.String()))
				child := nestedChild(ctx, parent, "child")
				expectNestedState(child, tc.childStatus)
				cleanup := nestedTask(ctx, child, "cleanup")
				Expect(cleanup.Status.Results).To(ContainElement(v1.TaskRunResult{
					Name: "cleaned", Type: v1.ResultsTypeString, Value: *v1.NewStructuredValues(tc.mainStatus),
				}))
				failedRun := parent
				if tc.childStatus == corev1.ConditionFalse {
					failedRun = child
					Expect(parent.Status.SkippedTasks).To(ContainElement(HaveField("Name", "after-child")))
					dependents, err := sharedClients.TaskRunClient.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineTask=after-child"})
					Expect(err).NotTo(HaveOccurred())
					Expect(dependents.Items).To(BeEmpty())
				} else {
					nestedTask(ctx, child, "main")
				}
				Expect(failedRun.Status.GetCondition(apis.ConditionSucceeded).Reason).To(Equal(v1.PipelineRunReasonFailed.String()))
				tasks, err := sharedClients.TaskRunClient.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineTask=main"})
				Expect(err).NotTo(HaveOccurred())
				var failures []v1.TaskRun
				for _, task := range tasks.Items {
					if owner := metav1.GetControllerOf(&task); owner != nil && owner.UID == failedRun.UID {
						failures = append(failures, task)
					}
				}
				Expect(failures).To(HaveLen(1))
				Expect(failures[0].Status.GetCondition(apis.ConditionSucceeded)).NotTo(BeNil())
				Expect(failures[0].Status.GetCondition(apis.ConditionSucceeded).Status).To(Equal(corev1.ConditionFalse))
				Expect(failures[0].Status.Steps).To(HaveLen(1))
				Expect(failures[0].Status.Steps[0].Terminated).NotTo(BeNil())
				Expect(failures[0].Status.Steps[0].Terminated.ExitCode).To(Equal(tc.exitCode))
				expectNestedRuns(ctx, parent, child)
			})
		})
	}
})
