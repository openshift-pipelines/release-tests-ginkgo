package tektonkueue_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	watchtools "k8s.io/client-go/tools/watch"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pac"
	multicluster "github.com/openshift-pipelines/release-tests-ginkgo/pkg/tektonkueue"
)

var _ = Describe("PAC multi-cluster Secret synchronization", Serial, Label("tekton-kueue", "pac-sync", "admin"), func() {
	It("synchronizes the PAC generated credential and returns status and console proxy live logs", NodeTimeout(2*time.Hour), func(ctx SpecContext) {
		for _, key := range []string{"PAC_GITHUB_TOKEN", "MULTICLUSTER_CONSOLE_TOKEN_FILE", "MULTICLUSTER_CONSOLE_SESSION_FILE"} {
			Expect(os.Getenv(key)).NotTo(BeEmpty(), "required credential input %s", key)
		}
		environment := preparedEnvironment(ctx)
		Expect(environment.WaitReady(ctx)).To(Succeed())
		Expect(environment.AssertReady(ctx)).To(Succeed())
		Expect(environment.EnablePACSecretSync(ctx)).To(Succeed())
		cs, namespace := environment.Hub.Clients, environment.Namespace
		pac.SetGitHubClient(pac.InitGitHubClient())

		// The relay watch predates deployment creation; no timing window or polling.
		relay, err := cs.KubeClient.Kube.AppsV1().Deployments(namespace).Watch(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("metadata.name", "gosmee-client").String(),
		})
		Expect(err).NotTo(HaveOccurred())
		defer relay.Stop()
		smeeURL, err := pac.SetupSmeeDeployment(cs, namespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = watchtools.UntilWithoutRetry(ctx, relay, func(event watch.Event) (bool, error) {
			if event.Type == watch.Error || event.Type == watch.Deleted {
				return false, fmt.Errorf("PAC relay watch failed")
			}
			deployment, ok := event.Object.(*appsv1.Deployment)
			return ok && deployment.Status.ObservedGeneration >= deployment.Generation && deployment.Status.AvailableReplicas == 1 && deployment.Status.UpdatedReplicas == 1, nil
		})
		Expect(err).NotTo(HaveOccurred(), "PAC relay did not become ready")
		owner, repo, err := pac.SetupGitHubProject(cs, namespace, smeeURL)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(pac.CleanupPACGitHub(cs, namespace, "gosmee-client", owner, repo)).To(Succeed()) })
		// Run/Secret cleanup must finish before removing the external event source.
		DeferCleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			Expect(environment.Cleanup(cleanupCtx)).To(Succeed())
		})
		correlation := rand.Text()
		definition, err := environment.PACPipelineRun(correlation)
		Expect(err).NotTo(HaveOccurred())
		armed, err := environment.ArmPAC(ctx, correlation)
		Expect(err).NotTo(HaveOccurred())
		pullRequest, err := pac.ConfigurePreviewPipelineGitHub(ctx, owner, repo, definition)
		Expect(err).NotTo(HaveOccurred())
		result, err := armed.ObservePAC(multicluster.PACIdentity{
			Correlation: correlation, Owner: owner, Repository: repo, Commit: pullRequest.GetHead().GetSHA(), PullRequest: pullRequest.GetNumber(),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Logs).To(ContainSubstring("multi-cluster-execution-ok"))
		GinkgoWriter.Printf("PAC PipelineRun %s executed on %s; generated Secret verified without logging its payload\n", result.PipelineRun, result.Spoke)
	})
})
