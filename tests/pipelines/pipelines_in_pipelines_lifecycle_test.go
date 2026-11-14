package pipelines_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // Ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // Gomega convention
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/apis"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

func awaitNestedParent(ctx context.Context, stream watch.Interface, accept func(*v1.PipelineRun) bool) *v1.PipelineRun {
	for {
		select {
		case <-ctx.Done():
			Fail("PipelineRun observation ended: " + ctx.Err().Error())
			return nil
		case event, open := <-stream.ResultChan():
			Expect(open).To(BeTrue(), "PipelineRun watch closed before the expected state")
			if event.Type == watch.Error {
				Expect(apierrors.FromObject(event.Object)).NotTo(HaveOccurred())
			}
			Expect(event.Type).NotTo(Equal(watch.Deleted), "PipelineRun disappeared before the expected state")
			if run, ok := event.Object.(*v1.PipelineRun); ok && accept(run) {
				return run
			}
		}
	}
}

func awaitNestedRunningPod(ctx context.Context, stream watch.Interface) *corev1.Pod {
	for {
		select {
		case <-ctx.Done():
			Fail("nested leaf did not start: " + ctx.Err().Error())
			return nil
		case event, open := <-stream.ResultChan():
			Expect(open).To(BeTrue(), "Pod watch closed before the nested leaf started")
			if event.Type == watch.Error {
				Expect(apierrors.FromObject(event.Object)).NotTo(HaveOccurred())
			}
			if pod, ok := event.Object.(*corev1.Pod); ok && event.Type != watch.Deleted && pod.Status.Phase == corev1.PodRunning {
				return pod
			}
		}
	}
}

func stopNestedWorkload(ctx context.Context) {
	runs, err := sharedClients.PipelineRunClient.List(ctx, metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	owners := make([]types.UID, len(runs.Items))
	for i, run := range runs.Items {
		owners[i] = run.UID
	}
	tasks, err := pipelines.CancelNestedTaskRuns(ctx, sharedClients.TaskRunClient, owners)
	Expect(err).NotTo(HaveOccurred())
	Expect(pipelines.WaitNestedPodsStopped(ctx, sharedClients.KubeClient.Kube.CoreV1().Pods(lastNamespace), tasks)).To(Succeed())
}

var _ = Describe("Pipelines-in-Pipelines lifecycle limitations: PIPELINES-50", Ordered, Serial,
	Label("e2e", "pipelines", "pipelines-in-pipelines", "admin"), func() {
		BeforeAll(func(ctx SpecContext) { requireNestedPipelineFeature(ctx) })
		for _, tc := range []struct {
			id     string
			reason v1.PipelineRunReason
			cancel bool
		}{
			{"PIPELINES-50-TC01", v1.PipelineRunReasonCouldntTimeOut, false},
			{"PIPELINES-50-TC02", v1.PipelineRunReasonCouldntCancel, true},
		} {
			Describe(tc.id, func() {
				It("reports "+tc.reason.String()+" instead of claiming nested cascade support", SpecTimeout(10*time.Minute), func(ctx SpecContext) {
					parents := sharedClients.PipelineRunClient
					options := metav1.ListOptions{FieldSelector: "metadata.name=pip-parent"}
					list, err := parents.List(ctx, options)
					Expect(err).NotTo(HaveOccurred())
					Expect(list.Items).To(BeEmpty())
					options.ResourceVersion = list.ResourceVersion
					stream, err := parents.Watch(ctx, options)
					Expect(err).NotTo(HaveOccurred())
					defer stream.Stop()
					pods := sharedClients.KubeClient.Kube.CoreV1().Pods(lastNamespace)
					podOptions := metav1.ListOptions{LabelSelector: "tekton.dev/pipelineTask=leaf"}
					podList, err := pods.List(ctx, podOptions)
					Expect(err).NotTo(HaveOccurred())
					Expect(podList.Items).To(BeEmpty())
					podOptions.ResourceVersion = podList.ResourceVersion
					podStream, err := pods.Watch(ctx, podOptions)
					Expect(err).NotTo(HaveOccurred())
					defer podStream.Stop()
					DeferCleanup(func(cleanupCtx SpecContext) { stopNestedWorkload(cleanupCtx) }, NodeTimeout(5*time.Minute))
					oc.Create("testdata/pipelines-in-pipelines/negative/lifecycle.yaml", lastNamespace)

					// Require an executing descendant before observing timeout or requesting cancellation.
					startup, cancelStartup := context.WithTimeout(ctx, 2*time.Minute)
					defer cancelStartup()
					pod := awaitNestedRunningPod(startup, podStream)
					owner := metav1.GetControllerOf(pod)
					Expect(owner).NotTo(BeNil())
					Expect(owner.Kind).To(Equal("TaskRun"))
					leaf, err := sharedClients.TaskRunClient.Get(startup, owner.Name, metav1.GetOptions{})
					Expect(err).NotTo(HaveOccurred())
					Expect(leaf.UID).To(Equal(owner.UID))
					parent := awaitNestedParent(startup, stream, func(run *v1.PipelineRun) bool {
						return len(run.Status.ChildReferences) != 0
					})
					child := nestedChild(startup, parent, "child")
					Expect(child).NotTo(BeNil())
					Expect(metav1.GetControllerOf(leaf)).NotTo(BeNil())
					Expect(metav1.GetControllerOf(leaf).UID).To(Equal(child.UID))
					if tc.cancel {
						patch := []byte(fmt.Sprintf(`{"spec":{"status":%q}}`, v1.PipelineRunSpecStatusCancelled))
						_, err = parents.Patch(ctx, parent.Name, types.MergePatchType, patch, metav1.PatchOptions{})
						Expect(err).NotTo(HaveOccurred())
					}
					parent = awaitNestedParent(ctx, stream, func(run *v1.PipelineRun) bool {
						condition := run.Status.GetCondition(apis.ConditionSucceeded)
						return condition != nil && condition.Reason == tc.reason.String()
					})
					expectNestedState(parent, corev1.ConditionUnknown)
					Expect(parent.Status.GetCondition(apis.ConditionSucceeded).Message).To(ContainSubstring("found child objects of unknown kinds"))
					child, err = parents.Get(ctx, child.Name, metav1.GetOptions{})
					Expect(err).NotTo(HaveOccurred())
					expectNestedState(child, corev1.ConditionUnknown)
					Expect(child.Spec.Status).NotTo(Equal(v1.PipelineRunSpecStatusCancelled))
					active, err := pods.Get(ctx, pod.Name, metav1.GetOptions{})
					Expect(err).NotTo(HaveOccurred())
					Expect(active.Status.Phase).To(Equal(corev1.PodRunning), "the current controller does not stop the descendant")
					expectNestedRuns(ctx, parent, child)
				})
			})
		}
	})
