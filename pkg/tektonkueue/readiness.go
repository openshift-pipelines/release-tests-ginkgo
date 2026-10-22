package tektonkueue

import (
	"context"
	"fmt"
	"reflect"

	operatorv1alpha1 "github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type readinessResource struct {
	gvr       schema.GroupVersionResource
	name      string
	namespace string
	condition string
	operand   string
}

func operatorResource(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "operator.tekton.dev", Version: "v1alpha1", Resource: resource}
}

// AssertReady checks the current prerequisite and multicluster state without
// mutating it. Use WaitReady to await controller reconciliation. Setup must register
// real spoke workers first; deployment availability alone does not prove /ready.
func (e *Environment) AssertReady(ctx context.Context) error {
	if err := e.assertResourcesReady(ctx); err != nil {
		return err
	}
	// Only probe the hub after every configured worker is observed Active.
	return e.proxyReady(ctx, true)
}

func (e *Environment) readinessResources(cluster Cluster) []readinessResource {
	resources := []readinessResource{
		{gvr: kueueOperatorGVR, name: "cluster", condition: "Available"},
		{gvr: operatorResource("tektonconfigs"), name: "config", condition: "Ready"},
		{gvr: operatorResource("tektonschedulers"), name: operatorv1alpha1.TektonSchedulerResourceName, condition: "Ready", operand: operatorv1alpha1.TektonSchedulerResourceName},
		{gvr: clusterQueueGVR, name: e.Prefix, condition: "Active"},
		{gvr: localQueueGVR, name: e.Prefix, namespace: e.Namespace, condition: "Active"},
		{gvr: resourceFlavorGVR, name: e.Prefix},
	}
	if cluster.Name == e.Hub.Name {
		resources = append(resources,
			readinessResource{gvr: operatorResource("tektonmulticlusterproxyaaes"), name: operatorv1alpha1.MultiClusterProxyAAEResourceName, condition: "Ready", operand: operatorv1alpha1.MultiClusterProxyAAEResourceName},
			readinessResource{gvr: operatorResource("syncerservices"), name: operatorv1alpha1.SyncerServiceResourceName, condition: "Ready", operand: operatorv1alpha1.OperandSyncerService},
			readinessResource{gvr: admissionCheckGVR, name: e.Prefix, condition: "Active"},
			readinessResource{gvr: multiKueueConfigGVR, name: e.Prefix})
		for _, spoke := range e.Spokes {
			resources = append(resources, readinessResource{gvr: multiKueueClusterGVR, name: e.workerName(spoke), condition: "Active"})
		}
	}
	return resources
}

func (e *Environment) assertResourcesReady(ctx context.Context) error {
	if err := e.validate(); err != nil {
		return err
	}
	for _, cluster := range e.clusters() {
		var tektonConfig operatorv1alpha1.TektonConfig
		for _, resource := range e.readinessResources(cluster) {
			object, err := cluster.Clients.Dynamic.Resource(resource.gvr).Namespace(resource.namespace).
				Get(ctx, resource.name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("%s %s/%s: %w", cluster.Name, resource.gvr.Resource, resource.name, err)
			}
			if err := resourceReady(object, resource); err != nil {
				return fmt.Errorf("%s %s/%s: %w", cluster.Name, resource.gvr.Resource, resource.name, err)
			}
			switch resource.gvr {
			case operatorResource("tektonconfigs"):
				role := operatorv1alpha1.MultiClusterRoleSpoke
				if cluster.Name == e.Hub.Name {
					role = operatorv1alpha1.MultiClusterRoleHub
				}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &tektonConfig); err != nil {
					return fmt.Errorf("%s TektonConfig: %w", cluster.Name, err)
				}
				scheduler := tektonConfig.Spec.Scheduler
				if scheduler.IsDisabled() || scheduler.MultiClusterDisabled || scheduler.MultiClusterRole != role || scheduler.QueueName != e.Prefix {
					return fmt.Errorf("%s TektonConfig scheduler is not configured for %s and queue %s", cluster.Name, role, e.Prefix)
				}
			case operatorResource("tektonschedulers"):
				var scheduler operatorv1alpha1.TektonScheduler
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &scheduler); err != nil {
					return fmt.Errorf("%s TektonScheduler: %w", cluster.Name, err)
				}
				if !reflect.DeepEqual(scheduler.Spec.Scheduler, tektonConfig.Spec.Scheduler) {
					return fmt.Errorf("%s TektonScheduler spec has not converged to TektonConfig", cluster.Name)
				}
			}
			if resource.operand != "" {
				namespace, _, err := unstructured.NestedString(object.Object, "spec", "targetNamespace")
				if err != nil || namespace != tektonConfig.Spec.TargetNamespace {
					return fmt.Errorf("%s %s targetNamespace has not converged to TektonConfig", cluster.Name, resource.operand)
				}
				if err := componentDeploymentsReady(ctx, cluster, object); err != nil {
					return fmt.Errorf("%s %s: %w", cluster.Name, resource.operand, err)
				}
			}
		}
	}
	return nil
}

func resourceReady(object *unstructured.Unstructured, resource readinessResource) error {
	if object.GetDeletionTimestamp() != nil {
		return fmt.Errorf("resource is terminating")
	}
	if resource.condition == "" {
		return nil
	}
	conditions, _, err := unstructured.NestedSlice(object.Object, "status", "conditions")
	if err != nil {
		return err
	}
	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("malformed status condition")
		}
		if condition["type"] == "Degraded" && condition["status"] == "True" {
			return fmt.Errorf("degraded: %v: %v", condition["reason"], condition["message"])
		}
	}
	for _, item := range conditions {
		condition := item.(map[string]any)
		if condition["type"] != resource.condition {
			continue
		}
		if condition["status"] != "True" {
			return fmt.Errorf("%s=%v: %v: %v", resource.condition, condition["status"], condition["reason"], condition["message"])
		}
		var observed int64
		var found bool
		switch resource.gvr {
		case kueueOperatorGVR, operatorResource("syncerservices"):
			observed, found, err = unstructured.NestedInt64(object.Object, "status", "observedGeneration")
		case clusterQueueGVR, localQueueGVR, admissionCheckGVR, multiKueueClusterGVR:
			observed, found, err = unstructured.NestedInt64(condition, "observedGeneration")
		default:
			// v0.80 TektonConfig, Scheduler and proxy controllers publish no
			// generation observation. AssertReady instead checks spec propagation,
			// observed Deployment rollouts and the proxy's live /ready endpoint.
			return nil
		}
		if err != nil {
			return err
		}
		if !found || observed < object.GetGeneration() {
			return fmt.Errorf("stale %s condition for generation %d", resource.condition, object.GetGeneration())
		}
		return nil
	}
	return fmt.Errorf("%s condition not reported", resource.condition)
}
