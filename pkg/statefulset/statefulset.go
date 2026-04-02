package statefulset

import (
	"context"
	"fmt"
	"log"

	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// ValidateStatefulSetDeployment waits until the named StatefulSet exists and
// has all replicas ready in the target namespace.
func ValidateStatefulSetDeployment(cs *clients.Clients, deploymentName string) {
	var labelSelector string
	switch deploymentName {
	case "tekton-pipelines-controller", "tekton-pipelines-remote-resolvers":
		labelSelector = "app.kubernetes.io/part-of=tekton-pipelines"
	case "tekton-chains-controller":
		labelSelector = "app.kubernetes.io/part-of=tekton-chains"
	case "tekton-results-watcher":
		labelSelector = "app.kubernetes.io/name=tekton-results-watcher"
	default:
		log.Printf("No labelSelector found for deployment: %s", deploymentName)
	}
	listOptions := metav1.ListOptions{LabelSelector: labelSelector}

	log.Printf("Starting validation for StatefulSet deployment: %s in namespace: %s", deploymentName, config.TargetNamespace)

	waitErr := wait.PollUntilContextTimeout(context.TODO(), config.APIRetry, config.APITimeout, true, func(ctx context.Context) (bool, error) {
		stsList, err := cs.KubeClient.Kube.AppsV1().StatefulSets(config.TargetNamespace).List(context.TODO(), listOptions)
		if err != nil {
			log.Printf("Error listing StatefulSets: %v", err)
			return false, fmt.Errorf("failed to list StatefulSets: %v", err)
		}

		log.Printf("Found %d StatefulSets in namespace %s", len(stsList.Items), config.TargetNamespace)

		for _, sts := range stsList.Items {
			if sts.Name == deploymentName {
				log.Printf("Found StatefulSet: %s", sts.Name)
				isAvailable := IsStatefulSetAvailable(&sts)
				if isAvailable {
					log.Printf("StatefulSet %s is available and ready", sts.Name)
					return true, nil
				}
				log.Printf("StatefulSet %s is not ready yet", sts.Name)
				return false, nil
			}
		}
		log.Printf("StatefulSet %s not found yet. Continuing to wait...", deploymentName)
		return false, nil
	})

	Expect(waitErr).NotTo(HaveOccurred(),
		"StatefulSet %s was not found or not available within timeout in namespace %q",
		deploymentName, config.TargetNamespace)
}

// IsStatefulSetAvailable checks whether a StatefulSet has all replicas ready.
func IsStatefulSetAvailable(sts *appsv1.StatefulSet) bool {
	if sts.Spec.Replicas == nil {
		return false
	}
	return sts.Status.ReadyReplicas == *sts.Spec.Replicas
}
