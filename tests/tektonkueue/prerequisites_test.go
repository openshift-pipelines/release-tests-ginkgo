package tektonkueue_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega
)

var _ = Describe("OpenShift Multicluster prerequisite readiness", Serial,
	Label("tekton-kueue", "multicluster", "prerequisites", "admin"), func() {
		It("installs or reuses prerequisites and proves the hub and every spoke ready",
			NodeTimeout(6*time.Hour), func(ctx SpecContext) {
				environment := preparedEnvironment(ctx)
				Expect(environment.WaitReady(ctx)).To(Succeed(), "multicluster prerequisites did not become ready")
				Expect(environment.AssertReady(ctx)).To(Succeed(), "multicluster readiness assertion failed")
			})
	})
