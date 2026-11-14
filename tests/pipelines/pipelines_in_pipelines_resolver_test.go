package pipelines_test

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/yaml"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

var _ = Describe("Pipelines-in-Pipelines cluster resolution: PIPELINES-47", Ordered, Serial,
	Label("e2e", "pipelines", "pipelines-in-pipelines", "admin", "no-auto-namespace"), func() {
		var env nestedResolverEnvironment
		BeforeAll(func(ctx SpecContext) {
			env = setupNestedResolver(ctx)
		}, NodeTimeout(10*time.Minute))

		for _, tc := range []struct {
			id, source, denied string
		}{
			{"PIPELINES-47-TC01", "same", ""},
			{"PIPELINES-47-TC02", "allowed", ""},
			{"PIPELINES-47-TC03", "blocked", "is blocked"},
			{"PIPELINES-47-TC04", "unlisted", "is not allowed"},
		} {
			It(tc.id+" resolves a "+tc.source+" namespace under an explicit policy", func(ctx SpecContext) {
				source := env.sources[tc.source]
				target := types.NamespacedName{Namespace: source.Namespace, Name: source.Name}
				By(fmt.Sprintf("Check resolver GET access and leaf denial for Pipeline %s/%s (UID %s, resourceVersion %s)",
					source.Namespace, source.Name, source.UID, source.ResourceVersion))
				allowed, err := pipelines.NestedPipelineReadAllowed(ctx, sharedClients.KubeClient.Kube, env.identity, target)
				Expect(err).NotTo(HaveOccurred())
				Expect(allowed).To(BeTrue(), "resolver RBAC must allow GET even when namespace policy denies resolution")
				allowed, err = pipelines.NestedPipelineReadAllowed(ctx, sharedClients.KubeClient.Kube,
					types.NamespacedName{Namespace: env.namespace, Name: "pip-leaf"}, target)
				Expect(err).NotTo(HaveOccurred())
				Expect(allowed).To(BeFalse(), "leaf service account must not have permission to fetch the source Pipeline")

				parent := submitNestedResolverRun(ctx, env.namespace, "pip-resolver-"+tc.source, target)
				condition := parent.Status.GetCondition(apis.ConditionSucceeded)
				Expect(condition).NotTo(BeNil())
				children := sharedClients.Tekton.TektonV1().PipelineRuns(env.namespace)
				child, err := pipelines.NestedChild(ctx, children, parent, "child")
				Expect(err).NotTo(HaveOccurred())
				if tc.denied != "" {
					expectNestedState(parent, corev1.ConditionFalse)
					Expect(condition.Reason).To(Equal(v1.PipelineRunReasonCouldntGetPipeline.String()))
					Expect(condition.Message).To(ContainSubstring("access to specified namespace " + source.Namespace + " " + tc.denied))
					Expect(child).To(BeNil(), "policy denial must happen before child execution")
					Expect(parent.Status.ChildReferences).To(BeEmpty())
				} else {
					expectNestedState(parent, corev1.ConditionTrue)
					expectNestedState(child, corev1.ConditionTrue)
					Expect(child.Status.PipelineSpec).To(Equal(&source.Spec), "execute the test-owned source, not a different Pipeline")
					leaf, err := pipelines.NestedTask(ctx, sharedClients.Tekton.TektonV1().TaskRuns(env.namespace), child, "leaf")
					Expect(err).NotTo(HaveOccurred())
					Expect(leaf.Spec.ServiceAccountName).To(Equal("pip-leaf"))
					Expect(leaf.Status.Results).To(ContainElement(v1.TaskRunResult{
						Name: "source", Type: v1.ResultsTypeString, Value: *v1.NewStructuredValues("pinned-cluster-pipeline-" + tc.source),
					}))
					pod, err := sharedClients.KubeClient.Kube.CoreV1().Pods(env.namespace).Get(ctx, leaf.Status.PodName, metav1.GetOptions{})
					Expect(err).NotTo(HaveOccurred())
					Expect(pod.Spec.ServiceAccountName).To(Equal("pip-leaf"), "verify the actual execution identity")
					Expect(metav1.GetControllerOf(pod)).NotTo(BeNil())
					Expect(metav1.GetControllerOf(pod).UID).To(Equal(leaf.UID))
				}
				runs, err := children.List(ctx, metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun=" + parent.Name})
				Expect(err).NotTo(HaveOccurred())
				tasks, err := sharedClients.Tekton.TektonV1().TaskRuns(env.namespace).List(ctx,
					metav1.ListOptions{LabelSelector: "tekton.dev/pipelineRun=" + parent.Name})
				Expect(err).NotTo(HaveOccurred())
				Expect(tasks.Items).To(BeEmpty(), "the parent must not execute a leaf TaskRun itself")
				if tc.denied != "" {
					Expect(runs.Items).To(BeEmpty())
				} else {
					Expect(runs.Items).To(HaveLen(1))
					Expect(runs.Items[0].UID).To(Equal(child.UID))
				}
				current, err := sharedClients.Tekton.TektonV1().Pipelines(source.Namespace).Get(ctx, source.Name, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				Expect(current.UID).To(Equal(source.UID))
				Expect(current.ResourceVersion).To(Equal(source.ResourceVersion), "cluster resolution has no immutable revision parameter")
				Expect(current.Spec).To(Equal(source.Spec))
			}, SpecTimeout(12*time.Minute))
		}
	})

func submitNestedResolverRun(ctx context.Context, namespace, name string, source types.NamespacedName) *v1.PipelineRun {
	bounded, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	client := sharedClients.Tekton.TektonV1().PipelineRuns(namespace)
	options := metav1.ListOptions{FieldSelector: "metadata.name=" + name}
	list, err := client.List(bounded, options)
	Expect(err).NotTo(HaveOccurred())
	Expect(list.Items).To(BeEmpty())
	options.ResourceVersion = list.ResourceVersion
	stream, err := client.Watch(bounded, options)
	Expect(err).NotTo(HaveOccurred())
	defer stream.Stop()
	By(fmt.Sprintf("Resolve Pipeline %s/%s as a child of %s/%s", source.Namespace, source.Name, namespace, name))
	_, err = client.Create(bounded, &v1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1.PipelineRunSpec{
			TaskRunTemplate: v1.PipelineTaskRunTemplate{ServiceAccountName: "pip-leaf"},
			PipelineSpec: &v1.PipelineSpec{Tasks: []v1.PipelineTask{{
				Name: "child",
				PipelineRef: &v1.PipelineRef{ResolverRef: v1.ResolverRef{
					Resolver: "cluster",
					Params: v1.Params{
						{Name: "kind", Value: *v1.NewStructuredValues("pipeline")},
						{Name: "name", Value: *v1.NewStructuredValues(source.Name)},
						{Name: "namespace", Value: *v1.NewStructuredValues(source.Namespace)},
					},
				}},
			}}},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	run, err := pipelines.ReadNestedWatch(bounded, stream.ResultChan(), name)
	if errors.Is(err, pipelines.ErrNestedWatchClosed) || apierrors.IsResourceExpired(err) {
		run, err = pipelines.ResumeNestedPipelineRun(bounded, client, name)
	}
	Expect(err).NotTo(HaveOccurred())
	return run
}

type nestedResolverEnvironment struct {
	namespace string
	identity  types.NamespacedName
	sources   map[string]*v1.Pipeline
}

func setupNestedResolver(ctx SpecContext) nestedResolverEnvironment {
	requireNestedPipelineFeature(ctx)
	env := nestedResolverEnvironment{
		identity: types.NamespacedName{Namespace: cmp.Or(os.Getenv("TEKTON_PIPELINES_NAMESPACE"), config.TargetNamespace)},
		sources:  make(map[string]*v1.Pipeline),
	}
	data, err := os.ReadFile(config.Path("testdata/pipelines-in-pipelines/resolver/child.yaml"))
	Expect(err).NotTo(HaveOccurred())
	var fixture v1.Pipeline
	Expect(yaml.Unmarshal(data, &fixture)).To(Succeed())
	for _, scenario := range []string{"same", "allowed", "blocked", "unlisted"} {
		ns, err := sharedClients.KubeClient.Kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "pip-resolver-" + scenario + "-"},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(cleanupCtx SpecContext) {
			Expect(sharedClients.KubeClient.Kube.CoreV1().Namespaces().Delete(cleanupCtx, ns.Name,
				metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &ns.UID}})).To(Succeed())
		}, NodeTimeout(time.Minute))
		input := fixture.DeepCopy()
		input.Spec.Tasks[0].TaskSpec.Steps[0].Env[0].Value = "pinned-cluster-pipeline-" + scenario
		source, err := sharedClients.Tekton.TektonV1().Pipelines(ns.Name).Create(ctx, input, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		env.sources[scenario] = source
	}
	env.namespace = env.sources["same"].Namespace
	_, err = sharedClients.KubeClient.Kube.CoreV1().ServiceAccounts(env.namespace).Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "pip-leaf"},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	tc, err := sharedClients.TektonConfig().Get(ctx, "config", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	cms := sharedClients.KubeClient.Kube.CoreV1().ConfigMaps(env.identity.Namespace)
	oldPolicy, err := cms.Get(ctx, "cluster-resolver-config", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	oldFlags, err := cms.Get(ctx, "resolvers-feature-flags", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	restorePolicy := map[string]interface{}{"allowed-namespaces": nil, "blocked-namespaces": nil}
	for key := range restorePolicy {
		if value, exists := tc.Spec.Pipeline.ClusterResolverConfig[key]; exists {
			restorePolicy[key] = value
		}
	}
	DeferCleanup(func(cleanupCtx SpecContext) {
		By("Restore the original cluster resolver policy and reload the resolver pods")
		applyNestedResolverPolicy(cleanupCtx, env.identity.Namespace, map[string]interface{}{
			"enable-cluster-resolver": tc.Spec.Pipeline.EnableClusterResolver,
			"cluster-resolver-config": restorePolicy,
		}, map[string]string{
			"allowed-namespaces": oldPolicy.Data["allowed-namespaces"],
			"blocked-namespaces": oldPolicy.Data["blocked-namespaces"],
		}, oldFlags.Data["enable-cluster-resolver"])
	}, NodeTimeout(6*time.Minute))
	policy := map[string]string{
		"allowed-namespaces": strings.Join([]string{env.namespace, env.sources["allowed"].Namespace, env.sources["blocked"].Namespace}, ","),
		"blocked-namespaces": env.sources["blocked"].Namespace,
	}
	By("Allow only test-owned source namespaces and explicitly block one allowed namespace")
	env.identity.Name = applyNestedResolverPolicy(ctx, env.identity.Namespace, map[string]interface{}{
		"enable-cluster-resolver": true,
		"cluster-resolver-config": policy,
	}, policy, "true")
	By(fmt.Sprintf("Resolver identity is system:serviceaccount:%s:%s; leaf identity is system:serviceaccount:%s:pip-leaf",
		env.identity.Namespace, env.identity.Name, env.namespace))
	Expect(env.identity).NotTo(Equal(types.NamespacedName{Namespace: env.namespace, Name: "pip-leaf"}))
	return env
}

func applyNestedResolverPolicy(ctx context.Context, namespace string, fields map[string]interface{}, policy map[string]string, enabled string) string {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	patch, err := json.Marshal(map[string]interface{}{"spec": map[string]interface{}{"pipeline": fields}})
	Expect(err).NotTo(HaveOccurred())
	_, err = sharedClients.TektonConfig().Patch(bounded, "config", types.MergePatchType, patch, metav1.PatchOptions{})
	Expect(err).NotTo(HaveOccurred())
	cms := sharedClients.KubeClient.Kube.CoreV1().ConfigMaps(namespace)
	Expect(pipelines.WaitNestedResolverConfig(bounded, cms, "cluster-resolver-config", policy)).To(Succeed())
	Expect(pipelines.WaitNestedResolverConfig(bounded, cms, "resolvers-feature-flags",
		map[string]string{"enable-cluster-resolver": enabled})).To(Succeed())
	identity, err := pipelines.RestartNestedResolvers(bounded, sharedClients.KubeClient.Kube, namespace)
	Expect(err).NotTo(HaveOccurred())
	return identity
}
