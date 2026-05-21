package k8s

import (
	"context"
	"log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

// GetClusterArchitecture detects the cluster architecture by querying a node.
// Returns the architecture (e.g., "amd64", "arm64", "ppc64le", "s390x") or empty string on error.
func GetClusterArchitecture(cs *clients.Clients) string {
	nodes, err := cs.KubeClient.Kube.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{Limit: 1})
	if err != nil {
		log.Printf("Failed to get cluster nodes for architecture detection: %v", err)
		return ""
	}

	if len(nodes.Items) == 0 {
		log.Printf("No nodes found for architecture detection")
		return ""
	}

	// Get architecture from first node
	arch := nodes.Items[0].Status.NodeInfo.Architecture
	log.Printf("Detected cluster architecture: %s", arch)
	return arch
}
