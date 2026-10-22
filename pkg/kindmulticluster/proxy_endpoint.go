package kindmulticluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

type proxyEndpoint struct {
	address string
	token   string
	tls     *tls.Config
}

func withProxy(ctx context.Context, hub *clients.Clients, proof func(context.Context, proxyEndpoint) error) error {
	// Ginkgo's SpecContext cancels at NodeTimeout but has no Deadline value.
	// Keep the entire log proof inside the requested token lifetime.
	ctx, cancel := context.WithTimeout(ctx, 9*time.Minute)
	defer cancel()
	seconds := int64(600)
	issuedAt := time.Now()
	token, err := hub.KubeClient.Kube.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, "release-log-reader",
		&authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &seconds}}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("issue bounded hub log token: %w", err)
	}
	if token.Status.Token == "" || !token.Status.ExpirationTimestamp.After(issuedAt) ||
		token.Status.ExpirationTimestamp.After(issuedAt.Add(11*time.Minute)) {
		return fmt.Errorf("hub returned an empty token or an expiration outside the ten-minute request")
	}
	secret, err := hub.KubeClient.Kube.CoreV1().Secrets("tekton-pipelines").Get(ctx, "proxy-server-tls", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read proxy trust certificate: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(secret.Data["tls.crt"]) {
		return fmt.Errorf("proxy TLS secret contains no certificate")
	}
	pods, err := hub.KubeClient.Kube.CoreV1().Pods("tekton-pipelines").List(ctx, metav1.ListOptions{LabelSelector: "app=proxy-aae"})
	if err != nil {
		return fmt.Errorf("find hub proxy pod: %w", err)
	}
	if len(pods.Items) != 1 {
		return fmt.Errorf("expected one hub proxy pod, found %d", len(pods.Items))
	}
	transport, upgrader, err := spdy.RoundTripperFor(hub.KubeConfig)
	if err != nil {
		return fmt.Errorf("configure proxy port-forward transport: %w", err)
	}
	address := hub.KubeClient.Kube.CoreV1().RESTClient().Post().Resource("pods").
		Namespace("tekton-pipelines").Name(pods.Items[0].Name).SubResource("portforward").URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, address)
	ready := make(chan struct{})
	forward, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{"0:8080"}, ctx.Done(), ready, io.Discard, io.Discard)
	if err != nil {
		return fmt.Errorf("create proxy tunnel: %w", err)
	}
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(forward.ForwardPorts)
	group.Go(func() error {
		defer cancel()
		select {
		case <-groupCtx.Done():
			return groupCtx.Err()
		case <-ready:
		}
		ports, err := forward.GetPorts()
		if err != nil {
			return fmt.Errorf("read allocated proxy port: %w", err)
		}
		if len(ports) != 1 {
			return fmt.Errorf("expected one allocated proxy port, got %d", len(ports))
		}
		return proof(groupCtx, proxyEndpoint{
			address: fmt.Sprintf("wss://127.0.0.1:%d", ports[0].Local),
			token:   token.Status.Token,
			tls:     &tls.Config{RootCAs: roots, ServerName: "proxy-aae.tekton-pipelines.svc", MinVersion: tls.VersionTLS12},
		})
	})
	return group.Wait()
}
