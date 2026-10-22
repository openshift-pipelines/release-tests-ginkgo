package kindmulticluster

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	pipelineclient "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

type scaleCluster struct {
	name    string
	path    string
	clients *clients.Clients
}

// A batch owns only successful creates, with a different UID and observer for
// every run. Failed creates never turn a pre-existing name into owned state.
type runBatch struct {
	hub     pipelineclient.PipelineRunInterface
	workers []pipelineclient.PipelineRunInterface
	runs    []*scaleRun
	prefix  string
}

func newRunBatch(hub pipelineclient.PipelineRunInterface) *runBatch {
	return &runBatch{hub: hub, prefix: "scale-" + strings.ToLower(rand.Text()[:12])}
}

func (b *runBatch) create(ctx context.Context, start func(*scaleRun) error) (*scaleRun, error) {
	run := newScaleRun(fmt.Sprintf("%s-%02d", b.prefix, len(b.runs)+1))
	if err := start(run); err != nil {
		run.close()
		return nil, err
	}
	request := pipelineRun(run.name)
	request.Labels["release-tests.tekton.dev/batch"] = b.prefix
	created, err := b.hub.Create(ctx, request, metav1.CreateOptions{})
	if err != nil {
		run.close()
		return nil, fmt.Errorf("create hub batch run %s: %w", run.name, err)
	}
	// Register ownership before inspecting the response or awaiting any event.
	run.uid = created.UID
	b.runs = append(b.runs, run)
	if run.uid == "" {
		return nil, fmt.Errorf("created PipelineRun %s has no UID", run.name)
	}
	return run, nil
}

func (b *runBatch) cleanup(ctx context.Context) error {
	var errs []error
	for _, run := range b.runs {
		run.close()
		if run.verified {
			if _, complete, err := run.progress(runFinished); err != nil {
				errs = append(errs, err)
			} else if !complete {
				errs = append(errs, fmt.Errorf("%s lost its completed execution proof", run.name))
			}
		}
		if run.uid == "" {
			errs = append(errs, fmt.Errorf("refusing to delete %s without its created UID", run.name))
			continue
		}
		err := deleteRun(ctx, b.hub, run)
		if err != nil {
			errs = append(errs, err)
		}
		for index, client := range b.workers {
			if worker := run.workers[index]; worker != nil {
				owned := &scaleRun{name: run.name, uid: worker.UID}
				if err := deleteRun(ctx, client, owned); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return errors.Join(errs...)
}

func deleteRun(ctx context.Context, client pipelineclient.PipelineRunInterface, run *scaleRun) error {
	uid := run.uid
	err := client.Delete(ctx, run.name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete owned run %s (%s): %w", run.name, uid, err)
	}
	return nil
}
