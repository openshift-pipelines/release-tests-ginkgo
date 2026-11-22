package kindmulticluster

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var clusterQueues = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta2", Resource: "clusterqueues"}

type queueControl struct {
	client   dynamic.ResourceInterface
	original *unstructured.Unstructured
}

func readScaleQueue(ctx context.Context, client dynamic.Interface) (*queueControl, error) {
	queues := client.Resource(clusterQueues)
	queue, err := queues.Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read ClusterQueue quota: %w", err)
	}
	if err := quotaTen(queue); err != nil {
		return nil, err
	}
	policy, _, err := unstructured.NestedString(queue.Object, "spec", "stopPolicy")
	if err != nil {
		return nil, err
	}
	if policy != "" && policy != "None" {
		return nil, fmt.Errorf("scale fixture queue already stopped with %q", policy)
	}
	return &queueControl{client: queues, original: queue}, nil
}

func quotaTen(queue *unstructured.Unstructured) error {
	groups, _, err := unstructured.NestedSlice(queue.Object, "spec", "resourceGroups")
	if err != nil {
		return err
	}
	matches := 0
	for _, raw := range groups {
		group, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid ClusterQueue resource group")
		}
		flavors, _, err := unstructured.NestedSlice(group, "flavors")
		if err != nil {
			return err
		}
		for _, raw := range flavors {
			flavor, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid ClusterQueue flavor")
			}
			resources, _, err := unstructured.NestedSlice(flavor, "resources")
			if err != nil {
				return err
			}
			for _, raw := range resources {
				entry, ok := raw.(map[string]any)
				if !ok {
					return fmt.Errorf("invalid ClusterQueue resource")
				}
				if entry["name"] != "tekton.dev/pipelineruns" {
					continue
				}
				quantity, err := resource.ParseQuantity(fmt.Sprint(entry["nominalQuota"]))
				if err != nil {
					return fmt.Errorf("parse PipelineRun nominal quota: %w", err)
				}
				if quantity.Cmp(resource.MustParse("10")) != 0 {
					return fmt.Errorf("PipelineRun nominal quota is %s, want 10", quantity.String())
				}
				matches++
			}
		}
	}
	if matches != 1 {
		return fmt.Errorf("want one PipelineRun quota entry, found %d", matches)
	}
	return nil
}

// set changes supported admission eligibility, never Workload status. The
// controller's observed generation is the acknowledgement, not a timing delay.
func (q *queueControl) set(ctx context.Context, policy string) error {
	current, err := q.client.Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if current.GetUID() != q.original.GetUID() {
		return fmt.Errorf("refusing to modify a replacement ClusterQueue")
	}
	if err := unstructured.SetNestedField(current.Object, policy, "spec", "stopPolicy"); err != nil {
		return err
	}
	updated, err := q.client.Update(ctx, current, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("set queue admission to %s: %w", policy, err)
	}
	_, err = observe(ctx, q.client, func(object *unstructured.Unstructured) (bool, error) {
		if object.GetName() != namespace {
			return false, nil
		}
		if object.GetUID() != q.original.GetUID() {
			return false, fmt.Errorf("ClusterQueue replaced while awaiting admission acknowledgement")
		}
		return queueAcknowledged(object, updated.GetGeneration(), policy)
	})
	return err
}

func queueAcknowledged(queue *unstructured.Unstructured, generation int64, policy string) (bool, error) {
	conditions, _, err := unstructured.NestedSlice(queue.Object, "status", "conditions")
	if err != nil {
		return false, err
	}
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			return false, fmt.Errorf("invalid ClusterQueue condition")
		}
		if condition["type"] != "Active" {
			continue
		}
		observed, _, err := unstructured.NestedInt64(condition, "observedGeneration")
		if err != nil {
			return false, err
		}
		if observed < generation {
			return false, nil
		}
		switch policy {
		case "None":
			return condition["status"] == "True", nil
		case "Hold":
			return condition["status"] == "False" && condition["reason"] == "Stopped", nil
		default:
			return false, fmt.Errorf("unsupported queue admission policy %q", policy)
		}
	}
	return false, nil
}
