package pipelines_test

import (
	"cmp"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/hooks"
)

var sharedClients *clients.Clients

// lastNamespace tracks the current test namespace for hooks-based namespace isolation.
// Set automatically by hooks.AutoNamespacePerDescribe().
var lastNamespace string

func TestPipelines(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Pipelines Suite", Label("pipelines"))
}

// clientConfig holds the serialized configuration passed between parallel nodes.
type clientConfig struct {
	Kubeconfig      string `json:"kubeconfig"`
	Cluster         string `json:"cluster"`
	Context         string `json:"context"`
	TargetNamespace string `json:"targetNamespace"`
}

var _ = SynchronizedBeforeSuite(
	// Node 1 only: validate cluster connectivity and serialize config
	func() []byte {
		// Verify cluster is reachable by creating clients
		cs, err := clients.NewClientsWithContext(
			config.Flags.Kubeconfig,
			config.Flags.Cluster,
			config.Flags.Context,
			config.TargetNamespace,
		)
		Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes clients on node 1")
		_ = cs // validation only on node 1

		cfg := clientConfig{
			Kubeconfig:      config.Flags.Kubeconfig,
			Cluster:         config.Flags.Cluster,
			Context:         config.Flags.Context,
			TargetNamespace: config.TargetNamespace,
		}
		data, err := json.Marshal(cfg)
		Expect(err).NotTo(HaveOccurred(), "Failed to serialize client config")
		return data
	},
	// All nodes: deserialize config and create node-local clients
	func(data []byte) {
		var cfg clientConfig
		Expect(json.Unmarshal(data, &cfg)).To(Succeed(), "Failed to deserialize client config")

		var err error
		sharedClients, err = clients.NewClientsWithContext(cfg.Kubeconfig, cfg.Cluster, cfg.Context, cfg.TargetNamespace)
		Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes clients")
	},
)

// Enable namespace isolation for parallel test execution in CI.
// Each Describe block gets its own namespace automatically.
var _ = hooks.AutoNamespacePerDescribe(&lastNamespace, func() *clients.Clients { return sharedClients })

var _ = AfterSuite(func() {
	hooks.CleanupNamespaces()
	CleanupClusterResolverNamespaces() // Cleanup shared namespaces for cluster resolver tests
	// The failure probe shares this repository scratch directory with its parent.
	if os.Getenv("PIP_FAILURE_PROBE") != "1" {
		_ = config.RemoveTempDir()
	}
})

func requireNestedPipelineFeature(ctx context.Context) {
	By("Enable alpha Pipeline fields and inline specs for this serial test group")
	tc, err := sharedClients.TektonConfig().Get(ctx, "config", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	namespace := cmp.Or(os.Getenv("TEKTON_PIPELINES_NAMESPACE"), config.TargetNamespace)
	flags, err := sharedClients.KubeClient.Kube.CoreV1().ConfigMaps(namespace).Get(ctx, "feature-flags", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	original := map[string]interface{}{
		"enable-api-fields":   nil,
		"disable-inline-spec": nil,
	}
	if tc.Spec.Pipeline.EnableApiFields != "" {
		original["enable-api-fields"] = tc.Spec.Pipeline.EnableApiFields
	}
	if tc.Spec.Pipeline.DisableInlineSpec != "" {
		original["disable-inline-spec"] = tc.Spec.Pipeline.DisableInlineSpec
	}
	DeferCleanup(func(cleanupCtx SpecContext) {
		By("Restore the original Pipeline feature configuration")
		setNestedFlags(cleanupCtx, original, flags.Data)
	}, NodeTimeout(5*time.Minute))
	setNestedFlags(ctx, map[string]interface{}{"enable-api-fields": "alpha", "disable-inline-spec": "false"},
		map[string]string{"enable-api-fields": "alpha", "disable-inline-spec": "false"})
}

func setNestedFlags(ctx context.Context, fields map[string]interface{}, expected map[string]string) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	namespace := cmp.Or(os.Getenv("TEKTON_PIPELINES_NAMESPACE"), config.TargetNamespace)
	client := sharedClients.KubeClient.Kube.CoreV1().ConfigMaps(namespace)
	list, err := client.List(ctx, metav1.ListOptions{FieldSelector: "metadata.name=feature-flags"})
	Expect(err).NotTo(HaveOccurred())
	stream, err := client.Watch(ctx, metav1.ListOptions{FieldSelector: "metadata.name=feature-flags", ResourceVersion: list.ResourceVersion})
	Expect(err).NotTo(HaveOccurred())
	defer stream.Stop()
	patch, err := json.Marshal(map[string]interface{}{"spec": map[string]interface{}{"pipeline": fields}})
	Expect(err).NotTo(HaveOccurred())
	_, err = sharedClients.TektonConfig().Patch(ctx, "config", types.MergePatchType, patch, metav1.PatchOptions{})
	Expect(err).NotTo(HaveOccurred())
	matches := func(cm *corev1.ConfigMap) bool {
		return cm.Data["enable-api-fields"] == expected["enable-api-fields"] &&
			cm.Data["disable-inline-spec"] == expected["disable-inline-spec"]
	}
	for i := range list.Items {
		if matches(&list.Items[i]) {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			Fail("feature-flags did not reconcile: " + ctx.Err().Error())
			return
		case event, open := <-stream.ResultChan():
			Expect(open).To(BeTrue(), "feature-flags watch closed before reconciliation")
			if event.Type == watch.Error {
				Expect(apierrors.FromObject(event.Object)).NotTo(HaveOccurred())
			}
			if cm, ok := event.Object.(*corev1.ConfigMap); ok && event.Type != watch.Deleted && matches(cm) {
				return
			}
		}
	}
}
