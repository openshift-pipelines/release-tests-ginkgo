package tektonkueue

import (
	"context"
	"fmt"
	"strings"

	operatorv1alpha1 "github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var deploymentGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

func componentDeploymentsReady(ctx context.Context, cluster Cluster, component *unstructured.Unstructured) error {
	namespace, _, err := unstructured.NestedString(component.Object, "spec", "targetNamespace")
	if err != nil || namespace == "" {
		return fmt.Errorf("%s has no valid targetNamespace", component.GetName())
	}
	deployments, err := cluster.Clients.Dynamic.Resource(deploymentGVR).Namespace(namespace).
		List(ctx, metav1.ListOptions{LabelSelector: operatorv1alpha1.LabelOperandName + "=" + component.GetName()})
	if err != nil {
		return err
	}
	if len(deployments.Items) == 0 {
		return fmt.Errorf("no matching Deployments in %s", namespace)
	}
	for _, object := range deployments.Items {
		var deployment appsv1.Deployment
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &deployment); err != nil {
			return err
		}
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		available := false
		for _, condition := range deployment.Status.Conditions {
			if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
				available = true
			}
		}
		if deployment.DeletionTimestamp != nil || desired == 0 || deployment.Status.ObservedGeneration < deployment.Generation ||
			deployment.Status.UpdatedReplicas != desired || deployment.Status.AvailableReplicas < desired || !available {
			return fmt.Errorf("deployment %s/%s is stale or unavailable: generation=%d observed=%d desired=%d updated=%d available=%d",
				namespace, deployment.Name, deployment.Generation, deployment.Status.ObservedGeneration,
				desired, deployment.Status.UpdatedReplicas, deployment.Status.AvailableReplicas)
		}
	}
	return nil
}

func (e *Environment) proxyReady(ctx context.Context, requestEndpoint bool) error {
	component, err := e.Hub.Clients.Dynamic.Resource(operatorResource("tektonmulticlusterproxyaaes")).
		Get(ctx, operatorv1alpha1.MultiClusterProxyAAEResourceName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	namespace, _, err := unstructured.NestedString(component.Object, "spec", "targetNamespace")
	if err != nil {
		return err
	}
	deployments, err := e.Hub.Clients.Dynamic.Resource(deploymentGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: operatorv1alpha1.LabelOperandName + "=" + operatorv1alpha1.MultiClusterProxyAAEResourceName,
	})
	if err != nil {
		return fmt.Errorf("list hub proxy Deployments: %w", err)
	}
	if len(deployments.Items) == 0 {
		return fmt.Errorf("hub proxy has no matching Deployment")
	}
	for _, object := range deployments.Items {
		var deployment appsv1.Deployment
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &deployment); err != nil {
			return err
		}
		if err := e.proxyDeploymentReady(ctx, &deployment, requestEndpoint); err != nil {
			return err
		}
	}
	return nil
}

func (e *Environment) proxyDeploymentReady(ctx context.Context, deployment *appsv1.Deployment, requestEndpoint bool) error {
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil || selector.Empty() {
		return fmt.Errorf("hub proxy Deployment %s has no valid pod selector", deployment.Name)
	}
	pods, err := e.Hub.Clients.KubeClient.Kube.CoreV1().Pods(deployment.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return fmt.Errorf("list hub proxy pods: %w", err)
	}
	probed := false
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		for _, container := range pod.Spec.Containers {
			if container.ReadinessProbe == nil || container.ReadinessProbe.HTTPGet == nil || container.ReadinessProbe.HTTPGet.Path != "/ready" {
				continue
			}
			if !requestEndpoint {
				podReady, containerReady := false, false
				for _, condition := range pod.Status.Conditions {
					if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
						podReady = true
					}
				}
				for _, status := range pod.Status.ContainerStatuses {
					if status.Name == container.Name && status.Ready {
						containerReady = true
					}
				}
				if !podReady || !containerReady {
					return fmt.Errorf("hub proxy pod %s is not ready", pod.Name)
				}
				probed = true
				continue
			}
			probe := container.ReadinessProbe.HTTPGet
			scheme := strings.ToLower(string(probe.Scheme))
			if scheme == "" {
				scheme = "http"
			}
			result := e.Hub.Clients.KubeClient.Kube.CoreV1().RESTClient().Get().Namespace(deployment.Namespace).
				Resource("pods").Name(scheme + ":" + pod.Name + ":" + probe.Port.String()).
				SubResource("proxy").Suffix("ready").Do(ctx)
			var status int
			if err := result.StatusCode(&status).Error(); err != nil {
				return fmt.Errorf("hub proxy %s /ready: %w", pod.Name, err)
			}
			if status != 200 {
				return fmt.Errorf("hub proxy %s /ready returned HTTP %d", pod.Name, status)
			}
			probed = true
		}
	}
	if !probed {
		return fmt.Errorf("hub proxy has no pod with an HTTP /ready probe")
	}
	return nil
}
