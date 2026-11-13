package kindmulticluster

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBootstrapCleansOnlyOwnedClustersWhenProvisioningFails(t *testing.T) {
	cases := []struct{ shell, stage string }{
		{"bash", "download"},
		{"bash", "compile"},
		{"bash", "create"},
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		for _, stage := range []string{"download", "compile", "create"} {
			cases = append(cases, struct{ shell, stage string }{"/bin/bash", stage})
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.shell+"/"+tc.stage, func(t *testing.T) {
			stage := tc.stage
			// Given command-boundary fakes, including an unrelated Kind cluster.
			dir := t.TempDir()
			logPath := filepath.Join(dir, "calls")
			for _, name := range []string{"kind", "docker", "curl", "go", "kubectl", "jq", "sha256sum"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(fakeProvisioner), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			script, err := filepath.Abs("../../scripts/kind-multicluster.sh")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, tc.shell, script)
			command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"CALLS="+logPath, "FAIL_AT="+stage)

			// When downloading, compiling, or partially creating a cluster fails.
			output, runErr := command.CombinedOutput()

			// Then failure is preserved and cleanup never touches the foreign cluster.
			var exit *exec.ExitError
			if !errors.As(runErr, &exit) || exit.ExitCode() != 42 {
				t.Fatalf("bootstrap error=%v output=%s", runErr, output)
			}
			calls, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			var created, deleted, runDir string
			for _, line := range strings.Split(string(calls), "\n") {
				fields := strings.Fields(line)
				if strings.HasPrefix(line, "kind create cluster --name ") {
					created = fields[4]
				}
				if strings.HasPrefix(line, "kind delete cluster --name ") {
					deleted = fields[4]
				}
				if strings.HasPrefix(line, "curl ") {
					runDir = filepath.Dir(filepath.Dir(fields[len(fields)-1]))
				}
			}
			if stage == "create" && (created == "" || deleted != created || !strings.HasPrefix(created, "rtg-")) {
				t.Fatalf("partial cluster was not cleaned: %s", calls)
			}
			if stage != "create" && (created != "" || deleted != "") {
				t.Fatalf("cluster touched before provisioning: %s", calls)
			}
			if strings.Contains(string(calls), "delete cluster --name unrelated-cluster") {
				t.Fatal("foreign cluster deleted")
			}
			if runDir == "" {
				t.Fatal("no isolated artifact directory was used")
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(runDir); err != nil {
					t.Error(err)
				}
			})
			if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("run directory survived cleanup: %s (%v)", runDir, err)
			}
		})
	}
}

// These fakes exercise shell ownership/cleanup, not component readiness or
// artifact integrity. No Kubernetes API or Docker daemon is contacted.
const fakeProvisioner = `#!/usr/bin/env bash
set -eu
tool=${0##*/}
printf '%s %s\n' "$tool" "$*" >> "$CALLS"
case "$tool:$1" in
  kind:version) printf 'kind v0.31.0 go1.26.4 linux/amd64\n' ;;
  kind:get) printf 'unrelated-cluster\n' ;;
  kind:create) exit 42 ;;
  go:*) if [[ $FAIL_AT == compile ]]; then exit 42; fi ;;
  sha256sum:*) while IFS= read -r line; do :; done ;;
  curl:*)
    if [[ $FAIL_AT == download ]]; then exit 42; fi
    while [[ $# -gt 0 ]]; do
      if [[ $1 == -o ]]; then : > "$2"; break; fi
      shift
    done ;;
esac
`
