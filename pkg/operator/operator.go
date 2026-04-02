package operator

import (
	"fmt"
	"log"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/opc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
	"github.com/tektoncd/operator/test/utils"
)

// WaitForTektonConfigCR ensures a TektonConfig CR exists.
func WaitForTektonConfigCR(cs *clients.Clients, rnames utils.ResourceNames) {
	_, err := EnsureTektonConfigExists(cs.TektonConfig(), rnames)
	Expect(err).NotTo(HaveOccurred(), "TektonConfig doesn't exist")
}

// ValidateRBAC verifies that RBAC resources are auto-created successfully.
func ValidateRBAC(cs *clients.Clients, rnames utils.ResourceNames) {
	log.Printf("Verifying that TektonConfig status is \"installed\"\n")
	EnsureTektonConfigStatusInstalled(cs.TektonConfig(), rnames)

	AssertServiceAccountPresent(cs, store.Namespace(), "pipeline")
	AssertClusterRolePresent(cs, "pipelines-scc-clusterrole")
	AssertConfigMapPresent(cs, store.Namespace(), "config-service-cabundle")
	AssertConfigMapPresent(cs, store.Namespace(), "config-trusted-cabundle")
	AssertRoleBindingPresent(cs, store.Namespace(), "openshift-pipelines-edit")
	AssertRoleBindingPresent(cs, store.Namespace(), "pipelines-scc-rolebinding")
	AssertSCCPresent(cs, "pipelines-scc")
}

// ValidateRBACAfterDisable verifies RBAC resources are properly disabled.
func ValidateRBACAfterDisable(cs *clients.Clients, rnames utils.ResourceNames) {
	EnsureTektonConfigStatusInstalled(cs.TektonConfig(), rnames)
	AssertServiceAccountPresent(cs, store.Namespace(), "pipeline")
	AssertClusterRoleNotPresent(cs, "pipelines-scc-clusterrole")
	AssertRoleBindingNotPresent(cs, store.Namespace(), "edit")
	AssertRoleBindingNotPresent(cs, store.Namespace(), "pipelines-scc-rolebinding")
	AssertSCCNotPresent(cs, "pipelines-scc")
}

// ValidateCABundleConfigMaps verifies CA Bundle ConfigMaps are created.
func ValidateCABundleConfigMaps(cs *clients.Clients, rnames utils.ResourceNames) {
	log.Printf("Verifying that TektonConfig status is \"installed\"\n")
	EnsureTektonConfigStatusInstalled(cs.TektonConfig(), rnames)
	AssertConfigMapPresent(cs, store.Namespace(), "config-service-cabundle")
	AssertConfigMapPresent(cs, store.Namespace(), "config-trusted-cabundle")
}

// ValidateOperatorInstallStatus verifies the operator is installed and running.
func ValidateOperatorInstallStatus(cs *clients.Clients, rnames utils.ResourceNames) {
	operatorVersion := opc.GetOPCServerVersion("operator")
	Expect(operatorVersion).NotTo(ContainSubstring("unknown"),
		"Operator is not installed")
	log.Printf("Waiting for operator to be up and running....\n")
	EnsureTektonConfigStatusInstalled(cs.TektonConfig(), rnames)
	log.Printf("Operator is up\n")
}

// Ensure the unused import warning doesn't fire.
var _ = fmt.Sprintf
var _ = GinkgoWriter
var _ = strings.Contains
