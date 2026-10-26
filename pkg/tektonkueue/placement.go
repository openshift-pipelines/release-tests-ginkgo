package tektonkueue

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

type executionIdentity struct {
	object  *unstructured.Unstructured
	deleted bool
}

type clusterExecution struct {
	run      *executionIdentity
	task     *executionIdentity
	pod      *executionIdentity
	workload *executionIdentity
}

func (c *clusterExecution) member(gvr schema.GroupVersionResource) **executionIdentity {
	switch gvr {
	case pipelineRunGVR:
		return &c.run
	case taskRunGVR:
		return &c.task
	case podGVR:
		return &c.pod
	default:
		return &c.workload
	}
}

func relatedRun(object *unstructured.Unstructured, name string) bool {
	if object.GetLabels()["tekton.dev/pipelineRun"] == name {
		return true
	}
	for _, owner := range object.GetOwnerReferences() {
		if owner.Kind == "PipelineRun" && owner.Name == name {
			return true
		}
	}
	return false
}

func ownedBy(object *unstructured.Unstructured, kind, name string, uid types.UID) bool {
	for _, owner := range object.GetOwnerReferences() {
		if owner.Kind == kind && owner.Name == name && owner.UID == uid {
			return true
		}
	}
	return false
}

func (w *executionWatches) validateHistory() error {
	if w.discontinuity != nil {
		return w.discontinuity
	}
	if w.runName == "" {
		return nil
	}
	// Resolve parent relationships across resource streams, including Pods that
	// arrive before their TaskRun and pre-bind objects already created/deleted.
	tasks := map[string]bool{}
	workloads := map[string]bool{}
	for _, event := range w.events {
		if event.gvr == taskRunGVR && relatedRun(event.object, w.runName) {
			tasks[event.cluster+"/"+event.object.GetName()] = true
		}
		if event.cluster == w.hub && event.gvr == workloadGVR && relatedRun(event.object, w.runName) {
			workloads[event.object.GetName()] = true
		}
	}
	w.retained, w.seen, w.success, w.failed = map[string]*clusterExecution{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	classified := map[string]bool{}
	for _, event := range w.events {
		object := event.object
		key := event.cluster + "/" + event.gvr.Resource + "/" + object.GetName()
		relevant := relatedRun(object, w.runName) || (event.gvr == pipelineRunGVR && object.GetName() == w.runName)
		// Candidate Workloads are copied without owner refs to eligible workers.
		// They require UID-pinned cleanup but do not prove execution placement.
		relevant = relevant || (event.gvr == workloadGVR && workloads[object.GetName()])
		if event.gvr == podGVR {
			for _, owner := range object.GetOwnerReferences() {
				relevant = relevant || (owner.Kind == "TaskRun" && tasks[event.cluster+"/"+owner.Name])
			}
		}
		classified[key] = classified[key] || relevant
	}
	// ponytail: bounded replay (4096 events), incremental indices if larger fixtures are needed.
	for _, event := range w.events {
		object := event.object
		if !classified[event.cluster+"/"+event.gvr.Resource+"/"+object.GetName()] {
			continue
		}
		if object.GetUID() == "" || object.GetNamespace() != w.namespace {
			return fmt.Errorf("execution resource is missing its namespace or UID")
		}
		if event.cluster == w.hub && (event.gvr == taskRunGVR || event.gvr == podGVR) {
			return fmt.Errorf("related TaskRun or Pod executed on the hub")
		}
		if event.cluster != w.hub && event.gvr != workloadGVR {
			w.seen[event.cluster] = true
			if len(w.seen) > 1 {
				return fmt.Errorf("related execution resources appeared on multiple spokes")
			}
		}
		c := w.retained[event.cluster]
		if c == nil {
			c = &clusterExecution{}
			w.retained[event.cluster] = c
		}
		member := c.member(event.gvr)
		if *member != nil {
			previous := *member
			if previous.object.GetName() != object.GetName() || previous.object.GetUID() != object.GetUID() {
				return fmt.Errorf("duplicate or replaced execution %s identity", event.gvr.Resource)
			}
			if previous.deleted {
				return fmt.Errorf("execution resource reappeared after deletion")
			}
		}
		*member = &executionIdentity{object: object, deleted: event.kind == watch.Deleted}
		if event.gvr == pipelineRunGVR {
			if object.GetName() != w.runName || (event.cluster == w.hub && object.GetUID() != w.hubUID) {
				return fmt.Errorf("hub or spoke PipelineRun identity changed")
			}
			conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
			for _, value := range conditions {
				condition, ok := value.(map[string]any)
				if !ok || condition["type"] != "Succeeded" {
					continue
				}
				w.failed[event.cluster] = w.failed[event.cluster] || condition["status"] == "False"
				w.success[event.cluster] = w.success[event.cluster] || condition["status"] == "True"
			}
		}
	}
	// Check every historic owner/reference, not just the last resource snapshot.
	for _, event := range w.events {
		key := event.cluster + "/" + event.gvr.Resource + "/" + event.object.GetName()
		if !classified[key] {
			continue
		}
		c := w.retained[event.cluster]
		switch event.gvr {
		case taskRunGVR, workloadGVR:
			if (event.gvr != workloadGVR || event.cluster == w.hub) && c.run != nil && !ownedBy(event.object, "PipelineRun", w.runName, c.run.object.GetUID()) {
				return fmt.Errorf("execution owner does not match the retained PipelineRun UID")
			}
			if event.gvr == taskRunGVR && c.pod != nil {
				pod, _, _ := unstructured.NestedString(event.object.Object, "status", "podName")
				if pod != "" && pod != c.pod.object.GetName() {
					return fmt.Errorf("TaskRun Pod identity changed")
				}
			}
		case podGVR:
			if c.task != nil && !ownedBy(event.object, "TaskRun", c.task.object.GetName(), c.task.object.GetUID()) {
				return fmt.Errorf("pod owner does not match the retained TaskRun UID")
			}
		}
	}
	return nil
}

func (w *executionWatches) completePlacement(spoke string) error {
	hub, worker := w.retained[w.hub], w.retained[spoke]
	if hub == nil || hub.run == nil || hub.workload == nil || worker == nil ||
		worker.run == nil || worker.task == nil || worker.pod == nil {
		return fmt.Errorf("execution placement identity set is incomplete")
	}
	for _, identity := range []*executionIdentity{hub.run, hub.workload, worker.run, worker.task, worker.pod} {
		if identity.deleted {
			return fmt.Errorf("execution identity disappeared before producer completion")
		}
	}
	return nil
}

func (w *executionWatches) gone(cluster string) bool {
	c := w.retained[cluster]
	if c == nil {
		return true
	}
	for _, identity := range []*executionIdentity{c.run, c.task, c.pod, c.workload} {
		if identity != nil && !identity.deleted {
			return false
		}
	}
	return true
}
