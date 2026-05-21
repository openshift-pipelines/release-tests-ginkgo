package ecosystem_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)

// ========================================================================
// PIPELINES-32: Multiarch Ecosystem Task Pipelines (ecosystem-multiarch.spec)
// ========================================================================
//
// These tests verify ecosystem tasks on different CPU architectures.
// Architecture-specific tests are skipped automatically when the cluster
// architecture doesn't match the required architecture.
//
// Test Cases:
//   TC01: jib-maven pipelinerun (amd64)
//   TC02: jib-maven P&Z pipelinerun (ppc64le, s390x, arm64)
//   TC03: kn-apply pipelinerun (amd64)
//   TC04: kn-apply p&z pipelinerun (ppc64le, s390x)
//   TC05: kn pipelinerun (amd64)
//   TC06: kn p&z pipelinerun (ppc64le, s390x)
// ========================================================================

// TC01: jib-maven pipelinerun (amd64)
var _ = Describe("jib-maven pipelinerun: PIPELINES-32-TC01", Label("ecosystem", "e2e", "sanity", "jib-maven"), func() {
	It("should create jib-maven pipelinerun with registry credentials", func() {
		if len([]string{"amd64"}) > 0 {
			archMatch := false
			for _, arch := range []string{"amd64"} {
				if config.Flags.ClusterArch == arch {
					archMatch = true
					break
				}
			}
			if !archMatch {
				Skip(fmt.Sprintf("requires one of architectures: %v (cluster is %s)", []string{"amd64"}, config.Flags.ClusterArch))
			}
		}

		ns := store.Namespace()
		sharedClients.NewClientSet(ns)

		oc.ValidateAndCreateJibMavenSecret(ns)

		oc.Create("testdata/ecosystem/pipelines/jib-maven.yaml")
		oc.Create("testdata/pvc/pvc.yaml")
		oc.Create("testdata/ecosystem/pipelineruns/jib-maven.yaml")

		pipelines.ValidatePipelineRun(sharedClients, "jib-maven-run", "successful", ns)
	})
})

// TC02: jib-maven P&Z pipelinerun (ppc64le, s390x, arm64)
var _ = Describe("jib-maven P&Z pipelinerun: PIPELINES-32-TC02", Label("ecosystem", "e2e", "sanity", "jib-maven"), func() {
	It("should create jib-maven pipelinerun with registry credentials", func() {
		if len([]string{"ppc64le", "s390x", "arm64"}) > 0 {
			archMatch := false
			for _, arch := range []string{"ppc64le", "s390x", "arm64"} {
				if config.Flags.ClusterArch == arch {
					archMatch = true
					break
				}
			}
			if !archMatch {
				Skip(fmt.Sprintf("requires one of architectures: %v (cluster is %s)", []string{"ppc64le", "s390x", "arm64"}, config.Flags.ClusterArch))
			}
		}

		ns := store.Namespace()
		sharedClients.NewClientSet(ns)

		// oc.ValidateAndCreateJibMavenSecret(ns)

		oc.Create("testdata/ecosystem/pipelines/jib-maven-pz.yaml")
		oc.Create("testdata/pvc/pvc.yaml")
		oc.Create("testdata/ecosystem/pipelineruns/jib-maven-pz.yaml")

		pipelines.ValidatePipelineRun(sharedClients, "jib-maven-pz-run", "successful", ns)
	})
})

// TC03: kn-apply pipelinerun (amd64)
var _ = Describe("kn-apply pipelinerun: PIPELINES-32-TC03", Label("ecosystem", "e2e", "kn-apply"), func() {
	It("should create and verify kn-apply pipelinerun", func() {
		if len([]string{"amd64"}) > 0 {
			archMatch := false
			for _, arch := range []string{"amd64"} {
				if config.Flags.ClusterArch == arch {
					archMatch = true
					break
				}
			}
			if !archMatch {
				Skip(fmt.Sprintf("requires one of architectures: %v (cluster is %s)", []string{"amd64"}, config.Flags.ClusterArch))
			}
		}

		ns := store.Namespace()
		sharedClients.NewClientSet(ns)

		oc.Create("testdata/ecosystem/pipelineruns/kn-apply.yaml")

		pipelines.ValidatePipelineRun(sharedClients, "kn-apply-run", "successful", ns)
	})
})

// TC04: kn-apply p&z pipelinerun (ppc64le, s390x)
var _ = Describe("kn-apply p&z pipelinerun: PIPELINES-32-TC04", Label("ecosystem", "e2e", "kn-apply"), func() {
	It("should create and verify kn-apply pipelinerun", func() {
		if len([]string{"ppc64le", "s390x"}) > 0 {
			archMatch := false
			for _, arch := range []string{"ppc64le", "s390x"} {
				if config.Flags.ClusterArch == arch {
					archMatch = true
					break
				}
			}
			if !archMatch {
				Skip(fmt.Sprintf("requires one of architectures: %v (cluster is %s)", []string{"ppc64le", "s390x"}, config.Flags.ClusterArch))
			}
		}

		ns := store.Namespace()
		sharedClients.NewClientSet(ns)

		oc.Create("testdata/ecosystem/pipelineruns/kn-apply-multiarch.yaml")

		pipelines.ValidatePipelineRun(sharedClients, "kn-apply-pz-run", "successful", ns)
	})
})

// TC05: kn pipelinerun (amd64)
var _ = Describe("kn pipelinerun: PIPELINES-32-TC05", Label("ecosystem", "e2e", "kn"), func() {
	It("should create and verify kn pipelinerun", func() {
		if len([]string{"amd64"}) > 0 {
			archMatch := false
			for _, arch := range []string{"amd64"} {
				if config.Flags.ClusterArch == arch {
					archMatch = true
					break
				}
			}
			if !archMatch {
				Skip(fmt.Sprintf("requires one of architectures: %v (cluster is %s)", []string{"amd64"}, config.Flags.ClusterArch))
			}
		}

		ns := store.Namespace()
		sharedClients.NewClientSet(ns)

		oc.Create("testdata/ecosystem/pipelineruns/kn.yaml")

		pipelines.ValidatePipelineRun(sharedClients, "kn-run", "successful", ns)
	})
})

// TC06: kn p&z pipelinerun (ppc64le, s390x)
var _ = Describe("kn p&z pipelinerun: PIPELINES-32-TC06", Label("ecosystem", "e2e", "kn"), func() {
	It("should create and verify kn pipelinerun", func() {
		if len([]string{"ppc64le", "s390x"}) > 0 {
			archMatch := false
			for _, arch := range []string{"ppc64le", "s390x"} {
				if config.Flags.ClusterArch == arch {
					archMatch = true
					break
				}
			}
			if !archMatch {
				Skip(fmt.Sprintf("requires one of architectures: %v (cluster is %s)", []string{"ppc64le", "s390x"}, config.Flags.ClusterArch))
			}
		}

		ns := store.Namespace()
		sharedClients.NewClientSet(ns)

		oc.Create("testdata/ecosystem/pipelineruns/kn-pz.yaml")

		pipelines.ValidatePipelineRun(sharedClients, "kn-pz-run", "successful", ns)
	})
})

// Ensure imports are used
var _ = Expect
