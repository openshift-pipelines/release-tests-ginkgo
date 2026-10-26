package tektonkueue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type proxyRun struct {
	namespace string
	name      string
	worker    string
	pod       string
	workload  string
	task      string
	runUID    types.UID
	taskUID   types.UID
	podUID    types.UID
}

func (r proxyRun) path(suffix string) string {
	return "/api/v1/namespaces/" + url.PathEscape(r.namespace) + "/pipelineruns/" + url.PathEscape(r.name) + "/" + suffix
}

func (r proxyRun) logPath(pod string) string {
	query := url.Values{
		"pipelineRun": {r.name}, "pod": {pod}, "container": {workloadLogContainer},
	}
	return "/api/v1/namespaces/" + url.PathEscape(r.namespace) + "/logs/stream?" + query.Encode()
}

func (p *consoleProxy) get(ctx context.Context, path string, into any) (worker string, resultErr error) {
	return p.json(ctx, p.base+path, into)
}

func (p *consoleProxy) json(ctx context.Context, endpoint string, into any) (worker string, resultErr error) {
	ctx, cancel := context.WithDeadline(ctx, p.expires)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", requestError(ctx, "create console request")
	}
	request.Header.Set("Accept", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		return "", requestError(ctx, "console request")
	}
	defer func() {
		if err := response.Body.Close(); err != nil && resultErr == nil {
			resultErr = requestError(ctx, "close console response")
		}
	}()
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || err != nil || mediaType != "application/json" {
		return "", fmt.Errorf("console response rejected (HTTP %d)", response.StatusCode)
	}
	const limit = 4 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return "", requestError(ctx, "read console JSON")
	}
	if len(data) > limit {
		return "", fmt.Errorf("console JSON exceeds size limit")
	}
	if err := json.Unmarshal(data, into); err != nil {
		return "", fmt.Errorf("invalid console JSON")
	}
	return response.Header.Get("X-Worker-Cluster"), nil
}

func requestError(ctx context.Context, operation string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return fmt.Errorf("%s: %w", operation, context.DeadlineExceeded)
	}
	return fmt.Errorf("%s failed", operation)
}

func (p *consoleProxy) pod(ctx context.Context, run proxyRun) (*corev1.Pod, error) {
	var resolved struct {
		Name         string `json:"name"`
		State        string `json:"state"`
		WorkloadName string `json:"workloadName"`
	}
	worker, err := p.get(ctx, run.path("resolve"), &resolved)
	if err != nil {
		return nil, err
	}
	if worker != run.worker || resolved.Name != run.worker || resolved.State != "Admitted" || resolved.WorkloadName == "" || resolved.WorkloadName != run.workload {
		return nil, fmt.Errorf("console proxy resolved the wrong worker or an unadmitted PipelineRun")
	}
	var tasks pipelinev1.TaskRunList
	worker, err = p.get(ctx, run.path("taskruns"), &tasks)
	if err != nil {
		return nil, err
	}
	if worker != run.worker || len(tasks.Items) != 1 || tasks.Items[0].Labels["tekton.dev/pipelineRun"] != run.name || tasks.Items[0].Namespace != run.namespace ||
		tasks.Items[0].Name != run.task || tasks.Items[0].UID != run.taskUID ||
		!typedOwner(tasks.Items[0].OwnerReferences, "PipelineRun", run.name, run.runUID) {
		return nil, fmt.Errorf("console proxy returned unrelated TaskRuns or the wrong worker")
	}
	var pods corev1.PodList
	worker, err = p.get(ctx, run.path("pods"), &pods)
	if err != nil {
		return nil, err
	}
	if worker != run.worker || len(pods.Items) != 1 {
		return nil, fmt.Errorf("console proxy returned the wrong worker or Pod count")
	}
	pod := &pods.Items[0]
	if pod.Name == "" || pod.Namespace != run.namespace || pod.Labels["tekton.dev/pipelineRun"] != run.name ||
		pod.Name != tasks.Items[0].Status.PodName || pod.Name != run.pod || pod.UID != run.podUID ||
		!typedOwner(pod.OwnerReferences, "TaskRun", run.task, run.taskUID) {
		return nil, fmt.Errorf("console proxy returned an unrelated Pod")
	}
	running := false
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == workloadLogContainer && status.State.Running != nil {
			running = true
		}
	}
	if !running {
		return nil, fmt.Errorf("console proxy did not return a running log producer")
	}
	return pod, nil
}

func typedOwner(owners []metav1.OwnerReference, kind, name string, uid types.UID) bool {
	for _, owner := range owners {
		if owner.Kind == kind && owner.Name == name && owner.UID == uid {
			return true
		}
	}
	return false
}
