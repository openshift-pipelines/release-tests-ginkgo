// Package diagnostics provides cluster diagnostic collection for Ginkgo ReportAfterEach.
// It collects pod logs, events, and resource state when tests fail, attaching them
// to the Ginkgo report for CI debuggability without cluster access.
package diagnostics

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // dot import is idiomatic for Ginkgo
)

const (
	// commandTimeout is the maximum time allowed for each oc command.
	commandTimeout = 30 * time.Second

	// maxEventLines caps the number of event lines collected.
	maxEventLines = 100

	// maxPods caps the number of pods from which logs are collected.
	maxPods = 10

	// maxLogLines caps the number of log lines per container.
	maxLogLines = 50
)

// CollectOnFailure returns a function suitable for use with ReportAfterEach.
// It collects cluster diagnostics (events, resource state, pod logs) only when
// a spec fails, panics, times out, or is interrupted.
//
// The namespace parameter is a pointer so that the current namespace value is
// read at report time (not at registration time), supporting per-spec namespaces.
//
// Usage:
//
//	var lastNamespace string
//	var _ = ReportAfterEach(diagnostics.CollectOnFailure(&lastNamespace))
func CollectOnFailure(namespace *string) func(SpecReport) {
	return func(report SpecReport) {
		if !report.Failed() {
			return
		}

		ns := ""
		if namespace != nil {
			ns = *namespace
		}
		if ns == "" {
			AddReportEntry("Diagnostics Skipped", "No namespace available for diagnostic collection",
				ReportEntryVisibilityFailureOrVerbose)
			return
		}

		events := collectEvents(ns)
		AddReportEntry("Cluster Events", events, ReportEntryVisibilityFailureOrVerbose)

		resources := collectResourceState(ns)
		AddReportEntry("Resource State", resources, ReportEntryVisibilityFailureOrVerbose)

		podLogs := collectPodLogs(ns)
		AddReportEntry("Pod Logs", podLogs, ReportEntryVisibilityFailureOrVerbose)
	}
}

// collectEvents runs oc get events sorted by timestamp and caps output.
func collectEvents(namespace string) string {
	out, err := runOC("get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
	if err != nil {
		return fmt.Sprintf("[error collecting events: %v]\n%s", err, out)
	}
	return capLines(out, maxEventLines)
}

// collectResourceState runs oc get for pipeline/task resources and pods.
func collectResourceState(namespace string) string {
	out, err := runOC("get", "pipelineruns,taskruns,pods", "-n", namespace, "-o", "wide")
	if err != nil {
		return fmt.Sprintf("[error collecting resource state: %v]\n%s", err, out)
	}
	return out
}

// collectPodLogs collects the last N lines of logs from each container in each pod,
// limited to maxPods pods.
func collectPodLogs(namespace string) string {
	// List pod names
	podListOut, err := runOC("get", "pods", "-n", namespace, "-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		return fmt.Sprintf("[error listing pods: %v]\n%s", err, podListOut)
	}

	podNames := strings.Fields(strings.TrimSpace(podListOut))
	if len(podNames) == 0 {
		return "[no pods found in namespace]"
	}

	// Cap the number of pods
	if len(podNames) > maxPods {
		podNames = podNames[:maxPods]
	}

	var sb strings.Builder
	for _, pod := range podNames {
		// Get container names for this pod
		containerOut, err := runOC("get", "pod", pod, "-n", namespace,
			"-o", "jsonpath={.spec.containers[*].name}")
		if err != nil {
			sb.WriteString(fmt.Sprintf("--- Pod: %s [error getting containers: %v] ---\n", pod, err))
			continue
		}

		containers := strings.Fields(strings.TrimSpace(containerOut))
		for _, container := range containers {
			sb.WriteString(fmt.Sprintf("--- Pod: %s  Container: %s (last %d lines) ---\n",
				pod, container, maxLogLines))

			logOut, err := runOC("logs", "-n", namespace, pod, "-c", container,
				fmt.Sprintf("--tail=%d", maxLogLines))
			if err != nil {
				sb.WriteString(fmt.Sprintf("[error: %v]\n", err))
			} else {
				sb.WriteString(logOut)
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// runOC executes an oc command with a timeout context.
// It uses exec.CommandContext directly (not pkg/cmd) to avoid test framework
// assertions in the reporter context.
func runOC(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "oc", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return stdout.String() + stderr.String(), fmt.Errorf("oc %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

// capLines truncates output to maxLines, adding a truncation notice if needed.
func capLines(output string, maxLines int) string {
	lines := strings.Split(output, "\n")
	if len(lines) <= maxLines {
		return output
	}
	truncated := strings.Join(lines[:maxLines], "\n")
	return truncated + fmt.Sprintf("\n... [truncated: showing %d of %d lines]", maxLines, len(lines))
}
