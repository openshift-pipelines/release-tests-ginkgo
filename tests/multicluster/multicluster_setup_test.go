package multicluster

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	olmpkg "github.com/openshift-pipelines/release-tests-ginkgo/pkg/olm"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

const (
	KueueOperatorsNamespace = "openshift-kueue-operator"
	CertManagerNamespace    = "cert-manager-operator"
	certManagerPackage      = "openshift-cert-manager-operator"
	kueueOperatorPackage    = "kueue-operator"
)

var _ = Describe("PIPELINES-11-TC01: Install operators", Label("install", "multi-cluster", "operator"), Ordered, func() {

	It("subscribes to  Cert-Manager operator", Label("cert-manager"), func() {
		cmsr := olmpkg.SubscriptionRequest{
			Name:              certManagerPackage,
			OperatorNamespace: CertManagerNamespace,
			SubscriptionSpec: &v1alpha1.SubscriptionSpec{
				CatalogSource: config.Flags.CatalogSource,
				Channel:       "stable-v1",
				Package:       certManagerPackage,
				Config: &v1alpha1.SubscriptionConfig{
					Env: []corev1.EnvVar{
						{
							Name:  "ROLEARN",
							Value: "test",
						},
					},
				},
			},
		}
		_, err := olmpkg.SubscribeAndWaitForOperatorToBeReady(sharedClients, cmsr, "")
		Expect(err).NotTo(HaveOccurred(), "Failed to subscribe to operator")
	})

	It("subscribes to  RHBOK operator", Label("rhbok", "kueue-operator"), func() {
		sr := olmpkg.SubscriptionRequest{
			Name:              kueueOperatorPackage,
			OperatorNamespace: KueueOperatorsNamespace,
			SubscriptionSpec: &v1alpha1.SubscriptionSpec{
				CatalogSource: config.Flags.CatalogSource,
				Channel:       "stable-v1.3",
				Package:       kueueOperatorPackage,
			},
		}
		_, err := olmpkg.SubscribeAndWaitForOperatorToBeReady(sharedClients, sr, "")
		Expect(err).NotTo(HaveOccurred(), "Failed to subscribe to operator")
	})

})
