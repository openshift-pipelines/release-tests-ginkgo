# OpenShift Pipelines Release Tests (Ginkgo)

End-to-end release test suite for OpenShift Pipelines, migrated from [Gauge](https://github.com/openshift-pipelines/release-tests) to [Ginkgo v2](https://onsi.github.io/ginkgo/).

## Test Areas

| Area | Tests | Labels |
|------|-------|--------|
| Ecosystem Tasks | 36 | `ecosystem`, `buildah`, `s2i`, `git-clone` |
| Operator | 33 | `operator`, `admin` |
| Triggers | 23 | `triggers`, `non-admin` |
| Pipelines Core | 16 | `pipelines`, `non-admin` |
| PAC | 7 | `pac` |
| OLM/Install | 3 | `install` |
| Chains | 2 | `chains` |
| Results | 2 | `results` |
| Manual Approval Gate | 2 | `approvalgate` |
| Versions | 2 | `versions` |
| Hub | 1 | `hub` |
| Metrics | 1 | `metrics` |
| Console | 1 | `console` |

## Prerequisites

- OpenShift 4.x cluster with cluster-admin access
- OpenShift Pipelines Operator installed (`TektonConfig` CR ready)
- `oc` and `ginkgo` CLI tools installed
- Go 1.24+

## Running Tests

### Run all sanity tests
```bash
ginkgo run --label-filter=sanity --timeout=30m ./tests/...
```

### Run a specific test area
```bash
ginkgo run --timeout=30m ./tests/ecosystem/
ginkgo run --timeout=30m ./tests/triggers/
ginkgo run --timeout=30m ./tests/operator/
```

### Run with label filtering
```bash
# Only e2e tests, exclude disconnected
ginkgo run --label-filter='e2e && !disconnected' --timeout=30m ./tests/...

# Only admin tests
ginkgo run --label-filter=admin --timeout=30m ./tests/...
```

### Run in parallel
```bash
ginkgo run -p --timeout=30m ./tests/...
```

### Run with JUnit XML output (for Polarion)
```bash
ginkgo run --label-filter=sanity --timeout=30m --junit-report=report.xml ./tests/...
# Post-process for Polarion compatibility:
go run ./cmd/junit-polarion/ < report.xml > polarion-report.xml
```

### Using the wrapper script
```bash
# Sanity preset
./scripts/run-tests.sh sanity

# Full suite
./scripts/run-tests.sh e2e

# Custom label filter
LABEL_FILTER='triggers && sanity' ./scripts/run-tests.sh
```

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `KUBECONFIG` | Yes | Path to kubeconfig file |
| `PIPELINE_VERSION` | For version tests | Expected pipeline version |
| `TRIGGERS_VERSION` | For version tests | Expected triggers version |
| `OPERATOR_VERSION` | For version tests | Expected operator version |
| `CHAINS_VERSION` | For version tests | Expected chains version |
| `PAC_VERSION` | For version/PAC tests | Expected PAC version |
| `GITLAB_TOKEN` | For PAC tests | GitLab API token |
| `GITLAB_WEBHOOK_TOKEN` | For PAC tests | GitLab webhook secret |
| `GITHUB_TOKEN` | For git resolver tests | GitHub token |
| `KO_DOCKER_REPO` | For build tests | Docker registry for test images |

## Project Structure

```
tests/              # Ginkgo test suites (one per area)
  ecosystem/        # Ecosystem task tests (buildah, s2i, git-clone, etc.)
  triggers/         # EventListener, TriggerBinding tests
  operator/         # Addon, RBAC, auto-prune, upgrade tests
  pipelines/        # PipelineRun, TaskRun, resolver tests
  pac/              # Pipelines-as-Code GitLab tests
  chains/           # Tekton Chains signing tests
  results/          # Tekton Results tests
  mag/              # Manual Approval Gate tests
  metrics/          # Monitoring/metrics tests
  versions/         # Component version validation
  olm/              # OLM install/upgrade tests
  hub/              # TektonHub tests
pkg/                # Shared helper packages
  clients/          # Kubernetes/Tekton client initialization
  cmd/              # CLI command execution wrapper
  config/           # Configuration, constants, env flags
  diagnostics/      # ReportAfterEach diagnostic collector
  junit/            # JUnit XML to Polarion transformer
  k8s/              # Kubernetes helpers (watch, wait, events)
  oc/               # OpenShift CLI operations
  opc/              # OpenShift Pipelines CLI operations
  operator/         # Operator CR lifecycle helpers
  pac/              # PAC GitLab integration helpers
  pipelines/        # Pipeline validation helpers
  store/            # Thread-safe test data store
  triggers/         # Trigger test helpers
  wait/             # Polling/condition wait functions
testdata/           # YAML fixtures for tests
scripts/            # CI scripts (run-tests.sh, parity validation)
Dockerfile          # CI Docker image with Ginkgo CLI
```

## CI Docker Image

Build and use the CI image:
```bash
docker build -t release-tests-ginkgo .
docker run -e KUBECONFIG=/kubeconfig -v ~/.kube/config:/kubeconfig release-tests-ginkgo
```

## Migration from Gauge

This repo is the Ginkgo equivalent of [openshift-pipelines/release-tests](https://github.com/openshift-pipelines/release-tests). The migration followed a phased approach:

1. **Foundation** - Fixed imports, upgraded Go/Ginkgo, removed Gauge dependencies
2. **Suite Scaffolding** - Created 11 suite entry points with BeforeSuite/AfterSuite
3. **Sanity Tests** - Migrated first 9 tests, validated JUnit/Polarion pipeline
4. **Full Migration** - Ported all ~137 tests across 11 areas in parallel
5. **CI & Reporting** - Dockerfile, diagnostics, parallel execution, JUnit post-processing
6. **Parity Validation** - Scripts to verify count, labels, and results match Gauge

### Gauge to Ginkgo Mapping

| Gauge | Ginkgo |
|-------|--------|
| Spec header | `Describe` |
| Scenario | `It` |
| Tags | `Label()` |
| Data table | `DescribeTable` / `Entry` |
| Concept | Shared Go helper function |
| Sequential steps | `Ordered` container |
