package operator_test

import (
	"log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/openshift"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/triggers"
)

// ---------------------------------------------------------------------------
// Pre-upgrade tests
// ---------------------------------------------------------------------------

var _ = Describe("PIPELINES-18: Openshift Pipelines pre upgrade specs", Serial, Ordered,
	Label("operator", "admin", "pre-upgrade"), func() {

		BeforeAll(func() {
			// NOTE: Do NOT delete the pre-upgrade projects in DeferCleanup.
			// They must persist for post-upgrade tests to verify.
		})

		It("PIPELINES-18-TC01: Setup environment for upgrade test", func() {
			ns := "releasetest-upgrade-triggers"
			oc.CreateNewProject(ns)

			// Verify pipeline SA exists
			operator.AssertServiceAccountPresent(sharedClients, ns, "pipeline")

			// Create trigger resources: embedded trigger template and eventlistener
			oc.Create("testdata/triggers/github-ctb/Embeddedtriggertemplate-git-push.yaml", ns)
			oc.Create("testdata/triggers/github-ctb/eventlistener-ctb-git-push.yaml", ns)

			// Verify imagestream "golang" exists
			tags := openshift.GetImageStreamTags(sharedClients, "openshift", "golang")
			Expect(tags).NotTo(BeEmpty(), "imagestream 'golang' should exist in openshift namespace")

			// Create & link github-secret
			oc.CreateSecretWithSecretToken("github-secret", ns)
			oc.LinkSecretToSA("github-secret", "pipeline", ns)

			// Expose eventlistener and send mock event
			route := triggers.ExposeEventListener(sharedClients, "listener-ctb-github-push", ns)
			resp, payload := triggers.MockPostEvent(route, "github", "push",
				config.Path("testdata/triggers/github-ctb/push.json"), false)
			store.PutScenarioData("payload", string(payload))
			triggers.AssertElResponse(sharedClients, resp, "listener-ctb-github-push", ns)

			// Verify pipelinerun
			pipelines.ValidatePipelineRun(sharedClients, "pipelinerun-git-push-ctb", "successful", ns)
			oc.DeleteResourceInNamespace("pipelinerun", "pipelinerun-git-push-ctb", ns)

			// Create triggersCRD resources
			oc.Create("testdata/triggers/triggersCRD/eventlistener-triggerref.yaml", ns)
			oc.Create("testdata/triggers/triggersCRD/trigger.yaml", ns)
			oc.Create("testdata/triggers/triggersCRD/triggerbindings.yaml", ns)
			oc.Create("testdata/triggers/triggersCRD/triggertemplate.yaml", ns)
			oc.Create("testdata/triggers/triggersCRD/pipeline.yaml", ns)

			// Expose eventlistener and send mock PR event
			route2 := triggers.ExposeEventListener(sharedClients, "listener-triggerref", ns)
			resp2, payload2 := triggers.MockPostEvent(route2, "github", "pull_request",
				config.Path("testdata/triggers/triggersCRD/pull-request.json"), false)
			store.PutScenarioData("payload", string(payload2))
			triggers.AssertElResponse(sharedClients, resp2, "listener-triggerref", ns)

			pipelines.ValidatePipelineRun(sharedClients, "parallel-pipelinerun", "successful", ns)
			oc.DeleteResourceInNamespace("pipelinerun", "parallel-pipelinerun", ns)

			// Create bitbucket resources
			oc.Create("testdata/triggers/bitbucket/bitbucket-eventlistener-interceptor.yaml", ns)
			oc.CreateSecretWithSecretToken("bitbucket-secret", ns)
			oc.LinkSecretToSA("bitbucket-secret", "pipeline", ns)

			route3 := triggers.ExposeEventListener(sharedClients, "bitbucket-listener", ns)
			resp3, payload3 := triggers.MockPostEvent(route3, "bitbucket", "refs_changed",
				config.Path("testdata/triggers/bitbucket/refs-change-event.json"), false)
			store.PutScenarioData("payload", string(payload3))
			triggers.AssertElResponse(sharedClients, resp3, "bitbucket-listener", ns)

			pipelines.ValidateTaskRun(sharedClients, "bitbucket-run", "Failure", ns)
			oc.DeleteResourceInNamespace("taskrun", "bitbucket-run", ns)
		})

		It("PIPELINES-18-TC03: Setup Eventlistener with TLS enabled pre upgrade", Label("sanity", "tls", "triggers"), func() {
			ns := "releasetest-upgrade-tls"
			oc.CreateNewProject(ns)

			oc.EnableTLSConfigForEventlisteners(ns)

			oc.Create("testdata/triggers/sample-pipeline.yaml", ns)
			oc.Create("testdata/triggers/triggerbindings/triggerbinding.yaml", ns)
			oc.Create("testdata/triggers/triggertemplate/triggertemplate.yaml", ns)
			oc.Create("testdata/triggers/eventlisteners/eventlistener-embeded-binding.yaml", ns)

			route := triggers.ExposeEventListenerForTLS(sharedClients, "listener-embed-binding", ns)
			resp, payload := triggers.MockPostEvent(route, "github", "push",
				config.Path("testdata/push.json"), true)
			store.PutScenarioData("payload", string(payload))
			triggers.AssertElResponse(sharedClients, resp, "listener-embed-binding", ns)

			pipelines.ValidatePipelineRun(sharedClients, "simple-pipeline-run", "successful", ns)
			oc.DeleteResourceInNamespace("pipelinerun", "simple-pipeline-run", ns)
		})

		It("PIPELINES-18-TC04: Setup link secret to pipeline SA", Label("sanity", "git-clone"), func() {
			ns := "releasetest-upgrade-pipelines"
			oc.CreateNewProject(ns)

			operator.AssertServiceAccountPresent(sharedClients, ns, "pipeline")

			oc.Create("testdata/ecosystem/pipelines/git-clone-read-private.yaml", ns)
			oc.Create("testdata/pvc/pvc.yaml", ns)
			oc.Create("testdata/ecosystem/secrets/ssh-key.yaml", ns)
			oc.LinkSecretToSA("ssh-key", "pipeline", ns)

			oc.Create("testdata/ecosystem/pipelineruns/git-clone-read-private.yaml", ns)
			pipelines.ValidatePipelineRun(sharedClients, "git-clone-read-private-pipeline-run", "successful", ns)
			oc.DeleteResourceInNamespace("pipelinerun", "git-clone-read-private-pipeline-run", ns)
		})

		It("PIPELINES-18-TC05: Setup S2I golang pipeline pre upgrade", Label("s2i"), func() {
			ns := "releasetest-upgrade-s2i"
			oc.CreateNewProject(ns)

			operator.AssertServiceAccountPresent(sharedClients, ns, "pipeline")

			oc.Create("testdata/ecosystem/pipelines/s2i-go.yaml", ns)
			oc.Create("testdata/pvc/pvc.yaml", ns)
		})
	})

// ---------------------------------------------------------------------------
// Post-upgrade tests
// ---------------------------------------------------------------------------

var _ = Describe("PIPELINES-19: Olm Openshift Pipelines operator post upgrade tests", Serial, Ordered,
	Label("operator", "admin", "post-upgrade"), func() {

		BeforeAll(func() {
			operator.ValidateOperatorInstallStatus(sharedClients, store.GetCRNames())

			// Cleanup all upgrade projects after post-upgrade tests
			DeferCleanup(func() {
				log.Println("Cleaning up upgrade test namespaces")
				oc.DeleteProjectIgnoreErrors("releasetest-upgrade-triggers")
				oc.DeleteProjectIgnoreErrors("releasetest-upgrade-tls")
				oc.DeleteProjectIgnoreErrors("releasetest-upgrade-pipelines")
				oc.DeleteProjectIgnoreErrors("releasetest-upgrade-s2i")
			})
		})

		It("PIPELINES-19-TC01: Verify environment after upgrade", func() {
			ns := "releasetest-upgrade-triggers"
			cmd.MustSucceed("oc", "project", ns)

			// GitHub push event
			route1 := triggers.GetRoute("listener-ctb-github-push", ns)
			resp1, payload1 := triggers.MockPostEvent(route1, "github", "push",
				config.Path("testdata/triggers/github-ctb/push.json"), false)
			store.PutScenarioData("payload", string(payload1))
			triggers.AssertElResponse(sharedClients, resp1, "listener-ctb-github-push", ns)
			pipelines.ValidatePipelineRun(sharedClients, "pipelinerun-git-push-ctb", "successful", ns)

			// GitHub PR event via triggersCRD
			route2 := triggers.GetRoute("listener-triggerref", ns)
			resp2, payload2 := triggers.MockPostEvent(route2, "github", "pull_request",
				config.Path("testdata/triggers/triggersCRD/pull-request.json"), false)
			store.PutScenarioData("payload", string(payload2))
			triggers.AssertElResponse(sharedClients, resp2, "listener-triggerref", ns)
			pipelines.ValidatePipelineRun(sharedClients, "parallel-pipelinerun", "successful", ns)

			// Bitbucket event
			route3 := triggers.GetRoute("bitbucket-listener", ns)
			resp3, payload3 := triggers.MockPostEvent(route3, "bitbucket", "refs_changed",
				config.Path("testdata/triggers/bitbucket/refs-change-event.json"), false)
			store.PutScenarioData("payload", string(payload3))
			triggers.AssertElResponse(sharedClients, resp3, "bitbucket-listener", ns)
			pipelines.ValidateTaskRun(sharedClients, "bitbucket-run", "Failure", ns)
		})

		It("PIPELINES-19-TC03: Verify Event listener with TLS after upgrade", Label("sanity", "tls", "triggers"), func() {
			ns := "releasetest-upgrade-tls"
			cmd.MustSucceed("oc", "project", ns)

			route := triggers.GetRoute("listener-embed-binding", ns)
			resp, payload := triggers.MockPostEvent(route, "github", "push",
				config.Path("testdata/push.json"), true)
			store.PutScenarioData("payload", string(payload))
			triggers.AssertElResponse(sharedClients, resp, "listener-embed-binding", ns)
			pipelines.ValidatePipelineRun(sharedClients, "simple-pipeline-run", "successful", ns)
		})

		It("PIPELINES-19-TC04: Verify secret is linked to SA even after upgrade", Label("sanity", "git-clone"), func() {
			ns := "releasetest-upgrade-pipelines"
			cmd.MustSucceed("oc", "project", ns)

			operator.AssertServiceAccountPresent(sharedClients, ns, "pipeline")

			oc.Create("testdata/ecosystem/pipelineruns/git-clone-read-private.yaml", ns)
			pipelines.ValidatePipelineRun(sharedClients, "git-clone-read-private-pipeline-run", "successful", ns)
		})

		It("PIPELINES-19-TC05: Verify S2I golang pipeline after upgrade", Label("s2i"), func() {
			ns := "releasetest-upgrade-s2i"
			cmd.MustSucceed("oc", "project", ns)

			// Get imagestream tags for golang
			tags := openshift.GetImageStreamTags(sharedClients, "openshift", "golang")
			Expect(tags).NotTo(BeEmpty(), "golang imagestream tags should not be empty")

			// TODO: Start and verify s2i pipeline with VERSION param from tags.
			// The exact helper for "Start and verify pipeline with param with values
			// stored in variable" is not yet migrated to the Ginkgo codebase.
			// For now, skip the pipeline execution part.
			Skip("helper not yet migrated: StartAndVerifyPipelineWithParam")
		})
	})
