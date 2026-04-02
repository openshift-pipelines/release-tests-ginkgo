package triggers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	resource "github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/opc"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/wait"
	"github.com/tektoncd/pipeline/pkg/names"
	eventReconciler "github.com/tektoncd/triggers/pkg/reconciler/eventlistener"
	"github.com/tektoncd/triggers/pkg/reconciler/eventlistener/resources"
	"github.com/tektoncd/triggers/pkg/sink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

func getServiceNameAndPort(c *clients.Clients, elname, namespace string) (string, string) {
	// Verify the EventListener to be ready
	err := wait.WaitFor(c.Ctx, wait.EventListenerReady(c, namespace, elname))
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("event listener %s in namespace %s not ready", elname, namespace))

	labelSelector := fields.SelectorFromSet(resources.GenerateLabels(elname, resources.DefaultStaticResourceLabels)).String()
	// Grab EventListener sink pods
	sinkPods, err := c.KubeClient.Kube.CoreV1().Pods(namespace).List(c.Ctx, metav1.ListOptions{LabelSelector: labelSelector})
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to list event listener %s sink pods in namespace %s", elname, namespace))

	log.Printf("sinkpod name: %s", sinkPods.Items[0].Name)

	serviceList, err := c.KubeClient.Kube.CoreV1().Services(namespace).List(c.Ctx, metav1.ListOptions{LabelSelector: labelSelector})
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to list services with label selector %s in namespace %s", labelSelector, namespace))
	return serviceList.Items[0].Name, serviceList.Items[0].Spec.Ports[0].Name
}

// ExposeEventListener exposes an EventListener service as an OpenShift route and returns the route URL.
func ExposeEventListener(c *clients.Clients, elname, namespace string) string {
	_, err := opc.VerifyResourceListMatchesName("eventlistener", elname, namespace)
	Expect(err).NotTo(HaveOccurred())

	svcName, _ := getServiceNameAndPort(c, elname, namespace)
	cmd.MustSucceed("oc", "expose", "service", svcName, "-n", namespace)

	return GetRoute(elname, namespace)
}

// ExposeEventListenerForTLS exposes an EventListener with TLS and returns the route URL.
func ExposeEventListenerForTLS(c *clients.Clients, elname, namespace string) string {
	svcName, portName := getServiceNameAndPort(c, elname, namespace)
	domain := getDomain()
	cmd.MustSucceed("mkdir", "-p", resource.Path("testdata/triggers/certs")).Stdout()

	caKey := resource.Path("testdata/triggers/certs/ca.key")
	caCrt := resource.Path("testdata/triggers/certs/ca.crt")
	serverKey := resource.Path("testdata/triggers/certs/server.key")
	serverCrt := resource.Path("testdata/triggers/certs/server.crt")
	serverCsr := resource.Path("testdata/triggers/certs/server.csr")
	serverExt := resource.Path("testdata/triggers/certs/server.ext")

	// first 3 files can be reused so they are committed in git repository
	if _, err := os.Stat(caKey); errors.Is(err, os.ErrNotExist) {
		log.Println("Generating ca.key")
		cmd.MustSucceed("openssl", "genrsa", "-out", caKey, "4096").Stdout()
	}

	if _, err := os.Stat(caCrt); errors.Is(err, os.ErrNotExist) {
		log.Println("Generating ca.crt")
		cmd.MustSucceed("openssl", "req", "-x509", "-new", "-nodes", "-key", caKey,
			"-sha256", "-days", "4096", "-out", caCrt, "-subj",
			"/C=IN/ST=Kar/L=Blr/O=RedHat").Stdout()
	}

	if _, err := os.Stat(serverKey); errors.Is(err, os.ErrNotExist) {
		log.Println("Generating server.key")
		cmd.MustSucceed("openssl", "genrsa", "-out", serverKey, "4096").Stdout()
	}

	// other files depend on domain name which changes for every test cluster
	log.Println("Generating server.csr")
	cmd.MustSucceed("openssl", "req", "-new", "-key", serverKey, "-out", serverCsr,
		"-subj", fmt.Sprintf("/C=IN/ST=Kar/L=Blr/O=RedHat/CN=%s", domain)).Stdout()

	extData := fmt.Sprintf("authorityKeyIdentifier=keyid,issuer\nbasicConstraints=CA:FALSE\nkeyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment\n"+
		"subjectAltName = @alt_names\n\n\n[alt_names]\nDNS.1 = %s\n", domain)

	log.Println("Generating server.ext")
	err := os.WriteFile(serverExt, []byte(extData), 0600)
	Expect(err).NotTo(HaveOccurred(), "failed to write server.ext")

	log.Println("Generating server.crt")
	cmd.MustSucceed("openssl", "x509", "-req", "-in", serverCsr, "-CA", caCrt, "-CAkey",
		caKey, "-CAcreateserial", "-out", serverCrt,
		"-days", "4096", "-sha256", "-extfile", serverExt).Stdout()

	log.Println("Creating route")
	routeName := cmd.MustSucceed("oc", "create", "route", "reencrypt", "--ca-cert="+caCrt,
		"--cert="+serverCrt, "--key="+serverKey,
		"--service="+svcName, "--hostname="+domain, "--port="+portName, "-n", namespace).Stdout()

	routeURL := cmd.MustSucceed("oc", "-n", namespace, "get", strings.Split(routeName, " ")[0], "--template='http://{{.spec.host}}'").Stdout()
	log.Printf("Route url: %s", routeURL)

	time.Sleep(5 * time.Second)
	return strings.Trim(routeURL, "'")
}

// getDomain returns the formatted hostname for TLS route creation.
func getDomain() string {
	// extract cluster's domain from ingress config, e.g. apps.mycluster.example.com
	routeDomainName := cmd.MustSucceed("oc", "get", "ingresses.config/cluster", "-o", "jsonpath={.spec.domain}").Stdout()
	randomName := names.SimpleNameGenerator.RestrictLengthWithRandomSuffix("rt")
	return "tls." + randomName + "." + routeDomainName
}

// MockPostEventWithEmptyPayload sends an empty POST request to the EventListener sink.
func MockPostEventWithEmptyPayload(routeurl string) *http.Response {
	req, err := http.NewRequest("POST", routeurl, bytes.NewBuffer([]byte("{}")))
	Expect(err).NotTo(HaveOccurred(), "failed to create HTTP request")

	req.Header.Add("Accept", "application/json")
	resp, err := CreateHTTPClient().Do(req)
	Expect(err).NotTo(HaveOccurred(), "failed to execute HTTP request")

	Expect(resp.StatusCode).To(BeNumerically("<=", http.StatusAccepted),
		fmt.Sprintf("sink did not return 2xx response. Got status code: %d", resp.StatusCode))
	return resp
}

// MockPostEvent sends a POST request to the EventListener sink with interceptor-specific headers.
// It returns both the HTTP response and the payload bytes (for downstream use).
func MockPostEvent(routeurl, interceptor, eventType, payload string, isTLS bool) (*http.Response, []byte) {
	var (
		req  *http.Request
		err  error
		resp *http.Response
	)
	eventBodyJSON, err := os.ReadFile(resource.Path(payload))
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("could not load test data from file %s", payload))

	// Send POST request to EventListener sink
	if isTLS {
		req, err = http.NewRequest("POST", "https://"+strings.Split(routeurl, "//")[1], bytes.NewBuffer(eventBodyJSON))
	} else {
		req, err = http.NewRequest("POST", routeurl, bytes.NewBuffer(eventBodyJSON))
	}
	Expect(err).NotTo(HaveOccurred(), "failed to create HTTP request")

	req = BuildHeaders(req, interceptor, eventType, eventBodyJSON)

	if isTLS {
		resp, err = CreateHTTPSClient().Do(req)
	} else {
		resp, err = CreateHTTPClient().Do(req)
	}
	Expect(err).NotTo(HaveOccurred(), "failed to execute HTTP request")

	Expect(resp.StatusCode).To(BeNumerically("<=", http.StatusAccepted),
		fmt.Sprintf("sink did not return 2xx response. Got status code: %d", resp.StatusCode))
	return resp, eventBodyJSON
}

// AssertElResponse asserts the EventListener response body and checks sink pod logs for errors.
func AssertElResponse(c *clients.Clients, resp *http.Response, elname, namespace string) {
	wantBody := sink.Response{
		EventListener: elname,
		Namespace:     namespace,
	}
	//nolint:errcheck
	defer resp.Body.Close()
	var gotBody sink.Response
	err := json.NewDecoder(resp.Body).Decode(&gotBody)
	Expect(err).NotTo(HaveOccurred(), "failed to decode sink response")

	if diff := cmp.Diff(wantBody, gotBody, cmpopts.IgnoreFields(sink.Response{}, "EventID", "EventListenerUID")); diff != "" {
		Fail(fmt.Sprintf("unexpected sink response -want/+got: %s", diff))
	}

	Expect(gotBody.EventID).NotTo(BeEmpty(), "sink response has no eventID")

	labelSelector := fields.SelectorFromSet(resources.GenerateLabels(elname, resources.DefaultStaticResourceLabels)).String()
	// Grab EventListener sink pods
	sinkPods, err := c.KubeClient.Kube.CoreV1().Pods(namespace).List(c.Ctx, metav1.ListOptions{LabelSelector: labelSelector})
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to list event listener sink pods with label selector %s in namespace %s", labelSelector, namespace))

	logs := cmd.MustSucceed("oc", "-n", namespace, "logs", "pods/"+sinkPods.Items[0].Name, "--all-containers", "--tail=2").Stdout()
	if strings.Contains(logs, "error") {
		GinkgoWriter.Printf("Error: sink logs: \n %s", logs)
		Fail(fmt.Sprintf("Error: sink logs: \n %s", logs))
	}
}

// CleanupTriggers deletes an EventListener and waits for generated resources to be removed.
func CleanupTriggers(c *clients.Clients, elName, namespace string) {
	// Delete EventListener
	err := c.TriggersClient.TriggersV1alpha1().EventListeners(namespace).Delete(c.Ctx, elName, metav1.DeleteOptions{})
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to delete event listener %s", elName))

	log.Println("Deleted EventListener")

	// Verify the EventListener's Deployment is deleted
	err = wait.WaitFor(c.Ctx, wait.DeploymentNotExist(c, namespace, fmt.Sprintf("%s-%s", eventReconciler.GeneratedResourcePrefix, elName)))
	Expect(err).NotTo(HaveOccurred(), "EventListener's Deployment was not deleted")

	log.Println("EventListener's Deployment was deleted")

	// Verify the EventListener's Service is deleted
	err = wait.WaitFor(c.Ctx, wait.ServiceNotExist(c, namespace, fmt.Sprintf("%s-%s", eventReconciler.GeneratedResourcePrefix, elName)))
	Expect(err).NotTo(HaveOccurred(), "EventListener's Service was not deleted")

	log.Println("EventListener's Service was deleted")

	// Delete Route exposed earlier
	err = c.Route.Routes(namespace).Delete(c.Ctx, fmt.Sprintf("%s-%s", eventReconciler.GeneratedResourcePrefix, elName), metav1.DeleteOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to delete EventListener's Route")

	// Verify the EventListener's Route is deleted
	err = wait.WaitFor(c.Ctx, wait.RouteNotExist(c, namespace, fmt.Sprintf("%s-%s", eventReconciler.GeneratedResourcePrefix, elName)))
	Expect(err).NotTo(HaveOccurred(), "EventListener's Route was not deleted")
	log.Println("EventListener's Route got deleted successfully...")

	// Clean up TLS cert files
	cmd.MustSucceed("rm", "-rf", resource.Path("testdata/triggers/certs"))
}

// GetRoute retrieves the route for an EventListener and returns the route URL.
func GetRoute(elname, namespace string) string {
	route := cmd.MustSucceed("oc", "-n", namespace, "get", "route", "--selector=eventlistener="+elname, "-o", "jsonpath='{range .items[*]}{.metadata.name}'").Stdout()
	serverCert := cmd.MustSucceed("oc", "-n", namespace, "get", "route", "--selector=eventlistener="+elname, "-o", "jsonpath='{.items[].spec.tls.certificate}'").Stdout()
	serverCert = strings.Trim(serverCert, "'")

	// event listener is using TLS
	if serverCert != "" {
		file, err := os.Create(resource.Path("testdata/triggers/certs/server.crt"))
		Expect(err).NotTo(HaveOccurred(), "failed to create server.crt file")
		//nolint:errcheck
		defer file.Close()

		_, err = file.WriteString(serverCert)
		Expect(err).NotTo(HaveOccurred(), "failed to write server.crt")
	}
	return GetRouteURL(route, namespace)
}

// GetRouteURL retrieves the URL for a given route name.
func GetRouteURL(routeName, namespace string) string {
	routeURL := cmd.MustSucceed("oc", "-n", namespace, "get", "route", strings.Trim(routeName, "'"), "--template='http://{{.spec.host}}'").Stdout()
	log.Printf("Route url: %s", routeURL)

	time.Sleep(5 * time.Second)
	return strings.Trim(routeURL, "'")
}
