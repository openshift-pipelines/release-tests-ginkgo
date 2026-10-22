package kindmulticluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBootstrapCleansOnlyOwnedClustersWhenProvisioningFails(t *testing.T) {
	shells := []string{"bash"}
	if _, err := os.Stat("/bin/bash"); err == nil {
		shells = append(shells, "/bin/bash")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	type bootstrapCase struct {
		shell, stage string
		spokes       int
	}
	cases := make([]bootstrapCase, 0, 8*len(shells))
	for _, shell := range shells {
		for _, spokes := range []int{1, 2} {
			for _, stage := range []string{"download", "compile", "create-last", "success"} {
				cases = append(cases, bootstrapCase{shell, stage, spokes})
			}
		}
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%d-spokes/%s", tc.shell, tc.spokes, tc.stage), func(t *testing.T) {
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
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			args := []string{script}
			if tc.spokes == 2 {
				args = append(args, "--spokes=2")
			}
			command := exec.CommandContext(ctx, tc.shell, args...)
			command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"CALLS="+logPath, "FAIL_AT="+stage, fmt.Sprintf("EXPECTED_CLUSTERS=%d", tc.spokes+1))

			// When downloading, compiling, or partially creating a cluster fails.
			output, runErr := command.CombinedOutput()

			// Then failure is preserved and cleanup never touches the foreign cluster.
			calls, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			created, deleted := map[string]bool{}, map[string]bool{}
			var runDir string
			internalEndpoints, quotaPatches := 0, 0
			for _, line := range strings.Split(string(calls), "\n") {
				fields := strings.Fields(line)
				if strings.HasPrefix(line, "kind create cluster --name ") {
					created[fields[4]] = true
				}
				if strings.HasPrefix(line, "kind delete cluster --name ") {
					deleted[fields[4]] = true
				}
				if strings.HasPrefix(line, "curl ") {
					runDir = filepath.Dir(filepath.Dir(fields[len(fields)-1]))
				}
				if strings.HasPrefix(line, "kind get kubeconfig ") && strings.HasSuffix(line, " --internal") {
					internalEndpoints++
				}
				if strings.HasPrefix(line, "kubectl ") {
					if !strings.Contains(line, " --kubeconfig ") {
						t.Fatalf("ambient kubeconfig used: %s", line)
					}
					if strings.Contains(line, "quota-ten.yaml") {
						quotaPatches++
					}
				}
			}
			if runDir == "" {
				t.Fatal("no isolated artifact directory was used")
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(runDir); err != nil {
					t.Error(err)
				}
			})
			wantClusters := 0
			switch stage {
			case "download", "compile":
			case "create-last", "success":
				wantClusters = tc.spokes + 1
			default:
				t.Fatal("unknown bootstrap stage")
			}
			if len(created) != wantClusters || len(deleted) != wantClusters {
				t.Fatalf("creates=%v deletes=%v want=%d\n%s", created, deleted, wantClusters, calls)
			}
			for name := range deleted {
				if !created[name] || !strings.HasPrefix(name, "rtg-") {
					t.Fatalf("deleted an unowned cluster %s", name)
				}
			}
			if stage == "success" {
				if runErr != nil {
					t.Fatalf("bootstrap error=%v output=%s", runErr, output)
				}
				wantQuotaPatches := 0
				label := "kind-one-spoke"
				if tc.spokes == 2 {
					wantQuotaPatches, label = 3, "kind-two-spoke"
				}
				if internalEndpoints != tc.spokes || quotaPatches != wantQuotaPatches ||
					!strings.Contains(string(calls), "-ginkgo.label-filter="+label) ||
					!strings.Contains(string(calls), "-ginkgo.fail-on-empty") {
					t.Fatalf("wrong topology, quota, or suite selection:\n%s", calls)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(runErr, &exit) || exit.ExitCode() != 42 {
					t.Fatalf("bootstrap error=%v output=%s", runErr, output)
				}
			}
			if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("run directory survived cleanup: %s (%v)", runDir, err)
			}
		})
	}
}
