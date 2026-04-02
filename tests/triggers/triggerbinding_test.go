package triggers_test

import (
	. "github.com/onsi/ginkgo/v2"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/triggers"
)

var _ = Describe("TriggerBindings", Label("triggers"), func() {

	It("PIPELINES-10-TC01: Verify CEL marshaljson function", Label("e2e", "triggers", "non-admin", "sanity"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns
		oc.Create("testdata/triggers/triggerbindings/cel-marshalJson.yaml", ns)
		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "cel-marshaljson", ns) })
		routeURL := triggers.ExposeEventListener(sharedClients, "cel-marshaljson", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "github", "push", "testdata/push.json", false)
		triggers.AssertElResponse(sharedClients, resp, "cel-marshaljson", ns)
		pipelines.ValidateTaskRun(sharedClients, "cel-trig-marshaljson", "Success", ns)
	})

	It("PIPELINES-10-TC02: Verify event message body parsing with old annotation", Label("e2e", "triggers", "non-admin", "sanity"), func() {
		ns := config.TargetNamespace
		lastNamespace = ns
		oc.Create("testdata/triggers/triggerbindings/parse-json-body-with-annotation.yaml", ns)
		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "parse-json-body-with-annotation", ns) })
		routeURL := triggers.ExposeEventListener(sharedClients, "parse-json-body-with-annotation", ns)
		resp, _ := triggers.MockPostEvent(routeURL, "github", "push", "testdata/push.json", false)
		triggers.AssertElResponse(sharedClients, resp, "parse-json-body-with-annotation", ns)
		pipelines.ValidateTaskRun(sharedClients, "trig-parse-json-body-with-annotation", "Success", ns)
	})

	It("PIPELINES-10-TC03: Verify event message body marshalling error", Label("non-admin"), Pending, func() {
		// Pending: tagged bug-to-fix in Gauge suite
	})
})
