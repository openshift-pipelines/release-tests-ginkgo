package kindmulticluster

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
)

type scaleEnvironment struct {
	clusters []scaleCluster // Hub, spoke A, spoke B.
	queues   []*queueControl
	batch    *runBatch
}

// ExecuteScale validates the script-owned two-spoke, quota-ten fixture. The
// first two runs have deterministic worker eligibility; the other eight share
// both workers. No Workload placement or status is written by the test.
func ExecuteScale(ctx context.Context, hubPath string, spokePaths []string) (err error) {
	if len(spokePaths) != 2 {
		return fmt.Errorf("quota-ten scenario requires exactly two spoke kubeconfigs")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	environment := &scaleEnvironment{}
	hosts := map[string]bool{}
	for index, path := range append([]string{hubPath}, spokePaths...) {
		if path == "" {
			return fmt.Errorf("all three kubeconfig paths must be explicit")
		}
		client, err := clients.NewClientsWithContext(path, "", "", namespace)
		if err != nil {
			return err
		}
		if hosts[client.KubeConfig.Host] {
			return fmt.Errorf("hub and spokes must use three distinct API servers")
		}
		hosts[client.KubeConfig.Host] = true
		name := "hub"
		if index != 0 {
			name = fmt.Sprintf("release-spoke-%d", index)
		}
		environment.clusters = append(environment.clusters, scaleCluster{name: name, path: path, clients: client})
		queue, err := readScaleQueue(ctx, client.Dynamic)
		if err != nil {
			return fmt.Errorf("quota-ten fixture on %s: %w", name, err)
		}
		environment.queues = append(environment.queues, queue)
	}
	environment.batch = newRunBatch(environment.clusters[0].clients.PipelineRunClient)
	for _, worker := range environment.clusters[1:] {
		environment.batch.workers = append(environment.batch.workers, worker.clients.PipelineRunClient)
	}
	// Registered before eligibility changes or the first PipelineRun create.
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
		defer stop()
		err = errors.Join(err, environment.batch.cleanup(cleanupCtx))
		for _, queue := range environment.queues[1:] {
			err = errors.Join(err, queue.set(cleanupCtx, "None"))
		}
	}()
	return environment.execute(ctx)
}

func (e *scaleEnvironment) execute(ctx context.Context) error {
	// Hold B until A has completed; then hold A until B has completed.
	// Active + observedGeneration acknowledges Kueue's reservation cache.
	for target := range 2 {
		if err := e.queues[2-target].set(ctx, "Hold"); err != nil {
			return err
		}
		if err := e.queues[1+target].set(ctx, "None"); err != nil {
			return err
		}
		run, err := e.create(ctx, target)
		if err != nil {
			return err
		}
		if err := e.prove(ctx, run); err != nil {
			return err
		}
	}
	for _, queue := range e.queues[1:] {
		if err := queue.set(ctx, "None"); err != nil {
			return err
		}
	}
	remaining := make([]*scaleRun, 0, 8)
	for range 8 {
		run, err := e.create(ctx, -1)
		if err != nil {
			return err
		}
		remaining = append(remaining, run)
	}
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(8)
	for _, run := range remaining {
		group.Go(func() error { return e.prove(groupCtx, run) })
	}
	if err := group.Wait(); err != nil {
		return err
	}
	// All ten retain placement and status assertions, including the two whose
	// observers remained active while later runs executed.
	seen := [2]int{}
	if len(e.batch.runs) != 10 {
		return fmt.Errorf("created %d PipelineRuns, want exactly ten", len(e.batch.runs))
	}
	for _, run := range e.batch.runs {
		placement, complete, err := run.progress(runFinished)
		if err != nil {
			return err
		}
		if !complete {
			return fmt.Errorf("%s has no completed execution proof", run.name)
		}
		seen[placement.worker]++
		if err := assertHubIdle(ctx, e.clusters[0].clients, run.name); err != nil {
			return err
		}
	}
	if seen[0] == 0 || seen[1] == 0 {
		return fmt.Errorf("both spokes must execute: counts=%v", seen)
	}
	for _, cluster := range e.clusters {
		if _, err := readScaleQueue(ctx, cluster.clients.Dynamic); err != nil {
			return fmt.Errorf("final quota on %s: %w", cluster.name, err)
		}
	}
	return nil
}

func (e *scaleEnvironment) create(ctx context.Context, target int) (*scaleRun, error) {
	return e.batch.create(ctx, func(run *scaleRun) error {
		run.target = target
		return run.subscribe(ctx, e.clusters)
	})
}
