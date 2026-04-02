package versions_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/opc"
)

var _ = Describe("PIPELINES-22: Versions", Label("sanity", "e2e"), func() {

	It("PIPELINES-22-TC01: Check server side components versions", func() {
		// Check each component version against environment variable
		components := []struct {
			name   string
			envVar string
		}{
			{"pipeline", "PIPELINE_VERSION"},
			{"triggers", "TRIGGERS_VERSION"},
			{"operator", "OPERATOR_VERSION"},
			{"chains", "CHAINS_VERSION"},
			{"pac", "PAC_VERSION"},
			{"hub", "HUB_VERSION"},
			{"results", "RESULTS_VERSION"},
			{"manual-approval-gate", "MANUAL_APPROVAL_GATE_VERSION"},
		}
		for _, comp := range components {
			version := os.Getenv(comp.envVar)
			if version == "" {
				Skip(comp.envVar + " not set in environment")
			}
			opc.AssertComponentVersion(version, comp.name)
		}
		// Check OSP version
		ospVersion := os.Getenv("OSP_VERSION")
		if ospVersion == "" {
			Skip("OSP_VERSION not set in environment")
		}
		opc.AssertComponentVersion(ospVersion, "OSP")
	})

	It("PIPELINES-22-TC02: Check client versions", func() {
		opc.DownloadCLIFromCluster()
		opc.AssertClientVersion("tkn")
		opc.AssertClientVersion("tkn-pac")
		opc.AssertClientVersion("opc")
		opc.AssertServerVersion("opc")
	})
})
