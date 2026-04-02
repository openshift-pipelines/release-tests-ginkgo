package versions_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/opc"
)

var _ = Describe("Versions of OpenShift Pipelines", Label("versions", "e2e"), func() {

	BeforeEach(func() {
		lastNamespace = "openshift-pipelines"
	})

	Describe("PIPELINES-22-TC01: Check server side components versions", Label("sanity"), func() {

		DescribeTable("verifies component version",
			func(component, envVar string) {
				expectedVersion := os.Getenv(envVar)
				if expectedVersion == "" {
					Skip(envVar + " not set in environment")
				}

				opc.AssertComponentVersion(expectedVersion, component)
			},
			Entry("pipeline version", "pipeline", "PIPELINE_VERSION"),
			Entry("triggers version", "triggers", "TRIGGERS_VERSION"),
			Entry("operator version", "operator", "OPERATOR_VERSION"),
			Entry("chains version", "chains", "CHAINS_VERSION"),
			Entry("pac version", "pac", "PAC_VERSION"),
			Entry("hub version", "hub", "HUB_VERSION"),
			Entry("results version", "results", "RESULTS_VERSION"),
			Entry("manual-approval-gate version", "manual-approval-gate", "MAG_VERSION"),
			Entry("OSP version", "OSP", "OSP_VERSION"),
		)
	})

	Describe("PIPELINES-22-TC02: Check client versions", Label("sanity"), Ordered, func() {
		It("downloads and extracts CLI from cluster", func() {
			opc.DownloadCLIFromCluster()
		})

		It("checks tkn client version", func() {
			opc.AssertClientVersion("tkn")
		})

		It("checks tkn-pac version", func() {
			opc.AssertClientVersion("tkn-pac")
		})

		It("checks opc client version", func() {
			opc.AssertClientVersion("opc")
		})

		It("checks opc server version", func() {
			opc.AssertServerVersion("opc")
		})
	})
})
