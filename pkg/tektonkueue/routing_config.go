package tektonkueue

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	schedulerconfig "github.com/konflux-ci/tekton-kueue/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
	"sigs.k8s.io/yaml"
)

const routingConfigMap = "tekton-kueue-config"

// SetupRouting extends the workload environment with CEL routing and a hub-only queue.
// The existing workload setup and scenario are deliberately kept separate.
func (e *Environment) SetupRouting(ctx context.Context) error {
	e.routingWaits = true
	if err := e.Setup(ctx); err != nil {
		return err
	}
	local := e.Prefix + "-local"
	if err := e.createDynamic(ctx, e.Hub, clusterQueueGVR, object("ClusterQueue", local, "", map[string]any{
		"namespaceSelector": map[string]any{},
		"resourceGroups": []any{map[string]any{
			"coveredResources": []any{"tekton.dev/pipelineruns"},
			"flavors": []any{map[string]any{
				"name": e.Prefix,
				"resources": []any{map[string]any{
					"name": "tekton.dev/pipelineruns", "nominalQuota": int64(1),
				}},
			}},
		}},
	})); err != nil {
		return err
	}
	if err := e.createDynamic(ctx, e.Hub, localQueueGVR, object("LocalQueue", local, e.Namespace,
		map[string]any{"clusterQueue": local})); err != nil {
		return err
	}
	for _, cluster := range e.clusters() {
		owner, err := newRoutingConfigOwner(ctx, cluster)
		if err != nil {
			return err
		}
		applied := owner.original.DeepCopy()
		applied.Config = e.routingConfig(cluster.Name == e.Hub.Name)
		data, err := json.Marshal(applied.Config)
		if err != nil {
			return err
		}
		// Operator v0.80 derives the hub override from its role, ignoring the
		// config boolean. Additional options run after that transformation.
		if applied.Options.ConfigMaps == nil {
			applied.Options.ConfigMaps = map[string]corev1.ConfigMap{}
		}
		override := applied.Options.ConfigMaps[routingConfigMap]
		override = *override.DeepCopy()
		if override.Data == nil {
			override.Data = map[string]string{}
		}
		override.Data["config.yaml"] = string(data)
		applied.Options.ConfigMaps[routingConfigMap] = override
		e.addCleanup(func(cleanupCtx context.Context) error {
			return owner.restart(cleanupCtx, owner.original)
		})
		if err := owner.restart(ctx, *applied); err != nil {
			return fmt.Errorf("configure CEL routing on %s: %w", cluster.Name, err)
		}
		if err := waitForRoutingConfigMap(ctx, cluster, applied.Config); err != nil {
			return err
		}
	}
	if err := routingCondition(ctx, e.Hub.Clients.Dynamic.Resource(clusterQueueGVR), resourceCondition{local, "Active"}); err != nil {
		return err
	}
	return routingCondition(ctx, e.Hub.Clients.Dynamic.Resource(localQueueGVR).Namespace(e.Namespace), resourceCondition{local, "Active"})
}

func (e *Environment) routingConfig(hub bool) schedulerconfig.Config {
	cfg := schedulerconfig.Config{QueueName: e.Prefix, MultiKueueOverride: false}
	if hub {
		cfg.CEL.Expressions = []string{fmt.Sprintf(
			`plrNamespace == %q && pipelineRun.metadata.name != %q ? (has(pipelineRun.metadata.labels) && "release-tests.tekton.dev/route" in pipelineRun.metadata.labels ? (pipelineRun.metadata.labels["release-tests.tekton.dev/route"] == "remote" ? [multiKueue()] : (pipelineRun.metadata.labels["release-tests.tekton.dev/route"] == "explicit" ? [multiKueue(%q)] : [label(%q, %q)])) : [label(%q, %q)]) : []`,
			e.Namespace, e.Prefix+"-observation-fence", e.explicitQueueName(), queueLabel, e.Prefix+"-local", queueLabel, e.Prefix+"-local")}
	}
	return cfg
}

func waitForRoutingConfigMap(ctx context.Context, cluster Cluster, expected schedulerconfig.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	tc, err := cluster.Clients.TektonConfig().Get(ctx, "config", metav1.GetOptions{})
	if err != nil {
		return err
	}
	maps := cluster.Clients.KubeClient.Kube.CoreV1().ConfigMaps(tc.Spec.TargetNamespace)
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = "metadata.name=" + routingConfigMap
			return maps.List(ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = "metadata.name=" + routingConfigMap
			return maps.Watch(ctx, options)
		},
	}
	_, err = watchtools.UntilWithSync(ctx, lw, &corev1.ConfigMap{}, nil, func(event watch.Event) (bool, error) {
		configMap, ok := event.Object.(*corev1.ConfigMap)
		if !ok || event.Type == watch.Deleted {
			return false, nil
		}
		var actual schedulerconfig.Config
		if err := yaml.UnmarshalStrict([]byte(configMap.Data["config.yaml"]), &actual); err != nil {
			return false, err
		}
		return reflect.DeepEqual(actual, expected), nil
	})
	if err != nil {
		return fmt.Errorf("runtime scheduler config on %s did not match CEL config with override disabled: %w", cluster.Name, err)
	}
	return nil
}
