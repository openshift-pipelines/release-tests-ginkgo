package triggers_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/opc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/triggers"
)

// NOTE (S4): Trigger tests use the shared config.TargetNamespace, matching the
// original Gauge behavior. This means they cannot be safely parallelized —
// concurrent tests would collide on the same namespace resources.
var _ = Describe("EventListeners", Label("triggers"), func() {

	// ============================================================
	// Basic EventListeners (to-do) -- TC01-TC06
	// These tests use the old Gauge concept format and are tagged to-do.
	// ============================================================
	Context("Basic EventListeners (to-do)", func() {
		It("PIPELINES-05-TC01: Create Eventlistener", Label("triggers"), Pending, func() {})

		It("PIPELINES-05-TC02: Create Eventlistener with github interceptor", Label("triggers"), Pending, func() {})

		It("PIPELINES-05-TC03: Create EventListener with custom interceptor", Label("triggers"), Pending, func() {})

		It("PIPELINES-05-TC04: Create EventListener with CEL interceptor with filter", Label("triggers"), Pending, func() {})

		It("PIPELINES-05-TC05: Create EventListener with CEL interceptor without filter", Label("triggers"), Pending, func() {})

		It("PIPELINES-05-TC06: Create EventListener with multiple interceptors", Label("triggers"), Pending, func() {})
	})

	// ============================================================
	// TLS EventListeners -- TC07
	// ============================================================
	It("PIPELINES-05-TC07: Create Eventlistener with TLS enabled", Label("tls", "triggers", "admin", "e2e"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns
		oc.EnableTLSConfigForEventlisteners(ns)

		oc.Create("testdata/triggers/sample-pipeline.yaml", ns)
		oc.Create("testdata/triggers/triggerbindings/triggerbinding.yaml", ns)
		oc.Create("testdata/triggers/triggertemplate/triggertemplate.yaml", ns)
		oc.Create("testdata/triggers/eventlisteners/eventlistener-embeded-binding.yaml", ns)

		_, err := opc.VerifyResourceListMatchesName("triggerbinding", "pipeline-binding", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("triggertemplate", "pipeline-template", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("eventlistener", "listener-embed-binding", ns)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "listener-embed-binding", ns) })

		routeURL := triggers.ExposeEventListenerForTLS(sharedClients, "listener-embed-binding", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "github", "push", "testdata/push.json", true)
		triggers.AssertElResponse(sharedClients, resp, "listener-embed-binding", ns)

		pipelines.ValidatePipelineRun(sharedClients, "simple-pipeline-run", "successful", ns)
	})

	// ============================================================
	// Embedded Bindings -- TC08
	// ============================================================
	It("PIPELINES-05-TC08: Create Eventlistener embedded TriggersBindings specs", Label("e2e", "triggers", "non-admin", "sanity"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns

		oc.Create("testdata/triggers/sample-pipeline.yaml", ns)
		oc.Create("testdata/triggers/triggerbindings/triggerbinding.yaml", ns)
		oc.Create("testdata/triggers/triggertemplate/triggertemplate.yaml", ns)
		oc.Create("testdata/triggers/eventlisteners/eventlistener-embeded-binding.yaml", ns)

		_, err := opc.VerifyResourceListMatchesName("triggerbinding", "pipeline-binding", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("triggertemplate", "pipeline-template", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("eventlistener", "listener-embed-binding", ns)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "listener-embed-binding", ns) })

		routeURL := triggers.ExposeEventListener(sharedClients, "listener-embed-binding", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "github", "push", "testdata/push.json", false)
		triggers.AssertElResponse(sharedClients, resp, "listener-embed-binding", ns)

		pipelines.ValidatePipelineRun(sharedClients, "simple-pipeline-run", "successful", ns)
	})

	// ============================================================
	// Embedded TriggerTemplate -- TC09
	// ============================================================
	It("PIPELINES-05-TC09: Create embedded TriggersTemplate", Label("e2e", "triggers", "non-admin", "sanity"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns

		oc.Create("testdata/triggers/triggerbindings/triggerbinding.yaml", ns)
		oc.Create("testdata/triggers/triggertemplate/embed-triggertemplate.yaml", ns)
		oc.Create("testdata/triggers/eventlisteners/eventlistener-embeded-binding.yaml", ns)

		_, err := opc.VerifyResourceListMatchesName("triggerbinding", "pipeline-binding", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("triggertemplate", "pipeline-template", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("eventlistener", "listener-embed-binding", ns)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "listener-embed-binding", ns) })

		routeURL := triggers.ExposeEventListener(sharedClients, "listener-embed-binding", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "github", "push", "testdata/push.json", false)
		triggers.AssertElResponse(sharedClients, resp, "listener-embed-binding", ns)

		pipelines.ValidatePipelineRun(sharedClients, "pipelinerun-with-taskspec-to-echo-message", "successful", ns)
	})

	// ============================================================
	// Interceptors -- TC10 (gitlab), TC11 (bitbucket)
	// ============================================================
	It("PIPELINES-05-TC10: Create Eventlistener with gitlab interceptor", Label("e2e", "triggers", "non-admin"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns

		oc.Create("testdata/triggers/gitlab/gitlab-push-listener.yaml", ns)

		_, err := opc.VerifyResourceListMatchesName("triggerbinding", "gitlab-push-binding", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("triggertemplate", "gitlab-echo-template", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("eventlistener", "gitlab-listener", ns)
		Expect(err).NotTo(HaveOccurred())

		oc.CreateSecretWithSecretToken("gitlab-secret", ns)
		oc.LinkSecretToSA("gitlab-secret", "pipeline", ns)

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "gitlab-listener", ns) })

		routeURL := triggers.ExposeEventListener(sharedClients, "gitlab-listener", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "gitlab", "Push Hook", "testdata/triggers/gitlab/gitlab-push-event.json", false)
		triggers.AssertElResponse(sharedClients, resp, "gitlab-listener", ns)

		pipelines.ValidatePipelineRun(sharedClients, "gitlab-run", "successful", ns)
	})

	It("PIPELINES-05-TC11: Create Eventlistener with bitbucket interceptor", Label("e2e", "triggers", "non-admin"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns

		oc.Create("testdata/triggers/bitbucket/bitbucket-eventlistener-interceptor.yaml", ns)

		_, err := opc.VerifyResourceListMatchesName("triggerbinding", "bitbucket-binding", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("triggertemplate", "bitbucket-template", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("eventlistener", "bitbucket-listener", ns)
		Expect(err).NotTo(HaveOccurred())

		oc.CreateSecretWithSecretToken("bitbucket-secret", ns)
		oc.LinkSecretToSA("bitbucket-secret", "pipeline", ns)

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "bitbucket-listener", ns) })

		routeURL := triggers.ExposeEventListener(sharedClients, "bitbucket-listener", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "bitbucket", "refs_changed", "testdata/triggers/bitbucket/refs-change-event.json", false)
		triggers.AssertElResponse(sharedClients, resp, "bitbucket-listener", ns)

		// Bitbucket test verifies TaskRun with Failure status (intentional, matching Gauge spec)
		pipelines.ValidateTaskRun(sharedClients, "bitbucket-run", "Failure", ns)
	})

	// ============================================================
	// ClusterTriggerBinding -- TC12, TC13, TC14
	// ============================================================
	It("PIPELINES-05-TC12: Verify Github push event with Embedded TriggerTemplate using Github-CTB", Label("e2e", "triggers", "non-admin", "sanity"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns

		oc.Create("testdata/triggers/github-ctb/Embeddedtriggertemplate-git-push.yaml", ns)
		oc.Create("testdata/triggers/github-ctb/eventlistener-ctb-git-push.yaml", ns)

		_, err := opc.VerifyResourceListMatchesName("triggertemplate", "pipeline-template-git-push", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("eventlistener", "listener-ctb-github-push", ns)
		Expect(err).NotTo(HaveOccurred())

		oc.CreateSecretWithSecretToken("github-secret", ns)
		oc.LinkSecretToSA("github-secret", "pipeline", ns)

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "listener-ctb-github-push", ns) })

		routeURL := triggers.ExposeEventListener(sharedClients, "listener-ctb-github-push", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "github", "push", "testdata/triggers/github-ctb/push.json", false)
		triggers.AssertElResponse(sharedClients, resp, "listener-ctb-github-push", ns)

		pipelines.ValidatePipelineRun(sharedClients, "pipelinerun-git-push-ctb", "successful", ns)
	})

	It("PIPELINES-05-TC13: Verify Github pull_request event with Embedded TriggerTemplate using Github-CTB", Label("e2e", "triggers", "non-admin", "sanity"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns

		oc.Create("testdata/triggers/github-ctb/Embeddedtriggertemplate-git-pr.yaml", ns)
		oc.Create("testdata/triggers/github-ctb/eventlistener-ctb-git-pr.yaml", ns)

		_, err := opc.VerifyResourceListMatchesName("triggertemplate", "pipeline-template-git-pr", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("eventlistener", "listener-clustertriggerbinding-github-pr", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("clustertriggerbinding", "github-pullreq", ns)
		Expect(err).NotTo(HaveOccurred())

		oc.CreateSecretWithSecretToken("github-secret", ns)
		oc.LinkSecretToSA("github-secret", "pipeline", ns)

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "listener-clustertriggerbinding-github-pr", ns) })

		routeURL := triggers.ExposeEventListener(sharedClients, "listener-clustertriggerbinding-github-pr", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "github", "pull_request", "testdata/triggers/github-ctb/pr.json", false)
		triggers.AssertElResponse(sharedClients, resp, "listener-clustertriggerbinding-github-pr", ns)

		pipelines.ValidatePipelineRun(sharedClients, "pipelinerun-git-pr-ctb", "successful", ns)
	})

	It("PIPELINES-05-TC14: Verify Github pr_review event with Embedded TriggerTemplate using Github-CTB", Label("e2e", "triggers", "non-admin"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns

		oc.Create("testdata/triggers/github-ctb/Embeddedtriggertemplate-git-pr-review.yaml", ns)
		oc.Create("testdata/triggers/github-ctb/eventlistener-ctb-git-pr-review.yaml", ns)

		_, err := opc.VerifyResourceListMatchesName("triggertemplate", "pipeline-template-git-pr-review", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("eventlistener", "listener-ctb-github-pr-review", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("clustertriggerbinding", "github-pullreq", ns)
		Expect(err).NotTo(HaveOccurred())

		oc.CreateSecretWithSecretToken("github-secret", ns)
		oc.LinkSecretToSA("github-secret", "pipeline", ns)

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "listener-ctb-github-pr-review", ns) })

		routeURL := triggers.ExposeEventListener(sharedClients, "listener-ctb-github-pr-review", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "github", "issue_comment", "testdata/triggers/github-ctb/issue-comment.json", false)
		triggers.AssertElResponse(sharedClients, resp, "listener-ctb-github-pr-review", ns)

		pipelines.ValidatePipelineRun(sharedClients, "pipelinerun-git-pr-review-ctb", "successful", ns)
	})

	// ============================================================
	// TriggersCRD -- TC15
	// ============================================================
	It("PIPELINES-05-TC15: Create TriggersCRD resource with CEL interceptors (overlays)", Label("e2e", "triggers", "non-admin", "sanity"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns

		oc.Create("testdata/triggers/triggersCRD/eventlistener-triggerref.yaml", ns)
		oc.Create("testdata/triggers/triggersCRD/trigger.yaml", ns)
		oc.Create("testdata/triggers/triggersCRD/triggerbindings.yaml", ns)
		oc.Create("testdata/triggers/triggersCRD/triggertemplate.yaml", ns)
		oc.Create("testdata/triggers/triggersCRD/pipeline.yaml", ns)

		_, err := opc.VerifyResourceListMatchesName("triggerbinding", "github-pr-binding", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("triggertemplate", "github-template", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("eventlistener", "listener-triggerref", ns)
		Expect(err).NotTo(HaveOccurred())

		oc.CreateSecretWithSecretToken("github-secret", ns)
		oc.LinkSecretToSA("github-secret", "pipeline", ns)

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "listener-triggerref", ns) })

		routeURL := triggers.ExposeEventListener(sharedClients, "listener-triggerref", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "github", "pull_request", "testdata/triggers/triggersCRD/pull-request.json", false)
		triggers.AssertElResponse(sharedClients, resp, "listener-triggerref", ns)

		pipelines.ValidatePipelineRun(sharedClients, "parallel-pipelinerun", "successful", ns)
	})

	// ============================================================
	// Multiple TLS EventListeners -- TC16
	// ============================================================
	It("PIPELINES-05-TC16: Create multiple Eventlistener with TLS enabled", Label("e2e", "tls", "triggers", "admin", "sanity"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns
		oc.EnableTLSConfigForEventlisteners(ns)

		// First EventListener
		oc.Create("testdata/triggers/sample-pipeline.yaml", ns)
		oc.Create("testdata/triggers/triggerbindings/triggerbinding.yaml", ns)
		oc.Create("testdata/triggers/triggertemplate/triggertemplate.yaml", ns)
		oc.Create("testdata/triggers/eventlisteners/eventlistener-embeded-binding.yaml", ns)

		_, err := opc.VerifyResourceListMatchesName("triggerbinding", "pipeline-binding", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("triggertemplate", "pipeline-template", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("eventlistener", "listener-embed-binding", ns)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "listener-embed-binding", ns) })

		routeURL := triggers.ExposeEventListenerForTLS(sharedClients, "listener-embed-binding", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "github", "push", "testdata/push.json", true)
		triggers.AssertElResponse(sharedClients, resp, "listener-embed-binding", ns)

		pipelines.ValidatePipelineRun(sharedClients, "simple-pipeline-run", "successful", ns)

		// Second EventListener
		oc.Create("testdata/triggers/triggertemplate/triggertemplate-2.yaml", ns)
		oc.Create("testdata/triggers/eventlisteners/eventlistener-embeded-binding-2.yaml", ns)

		routeURL2 := triggers.ExposeEventListenerForTLS(sharedClients, "listener-embed-binding-2", ns)
		resp2, _ := triggers.MockPostEvent(routeURL2, "github", "push", "testdata/push.json", true)
		triggers.AssertElResponse(sharedClients, resp2, "listener-embed-binding-2", ns)

		pipelines.ValidatePipelineRun(sharedClients, "simple-pipeline-run-2", "successful", ns)
	})

	// ============================================================
	// Kubernetes Events -- TC17
	// ============================================================
	It("PIPELINES-05-TC17: Create Eventlistener with github interceptor And verify Kubernetes Events", Label("e2e", "events", "triggers", "admin", "sanity"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns

		oc.Create("testdata/triggers/sample-pipeline.yaml", ns)
		oc.Create("testdata/triggers/triggerbindings/triggerbinding.yaml", ns)
		oc.Create("testdata/triggers/triggertemplate/triggertemplate.yaml", ns)
		oc.Create("testdata/triggers/eventlisteners/eventlistener-embeded-binding.yaml", ns)

		_, err := opc.VerifyResourceListMatchesName("triggerbinding", "pipeline-binding", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("triggertemplate", "pipeline-template", ns)
		Expect(err).NotTo(HaveOccurred())
		_, err = opc.VerifyResourceListMatchesName("eventlistener", "listener-embed-binding", ns)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "listener-embed-binding", ns) })

		routeURL := triggers.ExposeEventListener(sharedClients, "listener-embed-binding", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "github", "push", "testdata/push.json", false)
		triggers.AssertElResponse(sharedClients, resp, "listener-embed-binding", ns)

		oc.VerifyKubernetesEventsForEventListener(ns)

		pipelines.ValidatePipelineRun(sharedClients, "simple-pipeline-run", "successful", ns)
	})
})
