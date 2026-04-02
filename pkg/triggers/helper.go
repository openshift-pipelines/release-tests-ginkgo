package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

const (
	// MaxIdleConnections specifies max connection to the http client
	MaxIdleConnections int = 30
	// RequestTimeout specifies request timeout with http client
	RequestTimeout int = 15
)

// CreateHTTPClient creates an HTTP client for connection re-use.
func CreateHTTPClient() *http.Client {
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: MaxIdleConnections,
		},
		Timeout: time.Duration(RequestTimeout) * time.Second,
	}
	return client
}

// CreateHTTPSClient creates an HTTPS client with TLS certificates for connection re-use.
func CreateHTTPSClient() *http.Client {
	// Load client cert
	cert, err := tls.LoadX509KeyPair(config.Path("testdata/triggers/certs/server.crt"), config.Path("testdata/triggers/certs/server.key"))
	if err != nil {
		log.Fatal(err)
	}
	caCert, err := os.ReadFile(config.Path("testdata/triggers/certs/ca.crt"))
	if err != nil {
		log.Fatal(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: MaxIdleConnections,
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      caCertPool,
				MinVersion:   tls.VersionTLS12,
			},
		},
		Timeout: time.Duration(RequestTimeout) * time.Second,
	}
	return client
}

// GetSignature is a HMAC SHA256 generator.
func GetSignature(input []byte, key string) string {
	keyForSign := []byte(key)
	h := hmac.New(sha256.New, keyForSign)
	_, err := h.Write(input)
	Expect(err).NotTo(HaveOccurred(), "could not generate signature")
	return hex.EncodeToString(h.Sum(nil))
}

// BuildHeaders adds interceptor-specific headers to the HTTP request.
// The payload parameter is used for HMAC signature calculation.
func BuildHeaders(req *http.Request, interceptor, eventType string, payload []byte) *http.Request {
	switch strings.ToLower(interceptor) {
	case "github":
		log.Printf("Building headers for github interceptor..")
		req.Header.Add("Accept", "application/json")
		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("X-Hub-Signature-256", "sha256="+GetSignature(payload, config.TriggersSecretToken))
		req.Header.Add("X-GitHub-Event", eventType)
	case "gitlab":
		log.Printf("Building headers for gitlab interceptor..")
		req.Header.Add("Accept", "application/json")
		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("X-GitLab-Token", config.TriggersSecretToken)
		req.Header.Add("X-Gitlab-Event", eventType)
	case "bitbucket":
		log.Printf("Building headers for bitbucket interceptor..")
		req.Header.Add("Accept", "application/json")
		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("X-Hub-Signature", "sha256="+GetSignature(payload, config.TriggersSecretToken))
		req.Header.Add("X-Event-Key", "repo:"+eventType)
	default:
		Fail("unsupported interceptor type: " + interceptor)
	}
	return req
}
