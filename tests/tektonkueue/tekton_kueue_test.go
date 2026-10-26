package tektonkueue_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

var _ = Describe("Multi-cluster operator bootstrap", Serial, Label("tekton-kueue", "install", "admin"), func() {
	It("installs the required operators on the hub and every spoke", NodeTimeout(6*time.Hour), func(specCtx SpecContext) {
		configuredClusters(specCtx, config.TargetNamespace)
	})
})
