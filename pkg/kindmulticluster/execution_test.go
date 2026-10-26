package kindmulticluster

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

func TestCompletedWhenPipelineRunChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status corev1.ConditionStatus
		fail   bool
	}{
		{name: "success", status: corev1.ConditionTrue},
		{name: "failure", status: corev1.ConditionFalse, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a real watch subscription with an initially running run.
			events := watch.NewRaceFreeFake()
			defer events.Stop()
			run := &pipelinev1.PipelineRun{}
			run.Status.Conditions = duckv1.Conditions{{Type: apis.ConditionSucceeded, Status: corev1.ConditionUnknown}}
			events.Modify(run.DeepCopy())
			run.Status.Conditions[0].Status = tc.status
			events.Modify(run.DeepCopy())
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			// When the completion observer consumes the events.
			_, err := completed(ctx, events)

			// Then only successful execution is accepted.
			if (err != nil) != tc.fail {
				t.Fatalf("completed() = %v, want failure=%t", err, tc.fail)
			}
		})
	}
}

func TestCompletedRejectsDeletionBeforeSuccess(t *testing.T) {
	// Given a workload that disappears before success is observed.
	events := watch.NewRaceFreeFake()
	defer events.Stop()
	events.Delete(&pipelinev1.PipelineRun{})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	// When completion is checked.
	_, err := completed(ctx, events)

	// Then deletion is not mistaken for successful execution.
	if err == nil {
		t.Fatal("deleted workload accepted as success")
	}
}

func TestPipelineRunUsesRemoteManagementAndPinnedImage(t *testing.T) {
	// Given a new hub workload.
	run := pipelineRun("test-run")

	// When its execution contract is inspected.
	step := run.Spec.PipelineSpec.Tasks[0].TaskSpec.Steps[0]

	// Then it cannot execute locally or race a fixed-duration log window.
	if run.Spec.ManagedBy == nil || *run.Spec.ManagedBy != "kueue.x-k8s.io/multikueue" ||
		run.Labels["kueue.x-k8s.io/queue-name"] != namespace ||
		!strings.Contains(step.Image, "@sha256:") {
		t.Fatal("workload is not a pinned, queue-managed remote execution")
	}
}

func TestWorkloadEmitsMarkerOnlyWhenFIFOReleased(t *testing.T) {
	// Given the real workload script with an isolated FIFO and stdout reader.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	fifo := filepath.Join(t.TempDir(), "gate")
	script := pipelineRun("test").Spec.PipelineSpec.Tasks[0].TaskSpec.Steps[0].Script
	script = strings.ReplaceAll(script, "/tmp/release-gate", `"`+fifo+`"`)
	command := exec.CommandContext(ctx, "sh", "-c", script)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	first, err := reader.ReadString('\n')
	if err != nil || first != readyLine+"\n" {
		cancel()
		waitErr := command.Wait()
		t.Fatalf("readiness=%q error=%v wait=%v", first, err, waitErr)
	}

	// When an external writer releases the FIFO.
	release := exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' "$2" > "$1"`, "sh", fifo, "unique-live-marker")
	releaseErr := release.Run()
	remaining, readErr := io.ReadAll(reader)
	waitErr := command.Wait()

	// Then the script emits that marker and exits successfully.
	if releaseErr != nil || readErr != nil || waitErr != nil || string(remaining) != "unique-live-marker\n" {
		t.Fatalf("release=%v read=%v wait=%v output=%q", releaseErr, readErr, waitErr, remaining)
	}
}
