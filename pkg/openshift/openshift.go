package openshift

import (
	"log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	imageStream "github.com/openshift/client-go/image/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetImageStreamTags returns the list of tag names for the given imagestream.
func GetImageStreamTags(c *clients.Clients, namespace, name string) []string {
	log.Printf("Getting imagestream %s from namespace %s", name, namespace)
	is := imageStream.NewForConfigOrDie(c.KubeConfig)
	isRequired, err := is.ImageV1().ImageStreams(namespace).Get(c.Ctx, name, metav1.GetOptions{})
	if err != nil {
		Fail("failed to get imagestream " + name + ": " + err.Error())
	}
	tags := isRequired.Spec.Tags
	var tagNames []string
	for _, tag := range tags {
		tagNames = append(tagNames, tag.Name)
	}
	return tagNames
}

// IsCapabilityEnabled checks whether a given OpenShift capability (e.g. "Console")
// is enabled on the cluster.
func IsCapabilityEnabled(c *clients.Clients, name string) bool {
	cv, err := c.ClusterVersion.Get(c.Ctx, "version", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get ClusterVersion")

	for _, capability := range cv.Status.Capabilities.EnabledCapabilities {
		if string(capability) == name {
			return true
		}
	}
	return false
}

// GetOpenShiftVersion returns the desired OpenShift version string (e.g. "4.16.3").
func GetOpenShiftVersion(c *clients.Clients) string {
	cv, err := c.ClusterVersion.Get(c.Ctx, "version", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to get ClusterVersion")
	return cv.Status.Desired.Version
}

// Ensure unused imports don't fire.
var _ = GinkgoWriter
