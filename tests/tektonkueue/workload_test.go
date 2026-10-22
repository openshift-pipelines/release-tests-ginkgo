package tektonkueue_test

import (
	"context"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega

	workload "github.com/openshift-pipelines/release-tests-ginkgo/pkg/tektonkueue"
)

var _ = Describe("Multi-cluster PipelineRun execution", Serial, Label("tekton-kueue", "workload", "admin"), func() {
	It("executes a hub PipelineRun on one spoke and validates its logs", NodeTimeout(2*time.Hour), func(specCtx SpecContext) {
		environment := preparedEnvironment(specCtx)
		result, err := environment.Execute(specCtx)
		Expect(err).NotTo(HaveOccurred(), "multi-cluster PipelineRun execution failed")
		Expect(result.Logs).To(ContainSubstring("multi-cluster-execution-ok"),
			"selected spoke %s did not return the expected workload log", result.Spoke)
		GinkgoWriter.Printf("PipelineRun %s executed on %s and completed on the hub; spoke logs:\n%s",
			result.PipelineRun, result.Spoke, result.Logs)
	})
})

func preparedEnvironment(ctx SpecContext) *workload.Environment {
	prefix := "rtg-mk-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	configured := configuredClusters(ctx, prefix)
	environment := workload.NewEnvironment(configured[0], configured[1:], prefix)
	DeferCleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		if err := environment.Cleanup(cleanupCtx); err != nil {
			GinkgoWriter.Printf("retrying failed multi-cluster cleanup: %v\n", err)
			Expect(environment.Cleanup(cleanupCtx)).To(Succeed(), "failed to restore multi-cluster test environment")
		}
	})
	Expect(environment.Setup(ctx)).To(Succeed(), "failed to configure multi-cluster environment")
	return environment
}
