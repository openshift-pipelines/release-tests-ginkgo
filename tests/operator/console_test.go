package operator_test

import (
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("Console", Label("console"), func() {

	It("PIPELINES-08-TC01: Verify icon on console", Label("manualonly"), func() {
		Skip("Manual verification only -- requires browser interaction with OpenShift console (login, navigate to OperatorHub, search for OpenShift Pipelines Operator, verify icon)")
	})
})
