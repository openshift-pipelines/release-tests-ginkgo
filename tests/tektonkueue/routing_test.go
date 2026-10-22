package tektonkueue_test

import (
	"context"
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	workload "github.com/openshift-pipelines/release-tests-ginkgo/pkg/tektonkueue"
)

var _ = Describe("CEL PipelineRun routing", Serial, Label("tekton-kueue", "routing", "webhook", "admin", "e2e"), func() {
	DescribeTable("routes admitted PipelineRuns to the expected execution cluster",
		func(specCtx SpecContext, route string, remote bool) {
			prefix := "rtg-cel-" + strconv.FormatInt(time.Now().UnixNano(), 36)
			configured := make([]workload.Cluster, 0, 1+len(spokeConfigs))
			configs := append([]clientConfig{hubConfig}, spokeConfigs...)
			for i, cfg := range configs {
				name := "hub"
				if i > 0 {
					name = fmt.Sprintf("spoke-%d", i)
				}
				clusterClients, err := clients.NewClientsWithContext(cfg.Kubeconfig, cfg.Cluster, cfg.Context, prefix)
				Expect(err).NotTo(HaveOccurred(), "create clients for %s", name)
				configured = append(configured, workload.Cluster{Name: name, Clients: clusterClients})
			}
			environment := workload.NewEnvironment(configured[0], configured[1:], prefix)
			DeferCleanup(func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
				defer cancel()
				Expect(environment.Cleanup(cleanupCtx)).To(Succeed(), "restore scheduler configuration and remove routing resources")
			})

			By("configuring CEL with multiKueueOverride disabled and a hub-only fallback queue")
			Expect(environment.SetupRouting(specCtx)).To(Succeed())
			By("checking admission, TaskRun placement, and terminal success")
			selected, err := environment.ExecuteRouting(specCtx, route)
			Expect(err).NotTo(HaveOccurred())
			if remote {
				Expect(selected).To(HavePrefix("spoke-"))
			} else {
				Expect(selected).To(Equal("hub"))
			}
			GinkgoWriter.Printf("CEL scenario %s executed successfully on %s\n", route, selected)
		},
		Entry("routes a labeled build remotely with multiKueue()", "remote", true, NodeTimeout(2*time.Hour)),
		Entry("overrides the input queue with multiKueue(queue)", "explicit", true, NodeTimeout(2*time.Hour)),
		Entry("falls back to local execution for an unmatched label", "local", false, NodeTimeout(2*time.Hour)),
		Entry("falls back to local execution when the route label is missing", "missing", false, NodeTimeout(2*time.Hour)),
	)
})
