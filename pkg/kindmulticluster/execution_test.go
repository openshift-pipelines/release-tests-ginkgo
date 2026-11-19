package kindmulticluster

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

func TestReadMarkerRejectsMissingAndFailedLogs(t *testing.T) {
	streamErr := errors.New("log transport failed")
	for _, tc := range []struct {
		name   string
		reader io.Reader
		fail   bool
	}{
		{name: "marker", reader: strings.NewReader("expected\n")},
		{name: "wrong marker", reader: strings.NewReader("different\n"), fail: true},
		{name: "closed stream", reader: strings.NewReader(""), fail: true},
		{name: "transport error", reader: iotest.ErrReader(streamErr), fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a successful, missing, or failed log stream.
			scanner := bufio.NewScanner(tc.reader)
			// When the execution marker is consumed.
			err := readMarker(scanner, "expected")
			// Then only the marker passes and transport errors remain inspectable.
			if (err != nil) != tc.fail {
				t.Fatalf("readMarker = %v, want failure %t", err, tc.fail)
			}
			if tc.name == "transport error" && !errors.Is(err, streamErr) {
				t.Fatalf("transport cause lost: %v", err)
			}
		})
	}
}

func TestPipelineRunLeavesWebhookFieldsUnset(t *testing.T) {
	for _, prelabelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default queue", true: "explicit queue"}[prelabelled], func(t *testing.T) {
			// Given either supported submission mode.
			run, queue := pipelineRun("test", prelabelled)
			// When the pre-admission request is inspected.
			label, labeled := run.Labels[queueLabel]
			// Then queue preservation differs from defaulting and no webhook output is seeded.
			if run.Spec.ManagedBy != nil || run.Spec.Status != "" || labeled != prelabelled {
				t.Fatalf("request bypasses admission: %#v", run.Spec)
			}
			if prelabelled && (label != namespace+"-explicit" || queue != label) {
				t.Fatalf("explicit queue %q must differ from default %q", label, namespace)
			}
			if !prelabelled && queue != namespace {
				t.Fatalf("default queue = %q", queue)
			}
		})
	}
}

func TestValidateAdmissionRejectsMissingMutations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*pipelinev1.PipelineRun)
		fail   bool
	}{
		{name: "all mutations"},
		{name: "queue absent", change: func(run *pipelinev1.PipelineRun) { delete(run.Labels, queueLabel) }, fail: true},
		{name: "explicit queue overwritten", change: func(run *pipelinev1.PipelineRun) { run.Labels[queueLabel] = namespace }, fail: true},
		{name: "managedBy absent", change: func(run *pipelinev1.PipelineRun) { run.Spec.ManagedBy = nil }, fail: true},
		{name: "managedBy wrong", change: func(run *pipelinev1.PipelineRun) { value := "wrong-controller"; run.Spec.ManagedBy = &value }, fail: true},
		{name: "pending absent", change: func(run *pipelinev1.PipelineRun) { run.Spec.Status = "" }, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given an API response with one mutation optionally missing.
			run, queue := pipelineRun("test", true)
			value := managedBy
			run.Spec.ManagedBy = &value
			run.Spec.Status = pipelinev1.PipelineRunSpecStatusPending
			if tc.change != nil {
				tc.change(run)
			}
			// When admission is validated.
			err := validateAdmission(run, queue)
			// Then every missing or overwritten field fails.
			if (err != nil) != tc.fail {
				t.Fatalf("admission error = %v, want failure %t", err, tc.fail)
			}
		})
	}
}

func TestCompletedWhenPipelineRunChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status corev1.ConditionStatus
		fail   bool
	}{
		{name: "success", status: corev1.ConditionTrue},
		{name: "failure", status: corev1.ConditionFalse, fail: true},
		{name: "deletion", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given a watch established before running and completion events.
			events := watch.NewRaceFreeFake()
			defer events.Stop()
			run := &pipelinev1.PipelineRun{}
			run.Status.Conditions = duckv1.Conditions{{Type: apis.ConditionSucceeded, Status: corev1.ConditionUnknown}}
			events.Modify(run.DeepCopy())
			if tc.name == "deletion" {
				events.Delete(run.DeepCopy())
			} else {
				run.Status.Conditions[0].Status = tc.status
				events.Modify(run.DeepCopy())
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			// When completion consumes the events.
			_, err := completed(ctx, events)
			// Then deletion and failure cannot be mistaken for success.
			if (err != nil) != tc.fail {
				t.Fatalf("completed = %v, want failure %t", err, tc.fail)
			}
		})
	}
}

func TestWorkloadCompletesOnlyAfterFIFORelease(t *testing.T) {
	// Given the actual workload script with a private FIFO.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	fifo := filepath.Join(t.TempDir(), "gate")
	run, _ := pipelineRun("test", false)
	script := strings.ReplaceAll(run.Spec.PipelineSpec.Tasks[0].TaskSpec.Steps[0].Script, "/tmp/release-gate", `"`+fifo+`"`)
	command := exec.CommandContext(ctx, "sh", "-c", script)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	first, err := reader.ReadString('\n')
	if err != nil || first != readyLine+"\n" {
		cancel()
		waitErr := command.Wait()
		t.Fatalf("readiness=%q error=%v wait=%v", first, err, waitErr)
	}
	// When the observer releases the gate after readiness.
	release := exec.CommandContext(ctx, "sh", "-c", `printf '%s\n' "$2" > "$1"`, "sh", fifo, "unique-marker")
	releaseErr := release.Run()
	remaining, readErr := io.ReadAll(reader)
	waitErr := command.Wait()
	// Then the marker is emitted and execution exits successfully.
	if releaseErr != nil || readErr != nil || waitErr != nil || string(remaining) != "unique-marker\n" {
		t.Fatalf("release=%v read=%v wait=%v output=%q", releaseErr, readErr, waitErr, remaining)
	}
}

func TestCompletedRejectsInterruptedWatch(t *testing.T) {
	for _, scenario := range []string{"closed", "API error", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			// Given an interrupted watch with no successful completion.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			events := watch.NewRaceFreeFake()
			defer events.Stop()
			switch scenario {
			case "closed":
				events.Stop()
			case "API error":
				events.Error(&metav1.Status{Status: metav1.StatusFailure, Reason: metav1.StatusReasonExpired, Code: 410})
			case "canceled":
				cancel()
			}
			// When completion observes the interruption.
			bounded, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			run, err := completed(bounded, events)
			// Then it fails promptly rather than accepting success or timing out.
			if run != nil || err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("run=%v error=%v", run, err)
			}
			if scenario == "API error" && !apierrors.IsResourceExpired(err) {
				t.Fatalf("API error cause lost: %v", err)
			}
			if scenario == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation cause lost: %v", err)
			}
		})
	}
}
