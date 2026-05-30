package operator_test

import (
	"fmt"
	"log"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// patchTektonConfigParam patches a TektonConfig param with the given name and value.
func patchTektonConfigParam(paramName, value string) {
	patchData := fmt.Sprintf(`{"spec":{"params":[{"name":"%s","value":"%s"}]}}`, paramName, value)
	log.Printf("Patching TektonConfig param %s=%s\n", paramName, value)
	cmd.MustSucceed("oc", "patch", "TektonConfig", "config", "--type=merge", "-p", patchData)
	operator.EnsureTektonConfigStatusInstalled(sharedClients.TektonConfig(), store.GetCRNames())
}

var _ = Describe("Verify RBAC Resources and CA Bundle Configuration", Serial, Ordered,
	Label("e2e", "operator", "admin"), func() {

		BeforeAll(func() {
			lastNamespace = config.TargetNamespace
			operator.ValidateOperatorInstallStatus(sharedClients, store.GetCRNames())

			// Restore both params to "true" on cleanup
			DeferCleanup(func() {
				log.Println("Restoring TektonConfig params: createRbacResource=true, createCABundleConfigMaps=true")
				cmd.MustSucceed("oc", "patch", "TektonConfig", "config", "--type=merge", "-p",
					`{"spec":{"params":[{"name":"createRbacResource","value":"true"}]}}`)
				cmd.MustSucceed("oc", "patch", "TektonConfig", "config", "--type=merge", "-p",
					`{"spec":{"params":[{"name":"createCABundleConfigMaps","value":"true"}]}}`)
			})
		})

		It("Disable RBAC resource creation", Label("sanity", "rbac-disable"), func() {
			// Enable RBAC and verify
			patchTektonConfigParam("createRbacResource", "true")
			operator.ValidateRBAC(sharedClients, store.GetCRNames())

			// Disable RBAC and verify
			patchTektonConfigParam("createRbacResource", "false")
			operator.ValidateRBACAfterDisable(sharedClients, store.GetCRNames())

			// Re-enable RBAC and verify
			patchTektonConfigParam("createRbacResource", "true")
			operator.ValidateRBAC(sharedClients, store.GetCRNames())
		})

		It("Independent CA Bundle ConfigMap creation control", Label("sanity", "cabundle-control"), func() {
			// Enable CA bundle and verify
			patchTektonConfigParam("createCABundleConfigMaps", "true")
			operator.ValidateCABundleConfigMaps(sharedClients, store.GetCRNames())

			// Disable CA bundle -- configmaps should still exist per spec
			patchTektonConfigParam("createCABundleConfigMaps", "false")
			operator.ValidateCABundleConfigMaps(sharedClients, store.GetCRNames())
		})
	})
