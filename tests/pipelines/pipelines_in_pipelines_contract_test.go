package pipelines_test

import (
	"context"
	"errors"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // Ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // Gomega convention
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/yaml"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

var _ = Describe("Pipelines-in-Pipelines contracts: PIPELINES-49", Ordered, Serial, Label("e2e", "pipelines", "pipelines-in-pipelines", "admin"), func() {
	BeforeAll(func(ctx SpecContext) { requireNestedPipelineFeature(ctx) })

	Describe("PIPELINES-49-TC01", func() {
		It("rejects parent consumption of a child Pipeline result at admission", SpecTimeout(2*time.Minute), func(ctx SpecContext) {
			// Given an otherwise valid child result producer and parent consumer.
			data, err := os.ReadFile(config.Path("testdata/pipelines-in-pipelines/negative/child-result-consumption.yaml"))
			Expect(err).NotTo(HaveOccurred())
			var request v1.PipelineRun
			Expect(yaml.Unmarshal(data, &request)).To(Succeed())
			request.Namespace = lastNamespace

			// When submitted through the real typed client.
			_, err = sharedClients.PipelineRunClient.Create(ctx, &request, metav1.CreateOptions{})

			// Then only this specific validation rejection satisfies the contract.
			Expect(apierrors.IsBadRequest(err)).To(BeTrue(), "expected webhook BadRequest, got %v", err)
			Expect(err.Error()).To(ContainSubstring("spec.pipelineSpec.tasks[1]"))
			Expect(err.Error()).To(ContainSubstring(`result reference to pipelineTask "child" is not supported`))
			Expect(err.Error()).To(ContainSubstring("result propagation from child Pipelines is not yet implemented"))
		})
	})

	for _, tc := range []struct{ id, field string }{
		{"PIPELINES-49-TC02", "pipelineRef"},
		{"PIPELINES-49-TC03", "pipelineSpec"},
	} {
		Describe(tc.id, func() {
			It("rejects nested "+tc.field+" with alpha disabled but ordinary inline specs enabled", SpecTimeout(12*time.Minute), func(ctx SpecContext) {
				// Given beta fields, inline specs enabled, and cleanup registered before changing flags.
				DeferCleanup(func(cleanupCtx SpecContext) {
					setNestedFlags(cleanupCtx, map[string]interface{}{"enable-api-fields": "alpha", "disable-inline-spec": "false"},
						map[string]string{"enable-api-fields": "alpha", "disable-inline-spec": "false"})
				}, NodeTimeout(5*time.Minute))
				setNestedFlags(ctx, map[string]interface{}{"enable-api-fields": "beta", "disable-inline-spec": "false"},
					map[string]string{"enable-api-fields": "beta", "disable-inline-spec": "false"})
				bounded, cancel := context.WithTimeout(ctx, 10*time.Minute)
				defer cancel()
				data, err := os.ReadFile(config.Path("testdata/v1beta1/pipelinerun/pipelinerun-with-pipelinespec-and-taskspec.yaml"))
				Expect(err).NotTo(HaveOccurred())
				var ordinary v1.PipelineRun
				Expect(yaml.Unmarshal(data, &ordinary)).To(Succeed())
				ordinary.APIVersion = "tekton.dev/v1"
				ordinary.Namespace = lastNamespace
				options := metav1.ListOptions{FieldSelector: "metadata.name=" + ordinary.Name}
				list, err := sharedClients.PipelineRunClient.List(bounded, options)
				Expect(err).NotTo(HaveOccurred())
				Expect(list.Items).To(BeEmpty())
				options.ResourceVersion = list.ResourceVersion
				stream, err := sharedClients.PipelineRunClient.Watch(bounded, options)
				Expect(err).NotTo(HaveOccurred())
				defer stream.Stop()
				_, err = sharedClients.PipelineRunClient.Create(bounded, &ordinary, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), "ordinary inline PipelineSpec/TaskSpec must be admitted on this build")
				control, err := pipelines.ReadNestedWatch(bounded, stream.ResultChan(), ordinary.Name)
				if errors.Is(err, pipelines.ErrNestedWatchClosed) || apierrors.IsResourceExpired(err) {
					control, err = pipelines.ResumeNestedPipelineRun(bounded, sharedClients.PipelineRunClient, ordinary.Name)
				}
				Expect(err).NotTo(HaveOccurred())
				expectNestedState(control, corev1.ConditionTrue)
				request := ordinary.DeepCopy()
				request.Name = "pip-parent"
				request.Spec.Params = nil
				request.Spec.PipelineSpec = &v1.PipelineSpec{Tasks: []v1.PipelineTask{{
					Name: "child", PipelineRef: &v1.PipelineRef{Name: "pip-child"},
				}}}
				if tc.field == "pipelineSpec" {
					request.Spec.PipelineSpec.Tasks[0].PipelineRef = nil
					request.Spec.PipelineSpec.Tasks[0].PipelineSpec = ordinary.Spec.PipelineSpec.DeepCopy()
				}

				// When a nested field is submitted on the same build.
				_, err = sharedClients.PipelineRunClient.Create(bounded, request, metav1.CreateOptions{})

				// Then the feature-gate contract, not a general API failure, rejects it.
				Expect(apierrors.IsBadRequest(err)).To(BeTrue(), "expected webhook BadRequest, got %v", err)
				Expect(err.Error()).To(ContainSubstring("spec.pipelineSpec.tasks[0]"))
				Expect(err.Error()).To(ContainSubstring(tc.field + ` requires "enable-api-fields" feature gate to be "alpha" but it is "beta"`))
			})
		})
	}

	Describe("PIPELINES-49-TC04", func() {
		It("continues after a failed child when its PipelineTask uses onError continue", SpecTimeout(12*time.Minute), func(ctx SpecContext) {
			// Given a child that exits 7 and an ordinary dependent result producer.
			// When the parent runs with onError continue on the nested PipelineTask.
			parent := runNestedPipeline(ctx, "testdata/pipelines-in-pipelines/positive/onerror-continue.yaml")

			// Then the failure is ignored only at the parent scheduling boundary.
			expectNestedState(parent, corev1.ConditionTrue)
			Expect(parent.Status.GetCondition(apis.ConditionSucceeded).Reason).To(Equal(v1.PipelineRunReasonSuccessful.String()))
			Expect(parent.Status.GetCondition(apis.ConditionSucceeded).Message).To(ContainSubstring("Failed: 1 (Ignored: 1)"))
			child := nestedChild(ctx, parent, "child")
			expectNestedState(child, corev1.ConditionFalse)
			Expect(child.Status.GetCondition(apis.ConditionSucceeded).Reason).To(Equal(v1.PipelineRunReasonFailed.String()))
			failures, err := sharedClients.TaskRunClient.List(ctx, metav1.ListOptions{
				LabelSelector: "tekton.dev/pipelineRun=" + child.Name + ",tekton.dev/pipelineTask=fail",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(failures.Items).To(HaveLen(1))
			failed := &failures.Items[0]
			Expect(metav1.GetControllerOf(failed)).NotTo(BeNil())
			Expect(metav1.GetControllerOf(failed).UID).To(Equal(child.UID))
			Expect(failed.Status.GetCondition(apis.ConditionSucceeded)).NotTo(BeNil())
			Expect(failed.Status.GetCondition(apis.ConditionSucceeded).Status).To(Equal(corev1.ConditionFalse))
			Expect(failed.Status.Steps).To(HaveLen(1))
			Expect(failed.Status.Steps[0].Terminated).NotTo(BeNil())
			Expect(failed.Status.Steps[0].Terminated.ExitCode).To(Equal(int32(7)))
			after := nestedTask(ctx, parent, "after-child")
			Expect(after.Status.GetCondition(apis.ConditionSucceeded)).NotTo(BeNil())
			Expect(after.Status.GetCondition(apis.ConditionSucceeded).Status).To(Equal(corev1.ConditionTrue))
			Expect(after.Status.Results).To(ContainElement(v1.TaskRunResult{
				Name: "continued", Type: v1.ResultsTypeString, Value: *v1.NewStructuredValues("continued-after-child"),
			}))
			Expect(child.Status.CompletionTime).NotTo(BeNil())
			Expect(after.Status.StartTime).NotTo(BeNil())
			Expect(after.Status.StartTime.Time.Before(child.Status.CompletionTime.Time)).To(BeFalse())
			Expect(parent.Status.SkippedTasks).To(BeEmpty())
			expectNestedRuns(ctx, parent, child)
			tasks, err := sharedClients.TaskRunClient.List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks.Items).To(ConsistOf(HaveField("UID", failed.UID), HaveField("UID", after.UID)))
		})
	})

	Describe("PIPELINES-49-TC05", func() {
		It("fails an indirect A to B to A cycle in B before creating another A run", SpecTimeout(12*time.Minute), func(ctx SpecContext) {
			// Given named Pipelines A and B referencing each other.
			// When root A creates B and B attempts to create A.
			parent := runNestedPipeline(ctx, "testdata/pipelines-in-pipelines/negative/indirect-cycle.yaml")

			// Then B owns the cycle diagnostic and A fails from its failed child.
			expectNestedState(parent, corev1.ConditionFalse)
			Expect(parent.Spec.PipelineRef.Name).To(Equal("pip-cycle-a"))
			Expect(parent.Status.GetCondition(apis.ConditionSucceeded).Reason).To(Equal(v1.PipelineRunReasonFailed.String()))
			child := nestedChild(ctx, parent, "child-b")
			expectNestedState(child, corev1.ConditionFalse)
			Expect(child.Spec.PipelineRef.Name).To(Equal("pip-cycle-b"))
			condition := child.Status.GetCondition(apis.ConditionSucceeded)
			Expect(condition.Reason).To(Equal(v1.PipelineRunReasonCreateRunFailed.String()))
			Expect(condition.Message).To(ContainSubstring(`detected cycle in pipeline-in-pipeline: pipeline "pip-cycle-a" is already running in ancestor chain [pip-cycle-b]`))
			Expect(nestedChild(ctx, child, "child-a")).To(BeNil())
			expectNestedRuns(ctx, parent, child)
			tasks, err := sharedClients.TaskRunClient.List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks.Items).To(BeEmpty(), "cycle detection must not execute ordinary tasks")
		})
	})
})
