package pipelines_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // Ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // Gomega convention
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/apis"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

func requireNestedPipelineFeature(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	namespace := cmp.Or(os.Getenv("TEKTON_PIPELINES_NAMESPACE"), config.TargetNamespace)
	flags, err := sharedClients.KubeClient.Kube.CoreV1().ConfigMaps(namespace).Get(ctx, "feature-flags", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		Skip("Pipelines-in-Pipelines prerequisite: feature-flags ConfigMap and enable-api-fields=alpha are required")
	}
	Expect(err).NotTo(HaveOccurred(), "read Pipelines-in-Pipelines prerequisites")
	if flags.Data["enable-api-fields"] != "alpha" {
		Skip("Pipelines-in-Pipelines prerequisite: enable-api-fields=alpha is required")
	}
	if flags.Data["disable-inline-spec"] != "" && flags.Data["disable-inline-spec"] != "false" {
		Skip("Pipelines-in-Pipelines prerequisite: inline PipelineSpecs and TaskSpecs must be enabled")
	}
}

func readNestedWatch(ctx context.Context, events <-chan watch.Event, name string) (*v1.PipelineRun, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("watch PipelineRun %s: %w", name, ctx.Err())
		case event, open := <-events:
			if !open {
				return nil, fmt.Errorf("PipelineRun %s watch closed before completion", name)
			}
			if event.Type == watch.Error {
				return nil, fmt.Errorf("watch PipelineRun %s: %w", name, apierrors.FromObject(event.Object))
			}
			run, ok := event.Object.(*v1.PipelineRun)
			if !ok || run.Name != name {
				continue
			}
			if event.Type == watch.Deleted {
				return nil, fmt.Errorf("PipelineRun %s deleted before completion", name)
			}
			condition := run.Status.GetCondition(apis.ConditionSucceeded)
			if condition != nil && (condition.Status == corev1.ConditionTrue || condition.Status == corev1.ConditionFalse) {
				return run, nil
			}
		}
	}
}

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
	oc.Create(fixture, lastNamespace)
	run, err := readNestedWatch(ctx, stream.ResultChan(), "pip-parent")
	Expect(err).NotTo(HaveOccurred())
	return run
}

func nestedChild(ctx context.Context, parent *v1.PipelineRun, task string) *v1.PipelineRun {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	list, err := sharedClients.PipelineRunClient.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineTask=" + task})
	Expect(err).NotTo(HaveOccurred())
	var child *v1.PipelineRun
	for i := range list.Items {
		candidate := &list.Items[i]
		if owner := metav1.GetControllerOf(candidate); owner != nil && owner.UID == parent.UID {
			Expect(child).To(BeNil(), "only one immediate child should execute task %s", task)
			child = candidate
		}
	}
	return child
}

func expectNestedState(run *v1.PipelineRun, status corev1.ConditionStatus) {
	Expect(run).NotTo(BeNil())
	Expect(run.Status.GetCondition(apis.ConditionSucceeded)).NotTo(BeNil())
	Expect(run.Status.GetCondition(apis.ConditionSucceeded).Status).To(Equal(status), "PipelineRun %s: %+v", run.Name, run.Status.Conditions)
	if status == corev1.ConditionTrue {
		Expect(run.Status.StartTime).NotTo(BeNil())
		Expect(run.Status.CompletionTime).NotTo(BeNil())
	}
}

func nestedTask(ctx context.Context, parent *v1.PipelineRun, task string) *v1.TaskRun {
	list, err := sharedClients.TaskRunClient.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineTask=" + task})
	Expect(err).NotTo(HaveOccurred())
	for i := range list.Items {
		run := &list.Items[i]
		if owner := metav1.GetControllerOf(run); owner != nil && owner.UID == parent.UID {
			Expect(run.Status.GetCondition(apis.ConditionSucceeded)).NotTo(BeNil())
			Expect(run.Status.GetCondition(apis.ConditionSucceeded).Status).To(Equal(corev1.ConditionTrue))
			Expect(run.Status.CompletionTime).NotTo(BeNil())
			return run
		}
	}
	Fail("missing owned TaskRun for " + task)
	return nil
}

var _ = Describe("Pipelines-in-Pipelines: PIPELINES-44", Label("e2e", "pipelines", "pipelines-in-pipelines", "non-admin"), func() {
	for _, tc := range []struct {
		id, fixture string
		check       func(context.Context, *v1.PipelineRun)
	}{
		{"PIPELINES-44-TC01", "inline", func(ctx context.Context, parent *v1.PipelineRun) {
			child := nestedChild(ctx, parent, "child")
			expectNestedState(child, corev1.ConditionTrue)
			Expect(child.Labels).To(HaveKeyWithValue("pip.example.test/scenario", "inline"))
			Expect(child.Annotations).To(HaveKeyWithValue("pip.example.test/metadata", "propagated"))
			nestedTask(ctx, child, "leaf")
		}},
		{"PIPELINES-44-TC02", "named", func(ctx context.Context, parent *v1.PipelineRun) {
			child := nestedChild(ctx, parent, "child")
			expectNestedState(child, corev1.ConditionTrue)
			Expect(child.Spec.PipelineRef).NotTo(BeNil())
			Expect(child.Spec.PipelineRef.Name).To(Equal("pip-child"))
			Expect(child.Spec.TaskRunTemplate.ServiceAccountName).To(Equal("pip-runner"))
			Expect(nestedTask(ctx, child, "leaf").Spec.ServiceAccountName).To(Equal("pip-runner"))
		}},
		{"PIPELINES-44-TC03", "mixed", func(ctx context.Context, parent *v1.PipelineRun) {
			referenced, inline := nestedChild(ctx, parent, "referenced"), nestedChild(ctx, parent, "inline")
			expectNestedState(referenced, corev1.ConditionTrue)
			expectNestedState(inline, corev1.ConditionTrue)
			grandchild := nestedChild(ctx, inline, "grandchild")
			expectNestedState(grandchild, corev1.ConditionTrue)
			nestedTask(ctx, referenced, "leaf")
			nestedTask(ctx, grandchild, "leaf")
			regular := nestedTask(ctx, parent, "regular")
			Expect(referenced.Status.StartTime.Time.Before(regular.Status.CompletionTime.Time)).To(BeFalse())
			Expect(inline.Status.StartTime.Time.Before(referenced.Status.CompletionTime.Time)).To(BeFalse())
		}},
		{"PIPELINES-44-TC04", "workspace", func(ctx context.Context, parent *v1.PipelineRun) {
			child := nestedChild(ctx, parent, "child")
			expectNestedState(child, corev1.ConditionTrue)
			Expect(child.Spec.Workspaces).To(ContainElement(HaveField("PersistentVolumeClaim.ClaimName", "pip-data")))
			nestedTask(ctx, parent, "writer")
			nestedTask(ctx, child, "reader")
		}},
		{"PIPELINES-44-TC05", "when", func(ctx context.Context, parent *v1.PipelineRun) {
			Expect(parent.Status.SkippedTasks).To(ContainElement(HaveField("Name", "child")))
			Expect(nestedChild(ctx, parent, "child")).To(BeNil())
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
			BeforeEach(func(ctx SpecContext) { requireNestedPipelineFeature(ctx) })
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

func TestNestedWatchTerminal(t *testing.T) {
	for _, status := range []corev1.ConditionStatus{corev1.ConditionTrue, corev1.ConditionFalse} {
		t.Run(string(status), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			events := make(chan watch.Event, 5)
			events <- watch.Event{Type: watch.Bookmark, Object: &metav1.PartialObjectMetadata{}}
			events <- watch.Event{Type: watch.Added, Object: &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "parent"}}}
			unrelated := &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "unrelated"}}
			unrelated.Status.SetConditions([]apis.Condition{{Type: apis.ConditionSucceeded, Status: corev1.ConditionTrue}})
			events <- watch.Event{Type: watch.Modified, Object: unrelated}
			for _, state := range []corev1.ConditionStatus{corev1.ConditionUnknown, status} {
				run := &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "parent"}}
				run.Status.SetConditions([]apis.Condition{{Type: apis.ConditionSucceeded, Status: state}})
				events <- watch.Event{Type: watch.Modified, Object: run}
			}
			got, err := readNestedWatch(ctx, events, "parent")
			if err != nil || got == nil || got.Name != "parent" || got.Status.GetCondition(apis.ConditionSucceeded).Status != status {
				t.Fatalf("terminal %s: run=%v error=%v", status, got, err)
			}
		})
	}
}

func TestNestedWatchFailures(t *testing.T) {
	for _, kind := range []string{"error", "deleted", "closed", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			events := make(chan watch.Event, 2)
			events <- watch.Event{Type: watch.Modified, Object: &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "unrelated"}}}
			switch kind {
			case "error":
				events <- watch.Event{Type: watch.Error, Object: &metav1.Status{Status: metav1.StatusFailure, Reason: metav1.StatusReasonExpired, Code: 410}}
			case "deleted":
				events <- watch.Event{Type: watch.Deleted, Object: &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{Name: "parent"}}}
			case "closed":
				close(events)
			case "canceled":
				cancel()
			}
			run, err := readNestedWatch(ctx, events, "parent")
			if err == nil || run != nil || errors.Is(err, context.DeadlineExceeded) || (kind == "canceled" && !errors.Is(err, context.Canceled)) || (kind == "error" && !apierrors.IsResourceExpired(err)) {
				t.Fatalf("%s: run=%v error=%v", kind, run, err)
			}
		})
	}
}
