package pipelines

import (
	"context"
	"fmt"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
)

// WaitNestedResolverConfig observes the operator-managed values without polling.
func WaitNestedResolverConfig(ctx context.Context, client coreclient.ConfigMapInterface, name string, expected map[string]string) error {
	options := metav1.ListOptions{FieldSelector: "metadata.name=" + name}
	list, err := client.List(ctx, options)
	if err != nil {
		return err
	}
	matches := func(cm *corev1.ConfigMap) bool {
		for key, value := range expected {
			if cm.Data[key] != value {
				return false
			}
		}
		return true
	}
	for i := range list.Items {
		if matches(&list.Items[i]) {
			return nil
		}
	}
	options.ResourceVersion = list.ResourceVersion
	stream, err := client.Watch(ctx, options)
	if err != nil {
		return err
	}
	defer stream.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %s: %w", name, ctx.Err())
		case event, open := <-stream.ResultChan():
			if !open {
				return fmt.Errorf("%s watch closed before configuration was applied", name)
			}
			if event.Type == watch.Error {
				return apierrors.FromObject(event.Object)
			}
			if cm, ok := event.Object.(*corev1.ConfigMap); ok && event.Type != watch.Deleted && matches(cm) {
				return nil
			}
		}
	}
}

// RestartNestedResolvers replaces the healthy resolver pods after a policy change.
// New processes load their ConfigMaps before serving resolution requests.
// This works with either Deployment- or StatefulSet-managed resolver pods.
func RestartNestedResolvers(ctx context.Context, kube kubernetes.Interface, namespace string) (string, error) {
	client := kube.CoreV1().Pods(namespace)
	options := metav1.ListOptions{LabelSelector: "app.kubernetes.io/component=resolvers,app.kubernetes.io/part-of=tekton-pipelines"}
	list, err := client.List(ctx, options)
	if err != nil {
		return "", err
	}
	if len(list.Items) == 0 {
		return "", fmt.Errorf("no resolver pods in %s", namespace)
	}
	old := make(map[types.UID]bool, len(list.Items))
	current := make(map[types.UID]*corev1.Pod, len(list.Items))
	serviceAccount := list.Items[0].Spec.ServiceAccountName
	if serviceAccount == "" {
		return "", fmt.Errorf("resolver pod has no explicit service account")
	}
	for i := range list.Items {
		pod := &list.Items[i]
		if pod.UID == "" || pod.Spec.ServiceAccountName != serviceAccount || !resolverPodReady(pod) {
			return "", fmt.Errorf("resolver pod %s must be healthy with a consistent service account before restart", pod.Name)
		}
		old[pod.UID] = true
		current[pod.UID] = pod
	}
	options.ResourceVersion = list.ResourceVersion
	stream, err := client.Watch(ctx, options)
	if err != nil {
		return "", err
	}
	defer stream.Stop()
	for i := range list.Items {
		pod := &list.Items[i]
		if err := client.Delete(ctx, pod.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &pod.UID}}); err != nil {
			return "", err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("restart resolver pods: %w", ctx.Err())
		case event, open := <-stream.ResultChan():
			if !open {
				return "", fmt.Errorf("resolver pod watch closed before restart completed")
			}
			if event.Type == watch.Error {
				return "", apierrors.FromObject(event.Object)
			}
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			if event.Type == watch.Deleted {
				delete(current, pod.UID)
			} else {
				current[pod.UID] = pod
			}
			ready := 0
			oldRemaining := false
			for uid, candidate := range current {
				if old[uid] {
					oldRemaining = true
				} else if candidate.Spec.ServiceAccountName == serviceAccount && resolverPodReady(candidate) {
					ready++
				}
			}
			if !oldRemaining && ready >= len(old) {
				return serviceAccount, nil
			}
		}
	}
}

func resolverPodReady(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// NestedPipelineReadAllowed evaluates a real service account's access to the
// specific Pipeline. Resolver namespace policy is checked separately by execution.
func NestedPipelineReadAllowed(ctx context.Context, kube kubernetes.Interface, identity types.NamespacedName, pipeline types.NamespacedName) (bool, error) {
	review, err := kube.AuthorizationV1().SubjectAccessReviews().Create(ctx, &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User: "system:serviceaccount:" + identity.Namespace + ":" + identity.Name,
			Groups: []string{
				"system:serviceaccounts",
				"system:serviceaccounts:" + identity.Namespace,
				"system:authenticated",
			},
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: pipeline.Namespace, Verb: "get", Group: "tekton.dev",
				Version: "v1", Resource: "pipelines", Name: pipeline.Name,
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}
	if review.Status.EvaluationError != "" {
		return false, fmt.Errorf("evaluate Pipeline access: %s", review.Status.EvaluationError)
	}
	return review.Status.Allowed, nil
}
