// Package shipwright provides reusable steps for Shipwright Build e2e tests.
package shipwright

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
)

// Shipwright API group, testdata placeholder, BuildRun service account and timeout.
const (
	APIGroup             = "shipwright.io"
	NamespacePlaceholder = "##NAMESPACE##"
	ServiceAccount       = "pipeline"
	BuildRunTimeout      = 20 * time.Minute
	internalRegistry     = "image-registry.openshift-image-registry.svc:5000/"
)

// GroupVersionResources of the Shipwright Build API.
var (
	BuildGVR                = schema.GroupVersionResource{Group: APIGroup, Version: "v1beta1", Resource: "builds"}
	BuildRunGVR             = schema.GroupVersionResource{Group: APIGroup, Version: "v1beta1", Resource: "buildruns"}
	ClusterBuildStrategyGVR = schema.GroupVersionResource{Group: APIGroup, Version: "v1beta1", Resource: "clusterbuildstrategies"}
)

// BuildResult holds what RunBuild observed for a successful build.
type BuildResult struct {
	BuildName    string
	BuildRunName string
	Strategy     string
	Image        string
	Digest       string
}

// SkipIfNotInstalled skips the spec when the Shipwright Build API is not available.
func SkipIfNotInstalled(cs *clients.Clients) {
	if !k8s.IsAPIGroupAvailable(cs, APIGroup) {
		Skip("Shipwright Build API (shipwright.io) is not available: install the Builds for Red Hat OpenShift operator")
	}
}

// RunBuild creates the Build from buildFile, runs it and checks the BuildRun succeeds.
func RunBuild(cs *clients.Clients, buildFile, ns string) *BuildResult {
	build := LoadBuild(buildFile, ns)
	res := &BuildResult{BuildName: build.GetName()}
	res.Strategy, _, _ = unstructured.NestedString(build.Object, "spec", "strategy", "name")
	res.Image, _, _ = unstructured.NestedString(build.Object, "spec", "output", "image")

	By(fmt.Sprintf("Checking ClusterBuildStrategy %q is installed", res.Strategy))
	ValidateClusterBuildStrategy(cs, res.Strategy)

	By(fmt.Sprintf("Creating Build %s/%s", ns, res.BuildName))
	CreateBuild(cs, build, ns)
	DeferCleanup(func() { DeleteBuildResources(cs, res, ns) })
	WaitForBuildRegistered(cs, res.BuildName, ns)

	By(fmt.Sprintf("Running a BuildRun for Build %s/%s", ns, res.BuildName))
	res.BuildRunName = StartBuildRun(cs, res.BuildName, ns)
	res.Digest = ValidateBuildRunSucceeded(cs, res.BuildRunName, ns)

	if strings.HasPrefix(res.Image, internalRegistry) {
		By("Checking the image was pushed to the internal registry")
		ValidateImageStreamTag(res.Image, ns, res.Digest)
	}
	return res
}

// LoadBuild reads a Build from buildFile and sets its namespace to ns.
func LoadBuild(buildFile, ns string) *unstructured.Unstructured {
	raw, err := os.ReadFile(config.Path(buildFile))
	Expect(err).NotTo(HaveOccurred(), "failed to read %s", buildFile)
	raw = bytes.ReplaceAll(raw, []byte(NamespacePlaceholder), []byte(ns))

	build := &unstructured.Unstructured{}
	Expect(k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096).Decode(&build.Object)).
		To(Succeed(), "failed to decode %s", buildFile)
	Expect(build.GetKind()).To(Equal("Build"), "%s must define a Shipwright Build", buildFile)
	build.SetNamespace(ns)
	return build
}

// ValidateClusterBuildStrategy checks the named ClusterBuildStrategy exists and has steps.
func ValidateClusterBuildStrategy(cs *clients.Clients, name string) {
	cbs, err := cs.Dynamic.Resource(ClusterBuildStrategyGVR).Get(cs.Ctx, name, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "ClusterBuildStrategy %q not found", name)
	steps, _, _ := unstructured.NestedSlice(cbs.Object, "spec", "steps")
	Expect(steps).NotTo(BeEmpty(), "ClusterBuildStrategy %q has no steps", name)
}

// CreateBuild replaces any existing Build with the same name and creates build in ns.
func CreateBuild(cs *clients.Clients, build *unstructured.Unstructured, ns string) {
	DeleteResource(cs, BuildGVR, build.GetName(), ns)
	_, err := cs.Dynamic.Resource(BuildGVR).Namespace(ns).Create(cs.Ctx, build, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create Build %s/%s", ns, build.GetName())
	log.Printf("created Build %s/%s", ns, build.GetName())
}

func WaitForBuildRegistered(cs *clients.Clients, name, ns string) {
	var registered, reason, message string
	err := wait.PollUntilContextTimeout(cs.Ctx, config.APIRetry, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		b, err := cs.Dynamic.Resource(BuildGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		registered, _, _ = unstructured.NestedString(b.Object, "status", "registered")
		reason, _, _ = unstructured.NestedString(b.Object, "status", "reason")
		message, _, _ = unstructured.NestedString(b.Object, "status", "message")
		return registered == "True", nil
	})
	Expect(err).NotTo(HaveOccurred(), "Build %s/%s was not registered: registered=%q reason=%q message=%q",
		ns, name, registered, reason, message)
}

// StartBuildRun creates a BuildRun for buildName and returns its name.
func StartBuildRun(cs *clients.Clients, buildName, ns string) string {
	br := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": APIGroup + "/v1beta1",
		"kind":       "BuildRun",
		"metadata": map[string]interface{}{
			"generateName": buildName + "-run-",
			"namespace":    ns,
		},
		"spec": map[string]interface{}{
			"build":          map[string]interface{}{"name": buildName},
			"serviceAccount": ServiceAccount,
		},
	}}
	created, err := cs.Dynamic.Resource(BuildRunGVR).Namespace(ns).Create(cs.Ctx, br, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create BuildRun for Build %s/%s", ns, buildName)
	log.Printf("created BuildRun %s/%s", ns, created.GetName())
	return created.GetName()
}

// ValidateBuildRunSucceeded waits for the BuildRun to succeed and returns the image digest.
func ValidateBuildRunSucceeded(cs *clients.Clients, name, ns string) string {
	var br *unstructured.Unstructured
	var status, reason, message string
	err := wait.PollUntilContextTimeout(cs.Ctx, 10*time.Second, BuildRunTimeout, true, func(ctx context.Context) (bool, error) {
		var err error
		br, err = cs.Dynamic.Resource(BuildRunGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		status, reason, message = succeededCondition(br)
		return status == "True" || status == "False", nil
	})
	if err != nil || status != "True" {
		DumpBuildRunLogs(name, ns)
		Fail(fmt.Sprintf("BuildRun %s/%s did not succeed within %s: status=%q reason=%q message=%q",
			ns, name, BuildRunTimeout, status, reason, message))
	}

	digest, _, _ := unstructured.NestedString(br.Object, "status", "output", "digest")
	Expect(digest).To(HavePrefix("sha256:"), "BuildRun %s/%s status.output.digest", ns, name)
	log.Printf("BuildRun %s/%s succeeded, image digest %s", ns, name, digest)
	return digest
}

func succeededCondition(br *unstructured.Unstructured) (status, reason, message string) {
	conds, _, _ := unstructured.NestedSlice(br.Object, "status", "conditions")
	for _, c := range conds {
		m, ok := c.(map[string]interface{})
		if !ok || m["type"] != "Succeeded" {
			continue
		}
		status, _ = m["status"].(string)
		reason, _ = m["reason"].(string)
		message, _ = m["message"].(string)
		break
	}
	return status, reason, message
}

// ValidateImageStreamTag checks the ImageStreamTag for image points at digest.
func ValidateImageStreamTag(image, ns, digest string) {
	istag := image[strings.LastIndex(image, "/")+1:]
	if !strings.Contains(istag, ":") {
		istag += ":latest"
	}
	out := cmd.MustSucceed("oc", "get", "istag", istag, "-n", ns, "-o", "jsonpath={.image.metadata.name}").Stdout()
	Expect(strings.TrimSpace(out)).To(Equal(digest), "ImageStreamTag %s/%s digest", ns, istag)
}

// DumpBuildRunLogs writes the BuildRun pod logs to the Ginkgo output.
func DumpBuildRunLogs(name, ns string) {
	res := cmd.Run("oc", "logs", "-l", "buildrun.shipwright.io/name="+name, "-n", ns,
		"--all-containers", "--prefix", "--tail=-1")
	GinkgoWriter.Printf("----- logs of BuildRun %s/%s -----\n%s%s\n----- end of logs -----\n",
		ns, name, res.Stdout(), res.Stderr())
}

// DeleteBuildResources deletes the BuildRun and Build recorded in res.
func DeleteBuildResources(cs *clients.Clients, res *BuildResult, ns string) {
	if res.BuildRunName != "" {
		DeleteResource(cs, BuildRunGVR, res.BuildRunName, ns)
	}
	DeleteResource(cs, BuildGVR, res.BuildName, ns)
}

// DeleteResource deletes a namespaced object if present and waits until it is gone.
func DeleteResource(cs *clients.Clients, gvr schema.GroupVersionResource, name, ns string) {
	client := cs.Dynamic.Resource(gvr).Namespace(ns)
	if err := client.Delete(cs.Ctx, name, metav1.DeleteOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		Expect(err).NotTo(HaveOccurred(), "failed to delete %s %s/%s", gvr.Resource, ns, name)
	}
	Eventually(func() bool {
		_, err := client.Get(cs.Ctx, name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, 2*time.Minute, config.APIRetry).Should(BeTrue(), "%s %s/%s was not deleted", gvr.Resource, ns, name)
}
