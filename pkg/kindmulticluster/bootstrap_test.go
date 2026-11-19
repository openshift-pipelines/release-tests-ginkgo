package kindmulticluster

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	operatorv1alpha1 "github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestBootstrapCleansOnlyOwnedClustersWhenProvisioningFails(t *testing.T) {
	var cases []struct{ shell, stage string }
	stages := []string{"download", "compile", "dry-run", "create", "node-ready", "install", "configure", "suite", "cleanup", "success", "suite-cleanup", "diagnostics"}
	for _, stage := range stages {
		cases = append(cases, struct{ shell, stage string }{"bash", stage})
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		for _, stage := range stages {
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
			artifacts := filepath.Join(dir, "public")
			logPath := filepath.Join(dir, "calls")
			manifestsPath := filepath.Join(dir, "schedulers.yaml")
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
				"CALLS="+logPath, "FAIL_AT="+stage, "ARTIFACTS_DIR="+artifacts, "MANIFESTS="+manifestsPath)

			// When downloading, compiling, or partially creating a cluster fails.
			output, runErr := command.CombinedOutput()

			// Then failure is preserved and cleanup never touches the foreign cluster.
			var exit *exec.ExitError
			wantCode := 42
			switch stage {
			case "success", "diagnostics":
				wantCode = 0
			case "cleanup":
				wantCode = 1
			}
			if wantCode == 0 && runErr != nil || wantCode != 0 && (!errors.As(runErr, &exit) || exit.ExitCode() != wantCode) {
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
			provisioned := stage != "download" && stage != "compile" && stage != "dry-run"
			if provisioned && (created == "" || deleted != created || !strings.HasPrefix(created, "rtg-")) {
				t.Fatalf("partial cluster was not cleaned: %s", calls)
			}
			if !provisioned && (created != "" || deleted != "") {
				t.Fatalf("cluster touched before provisioning: %s", calls)
			}
			if strings.Contains(string(calls), "delete cluster --name unrelated-cluster") {
				t.Fatal("foreign cluster deleted")
			}
			if stage == "success" {
				manifests, err := os.Open(manifestsPath)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := manifests.Close(); err != nil {
						t.Error(err)
					}
				}()
				decoder := k8syaml.NewYAMLOrJSONDecoder(manifests, 4096)
				for range 2 {
					var scheduler unstructured.Unstructured
					if err := decoder.Decode(&scheduler); err != nil {
						t.Fatal(err)
					}
					if scheduler.GetName() != operatorv1alpha1.TektonSchedulerResourceName {
						t.Fatalf("scheduler name %q is ignored by the released Operator", scheduler.GetName())
					}
					if _, found, err := unstructured.NestedMap(scheduler.Object, "spec", "options"); err != nil || !found {
						t.Fatalf("required scheduler options missing: %v", err)
					}
				}
				var extra unstructured.Unstructured
				if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
					t.Fatalf("unexpected extra scheduler: %v", err)
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
			if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("run directory survived cleanup: %s (%v)", runDir, err)
			}
			report, err := os.ReadFile(filepath.Join(artifacts, "junit.xml"))
			if err != nil {
				t.Fatal(err)
			}
			var suite struct {
				Failures int `xml:"failures,attr"`
			}
			if err := xml.Unmarshal(report, &suite); err != nil {
				t.Fatal(err)
			}
			if (suite.Failures > 0) != (wantCode != 0) {
				t.Fatalf("JUnit does not reflect exit %d: %s", wantCode, report)
			}
			if provisioned {
				exportIndex := strings.Index(string(calls), "kind export logs")
				deleteIndex := strings.Index(string(calls), "kind delete cluster")
				if exportIndex < 0 || exportIndex > deleteIndex {
					t.Fatalf("diagnostics not attempted before cleanup: %s", calls)
				}
				for _, name := range []string{"events.txt", "tekton-operator-controller.log", "kind/node/kubelet.log"} {
					if stage == "diagnostics" && strings.HasPrefix(name, "kind/") {
						continue
					}
					if _, err := os.Stat(filepath.Join(artifacts, created, name)); err != nil {
						t.Fatalf("missing exported diagnostic %s: %v", name, err)
					}
				}
			}
			if err := filepath.WalkDir(artifacts, func(path string, entry os.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if strings.Contains(string(data), "secret-sentinel") || strings.Contains(path, "kubeconfig") {
					t.Errorf("credential exported in %s", path)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
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
  kind:create)
    if [[ $FAIL_AT == create ]]; then exit 42; fi
    while [[ $# -gt 0 ]]; do
      if [[ $1 == --kubeconfig ]]; then printf 'secret-sentinel\n' > "$2"; break; fi
      shift
    done ;;
  kind:delete)
    if [[ $FAIL_AT == cleanup || $FAIL_AT == suite-cleanup ]]; then exit 43; fi ;;
  kind:export)
    if [[ $FAIL_AT == diagnostics ]]; then exit 44; fi
    mkdir -p "$5/node"
    printf 'node log\ntoken: secret-sentinel\n' > "$5/node/kubelet.log"
    printf 'secret-sentinel\n' > "$5/admin.kubeconfig" ;;
  go:*)
    if [[ $FAIL_AT == compile ]]; then exit 42; fi
    while [[ $# -gt 0 ]]; do
      if [[ $1 == -o ]]; then
        printf '#!/usr/bin/env bash\nif [[ $* == *-ginkgo.dry-run* ]]; then\n  if [[ $FAIL_AT == dry-run ]]; then exit 42; fi\n  exit 0\nfi\nif [[ $FAIL_AT == suite || $FAIL_AT == suite-cleanup ]]; then exit 42; fi\n' > "$2"
        chmod 700 "$2"
        break
      fi
      shift
    done ;;
  kubectl:*)
    if [[ $* == *'wait --for=condition=Ready node'* && $FAIL_AT == node-ready ]]; then exit 42; fi
    if [[ $* == *'apply --server-side'* && $FAIL_AT == install ]]; then exit 42; fi
    if [[ $* == *'queues.yaml'* && $FAIL_AT == configure ]]; then exit 42; fi
    if [[ $* == *'apply -f -' ]]; then
      printf -- '---\n' >> "$MANIFESTS"
      while IFS= read -r line; do printf '%s\n' "$line" >> "$MANIFESTS"; done
    elif [[ $* == *'create token'* ]]; then printf 'secret-sentinel\n'
    elif [[ $* == *'get tektonpipeline/'* ]]; then printf v1.12.0
    elif [[ $* == *'get tektonscheduler/'* ]]; then printf unknown
    elif [[ $* == *'get deployments'* ]]; then printf 'deployment/controller\n'
    elif [[ $* == *' logs '* ]]; then printf 'controller log\ntoken: secret-sentinel\n'
    fi ;;
  jq:*) while IFS= read -r line; do :; done ;;
  sha256sum:*) while IFS= read -r line; do :; done ;;
  curl:*)
    if [[ $FAIL_AT == download ]]; then exit 42; fi
    while [[ $# -gt 0 ]]; do
      if [[ $1 == -o ]]; then : > "$2"; break; fi
      shift
    done ;;
esac
`
