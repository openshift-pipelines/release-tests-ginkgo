package tektonkueue

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	packeys "github.com/openshift-pipelines/pipelines-as-code/pkg/apis/pipelinesascode/keys"
	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

var secretGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

type pacSecret struct {
	secret            *corev1.Secret
	deleted, conflict bool
}
type pacExecution struct {
	correlation, queue, namespace string
	expected                      *PACIdentity
	baseline                      map[string]map[string]bool
	secrets                       map[string]map[string]*pacSecret
	name, selected, runName, hub  string
	hubUID, workerUID             types.UID
	reference                     *corev1.Secret
	allowDeletion                 bool
	cleaning, completed           bool
}

func (s *ArmedExecution) armSecrets() error {
	for _, cluster := range s.environment.clusters() {
		sub := &executionSubscription{readinessSubscription: readinessSubscription{
			client: cluster.Clients.Dynamic.Resource(secretGVR).Namespace(s.environment.Namespace),
			name:   cluster.Name + "/secrets",
		}, cluster: cluster, gvr: secretGVR}
		if err := sub.subscribe(s.ctx); err != nil {
			return requestError(s.ctx, "arm Secret baseline and history")
		}
		s.watches.subscriptions = append(s.watches.subscriptions, sub)
		baseline := map[string]bool{}
		for _, secret := range sub.snapshot.Items {
			baseline[secret.GetName()] = true
		}
		s.pac.baseline[cluster.Name] = baseline
		s.pac.secrets[cluster.Name] = map[string]*pacSecret{}
		sub.snapshot = nil // baselines retain names only, never preexisting credentials
	}
	return nil
}

func (p *pacExecution) checkRunHistory(w *executionWatches) error {
	if p.expected == nil {
		return nil
	}
	for _, event := range w.events {
		if event.cluster != w.hub || event.gvr != pipelineRunGVR || event.object.GetName() != w.runName {
			continue
		}
		var run pipelinev1.PipelineRun
		if runtime.DefaultUnstructuredConverter.FromUnstructured(event.object.Object, &run) != nil {
			return fmt.Errorf("invalid PAC run history")
		}
		if err := p.matches(&run, p.queue); err != nil {
			return err
		}
		if p.name != "" && run.Annotations[packeys.GitAuthSecret] != p.name {
			return fmt.Errorf("PAC Secret annotation changed")
		}
	}
	return nil
}

func (s *ArmedExecution) pacCleanup(ctx context.Context, includeHub bool) (bool, error) {
	p := s.pac
	// Invalid owners cannot provide normal Secret GC. For a failed observation,
	// the parent's UID-guarded test Namespace cleanup removes these Secrets after
	// run/worker deletion. Successful observation still requires product copy GC.
	if p.cleaning && !p.completed {
		return true, nil
	}
	if p.name == "" {
		if s.root == nil {
			return true, nil
		}
		p.name = s.root.Annotations[packeys.GitAuthSecret]
		if p.name == "" {
			return true, nil
		}
	}
	for _, cluster := range s.environment.clusters() {
		if cluster.Name == s.environment.Hub.Name && !includeHub {
			continue
		}
		if p.baseline[cluster.Name][p.name] {
			return false, fmt.Errorf("generated Secret %s existed before trigger", p.name)
		}
		if current := p.secrets[cluster.Name][p.name]; current != nil && !current.deleted {
			return false, nil
		}
		_, err := cluster.Clients.KubeClient.Kube.CoreV1().Secrets(s.environment.Namespace).Get(ctx, p.name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return false, requestError(ctx, "verify generated Secret cleanup")
		}
		return false, nil
	}
	return true, nil
}

func (p *pacExecution) record(cluster string, kind watch.EventType, object *unstructured.Unstructured) error {
	name := object.GetName()
	if p.baseline[cluster][name] {
		return nil
	}
	current := p.secrets[cluster][name]
	var secret corev1.Secret
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &secret); err != nil {
		return fmt.Errorf("invalid Secret event")
	}
	if secret.Namespace != p.namespace || secret.Name == "" {
		return fmt.Errorf("invalid Secret event identity")
	}
	if current == nil {
		if len(p.secrets[cluster]) >= 256 {
			return fmt.Errorf("PAC Secret history exceeded capacity")
		}
		current = &pacSecret{}
		p.secrets[cluster][name] = current
	}
	if secret.UID == "" || current.secret != nil && (current.secret.UID != secret.UID || current.deleted) {
		current.conflict = true
	}
	if current.secret != nil && !current.conflict {
		if err := comparePACSecrets(current.secret, &secret); err != nil {
			current.conflict = true
			return err
		}
	}
	current.secret = secret.DeepCopy()
	current.deleted = kind == watch.Deleted
	if p.name == name && p.reference != nil {
		return p.checkRecorded(cluster, current)
	}
	return nil
}

func (p *pacExecution) checkRecorded(cluster string, current *pacSecret) error {
	if cluster != p.hub && cluster != p.selected {
		return fmt.Errorf("generated Secret %s appeared on the wrong spoke", p.name)
	}
	if current.conflict {
		return fmt.Errorf("generated Secret %s was replaced", p.name)
	}
	if current.deleted {
		if !p.allowDeletion {
			return fmt.Errorf("generated Secret %s disappeared before gate 2", p.name)
		}
		return nil
	}
	uid := p.hubUID
	if cluster == p.selected {
		uid = p.workerUID
	}
	if !typedOwner(current.secret.OwnerReferences, "PipelineRun", p.runName, uid) {
		return fmt.Errorf("generated Secret %s has the wrong owner UID", p.name)
	}
	return comparePACSecrets(p.reference, current.secret)
}

func (s *ArmedExecution) pacReady(run *pipelinev1.PipelineRun, spoke Cluster) (bool, error) {
	p, w := s.pac, s.watches
	if err := p.matches(run, s.environment.Prefix); err != nil {
		return false, err
	}
	hub, worker := w.retained[s.environment.Hub.Name], w.retained[spoke.Name]
	admitted, _ := conditionTrue(hub.workload.object, "Admitted")
	queue, _, _ := unstructured.NestedString(hub.workload.object.Object, "status", "admission", "clusterQueue")
	if !admitted || queue != s.environment.Prefix {
		return false, nil
	}
	name := run.Annotations[packeys.GitAuthSecret]
	if worker.run.object.GetAnnotations()[packeys.GitAuthSecret] != name {
		return false, fmt.Errorf("spoke PAC run lacks matching Secret annotation")
	}
	p.name, p.selected, p.runName, p.hub = name, spoke.Name, run.Name, s.environment.Hub.Name
	p.hubUID, p.workerUID = run.UID, worker.run.object.GetUID()
	for _, cluster := range s.environment.clusters() {
		if p.baseline[cluster.Name][name] {
			return false, fmt.Errorf("generated Secret %s existed in the baseline", name)
		}
		if cluster.Name != p.hub && cluster.Name != p.selected && p.secrets[cluster.Name][name] != nil {
			return false, fmt.Errorf("generated Secret %s appeared on the wrong spoke", name)
		}
	}
	source, copy := p.secrets[p.hub][name], p.secrets[p.selected][name]
	if source == nil || copy == nil {
		return false, nil
	}
	if source.conflict || copy.conflict || source.deleted || copy.deleted {
		return false, fmt.Errorf("generated Secret %s disappeared or was replaced", name)
	}
	if len(source.secret.OwnerReferences) == 0 {
		return false, nil
	}
	if !typedOwner(source.secret.OwnerReferences, "PipelineRun", run.Name, run.UID) || !typedOwner(copy.secret.OwnerReferences, "PipelineRun", run.Name, p.workerUID) {
		return false, fmt.Errorf("generated Secret %s has the wrong owner UID", name)
	}
	if err := comparePACSecrets(source.secret, copy.secret); err != nil {
		return false, err
	}
	if p.reference == nil {
		p.reference = source.secret.DeepCopy()
	}
	return true, nil
}

func comparePACSecrets(source, copy *corev1.Secret) error {
	name := source.Name
	if source.Type != copy.Type {
		return fmt.Errorf("secret %s type mismatch", name)
	}
	for _, key := range []string{".gitconfig", ".git-credentials", "git-provider-token"} {
		if len(source.Data[key]) == 0 {
			return fmt.Errorf("secret %s has an empty required key %s", name, key)
		}
	}
	keys := make([]string, 0, len(source.Data)+len(copy.Data))
	for key := range source.Data {
		keys = append(keys, key)
	}
	for key := range copy.Data {
		if _, found := source.Data[key]; !found {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		a, sourceHas := source.Data[key]
		b, copyHas := copy.Data[key]
		if !sourceHas || !copyHas {
			return fmt.Errorf("secret %s key-set mismatch at %s", name, key)
		}
		if !bytes.Equal(a, b) {
			return fmt.Errorf("secret %s payload mismatch at key %s", name, key)
		}
	}
	return nil
}
