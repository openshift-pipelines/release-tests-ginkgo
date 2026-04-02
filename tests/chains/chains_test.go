package chains_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/operator"
)

var _ = Describe("Tekton Chains", Label("chains", "e2e"), func() {

	Describe("PIPELINES-27-TC01: Using Tekton Chains to create and verify task run signatures", Label("sanity"), Ordered, func() {
		BeforeAll(func() {
			lastNamespace = config.TargetNamespace
			// Update TektonConfig for taskrun signing
			operator.UpdateTektonConfigForChains("in-toto", "tekton", "", "false")
			DeferCleanup(operator.RestoreTektonConfigChains)

			// Store cosign public key
			err := operator.CreateFileWithCosignPubKey()
			Expect(err).NotTo(HaveOccurred(), "Failed to store cosign public key")
		})

		It("applies the task-output-image task and verifies taskrun signature", func() {
			ns := config.TargetNamespace
			oc.Apply("testdata/chains/task-output-image.yaml", ns)

			err := operator.VerifySignature("taskrun")
			Expect(err).NotTo(HaveOccurred(), "Failed to verify taskrun signature")
		})
	})

	Describe("PIPELINES-27-TC02: Using Tekton Chains to sign and verify image and provenance", Ordered, func() {
		BeforeAll(func() {
			lastNamespace = config.TargetNamespace
			if os.Getenv("CHAINS_REPOSITORY") == "" {
				Skip("CHAINS_REPOSITORY not set -- skipping image signature test")
			}

			// Update TektonConfig for image signing with OCI storage
			operator.UpdateTektonConfigForChains("in-toto", "oci", "oci", "true")
			DeferCleanup(operator.RestoreTektonConfigChains)

			// Store cosign public key
			err := operator.CreateFileWithCosignPubKey()
			Expect(err).NotTo(HaveOccurred(), "Failed to store cosign public key")

			// Create image registry credentials secret
			oc.CreateChainsImageRegistrySecret(os.Getenv("DOCKER_CONFIG"))
		})

		It("starts kaniko task and applies chain resources", func() {
			ns := config.TargetNamespace
			oc.Apply("testdata/pvc/chains-pvc.yaml", ns)
			oc.Apply("testdata/chains/kaniko.yaml", ns)
			operator.StartKanikoTask()
		})

		It("verifies image signature", func() {
			err := operator.VerifyImageSignature()
			Expect(err).NotTo(HaveOccurred(), "Failed to verify image signature")
		})

		It("checks attestation exists in transparency log", func() {
			err := operator.CheckAttestationExists()
			Expect(err).NotTo(HaveOccurred(), "Failed to find attestation")
		})

		It("verifies attestation", func() {
			err := operator.VerifyAttestation()
			Expect(err).NotTo(HaveOccurred(), "Failed to verify attestation")
		})
	})
})
