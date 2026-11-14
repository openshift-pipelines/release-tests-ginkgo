package pipelines_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // Ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // Gomega convention
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Pipelines-in-Pipelines parameters and results: PIPELINES-48", Ordered, Serial, Label("e2e", "pipelines", "pipelines-in-pipelines", "admin"), func() {
	BeforeAll(func(ctx SpecContext) { requireNestedPipelineFeature(ctx) })

	for _, tc := range []struct {
		id, fixture, observed string
		tasks                 []string
	}{
		{"PIPELINES-48-TC01", "parameter-defaults", "child-default-value", []string{"leaf"}},
		{"PIPELINES-48-TC02", "parameter-override", "parent-override-value", []string{"leaf"}},
		{"PIPELINES-48-TC03", "structured-params", "array[1]=<alpha beta>\narray[2]=<  padded  >\narray[3]=<>\narray[4]=<tab\tvalue>\nobject.left=<left right>\nobject.right=<  object padded  >\n", []string{"leaf"}},
		{"PIPELINES-48-TC04", "child-results", "child-produced value", []string{"producer", "consumer"}},
	} {
		Describe(tc.id, func() {
			It("runs "+tc.fixture, SpecTimeout(12*time.Minute), func(ctx SpecContext) {
				bounded, cancel := context.WithTimeout(ctx, 10*time.Minute)
				defer cancel()

				parent := runNestedPipeline(bounded, "testdata/pipelines-in-pipelines/positive/"+tc.fixture+".yaml")
				expectNestedState(parent, corev1.ConditionTrue)
				child := nestedChild(bounded, parent, "child")
				expectNestedState(child, corev1.ConditionTrue)

				taskUIDs := make([]types.UID, 0, len(tc.tasks))
				taskRuns := make([]*v1.TaskRun, 0, len(tc.tasks))
				for _, name := range tc.tasks {
					task := nestedTask(bounded, child, name)
					Expect(task.Status.Results).To(ContainElement(v1.TaskRunResult{
						Name: "observed", Type: v1.ResultsTypeString, Value: *v1.NewStructuredValues(tc.observed),
					}))
					taskUIDs = append(taskUIDs, task.UID)
					taskRuns = append(taskRuns, task)
				}
				if tc.fixture == "parameter-defaults" {
					Expect(child.Spec.Params).To(BeEmpty(), "the child Pipeline must supply the omitted default")
				}
				if tc.fixture == "structured-params" {
					expected := []v1.Param{
						{Name: "items", Value: v1.ParamValue{Type: v1.ParamTypeArray, ArrayVal: []string{"alpha beta", "  padded  ", "", "tab\tvalue"}}},
						{Name: "details", Value: v1.ParamValue{Type: v1.ParamTypeObject, ObjectVal: map[string]string{"left": "left right", "right": "  object padded  "}}},
					}
					Expect(child.Spec.Params).To(ConsistOf(expected))
					Expect(taskRuns[0].Spec.Params).To(ConsistOf(expected))
				}
				if tc.fixture == "child-results" {
					producer, consumer := taskRuns[0], taskRuns[1]
					Expect(consumer.Status.StartTime).NotTo(BeNil())
					Expect(consumer.Status.StartTime.Time.Before(producer.Status.CompletionTime.Time)).To(BeFalse(),
						"the result edge must order consumer after producer without runAfter")
				}

				expectNestedRuns(bounded, parent, child)
				runs, err := sharedClients.TaskRunClient.List(bounded, metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				actual := make([]types.UID, len(runs.Items))
				for i, run := range runs.Items {
					actual[i] = run.UID
				}
				Expect(actual).To(ConsistOf(taskUIDs), "unexpected TaskRuns outside the child-local tasks")
			})
		})
	}
})
