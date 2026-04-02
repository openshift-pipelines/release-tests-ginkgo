package triggers_test

import (
	. "github.com/onsi/ginkgo/v2"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/triggers"
)

var _ = Describe("CronJob Triggers", Label("triggers"), func() {
	It("PIPELINES-04-TC01: Create Triggers using k8s cronJob", Label("e2e", "triggers", "non-admin", "sanity"), func() {
		ns := config.TargetNamespace

		oc.Create("testdata/triggers/cron/example-pipeline.yaml", ns)
		oc.Create("testdata/triggers/cron/triggerbinding.yaml", ns)
		oc.Create("testdata/triggers/cron/triggertemplate.yaml", ns)
		oc.Create("testdata/triggers/cron/eventlistener.yaml", ns)

		DeferCleanup(func() { triggers.CleanupTriggers(sharedClients, "cron-listener", ns) })

		routeURL := triggers.ExposeEventListener(sharedClients, "cron-listener", ns)

		// Verify image stream "golang" exists in openshift namespace
		cmd.MustSucceed("oc", "get", "is", "golang", "-n", "openshift")

		// Create cron job that curls the EventListener every minute
		cronJobName := k8s.CreateCronJob(sharedClients, routeURL, "*/1 * * * *", ns)
		DeferCleanup(func() { k8s.DeleteCronJob(sharedClients, cronJobName, ns) })

		// Watch for pipelinerun resources (waits 5 minutes)
		pipelines.WatchForPipelineRun(sharedClients, ns)

		// Delete cron job to stop triggering new runs
		k8s.DeleteCronJob(sharedClients, cronJobName, ns)

		// Assert no new pipelineruns are created after cron job deletion
		pipelines.AssertForNoNewPipelineRunCreation(sharedClients, ns)
	})
})
