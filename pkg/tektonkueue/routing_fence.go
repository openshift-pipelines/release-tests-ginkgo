package tektonkueue

import (
	"context"
	"fmt"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (e *Environment) routingMarker(gvr schema.GroupVersionResource) (*unstructured.Unstructured, error) {
	name := e.Prefix + "-observation-fence"
	var obj runtime.Object
	switch gvr.Resource {
	case "pipelineruns":
		run, _ := e.newPipelineRun("observation-fence", false)
		// Neither Tekton nor MultiKueue owns this inert observation object.
		run.Spec.ManagedBy = ptr("release-tests.tekton.dev/observer")
		run.Spec.Status = pipelinev1.PipelineRunSpecStatusPending
		run.Spec.PipelineSpec.Tasks[0].TaskSpec.Steps[0].Script = "#!/bin/sh\necho observation-fence\n"
		run.Labels[queueLabel] = ""
		obj = run
	case "taskruns":
		obj = &pipelinev1.TaskRun{
			TypeMeta:   metav1.TypeMeta{APIVersion: "tekton.dev/v1", Kind: "TaskRun"},
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: e.Namespace, Labels: e.ownedLabels()},
			Spec: pipelinev1.TaskRunSpec{Status: pipelinev1.TaskRunSpecStatusPending,
				TaskSpec: &pipelinev1.TaskSpec{Steps: []pipelinev1.Step{{Name: "inert", Image: workloadImage}}}},
		}
	case "workloads":
		return object("Workload", name, e.Namespace, map[string]any{
			"active": false, "queueName": name,
			"podSets": []any{map[string]any{"name": "main", "count": int64(1),
				"template": map[string]any{"spec": map[string]any{
					"containers": []any{map[string]any{"name": "inert", "image": workloadImage}},
				}},
			}},
		}), nil
	default:
		return nil, fmt.Errorf("unsupported observation resource %s", gvr.Resource)
	}
	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	value["apiVersion"] = "tekton.dev/v1"
	value["kind"] = "PipelineRun"
	if gvr.Resource == "taskruns" {
		value["kind"] = "TaskRun"
	}
	return &unstructured.Unstructured{Object: value}, nil
}

// Namespace disappearance prevents further execution writes, including updates.
// Only then do surviving, collection-wide fences prove delivery of all preceding
// events. Namespace-local marker deletion alone is not a final boundary.
func (o *routingObserver) finish(ctx context.Context) error {
	if err := o.env.quiesceRoutingNamespaces(ctx); err != nil {
		return err
	}
	for _, stream := range o.streams {
		resource := stream.resource
		created, err := resource.Create(ctx, stream.final.DeepCopy(), metav1.CreateOptions{})
		if err != nil {
			return err
		}
		o.mu.Lock()
		stream.final = created
		o.mu.Unlock()
		uid := created.GetUID()
		if err := resource.Delete(ctx, created.GetName(), metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: &uid},
		}); err != nil {
			return err
		}
	}
	if err := o.wait(ctx, func() bool {
		for _, stream := range o.streams {
			if !stream.fenced {
				return false
			}
		}
		return true
	}); err != nil {
		return err
	}
	o.stop()
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, stream := range o.streams {
		if len(stream.live) != 0 {
			return fmt.Errorf("routing objects remain after the final observation fence")
		}
	}
	return o.err
}

func (e *Environment) deleteRoutingRun(ctx context.Context, run *pipelinev1.PipelineRun) error {
	current, err := e.Hub.Clients.PipelineRunClient.Get(ctx, run.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !e.owns(current.Labels) || run.UID != "" && current.UID != run.UID {
		return fmt.Errorf("refusing to delete replacement routing PipelineRun %s", run.Name)
	}
	foreground := metav1.DeletePropagationForeground
	return ignoreNotFound(e.Hub.Clients.PipelineRunClient.Delete(ctx, run.Name, metav1.DeleteOptions{
		PropagationPolicy: &foreground, Preconditions: &metav1.Preconditions{UID: &current.UID},
	}))
}
