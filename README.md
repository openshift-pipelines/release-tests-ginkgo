# OpenShift Pipelines Release Tests

End-to-end test suite for [OpenShift Pipelines](https://docs.openshift.com/pipelines/latest/about/about-pipelines.html), written with [Ginkgo v2](https://onsi.github.io/ginkgo/).

## Prerequisites

- OpenShift 4.x cluster with cluster-admin access
- OpenShift Pipelines Operator installed and `TektonConfig` CR in a ready state
- Go 1.24+
- [`ginkgo`](https://onsi.github.io/ginkgo/#installing-ginkgo) CLI — `go install github.com/onsi/ginkgo/v2/ginkgo@latest`
- `oc` CLI authenticated to the cluster

## Configuration

Component versions and cluster settings are read from `env/default/default.properties`.  
Update this file for each release branch before running tests.

Key environment variables:

| Variable | Description |
|----------|-------------|
| `KUBECONFIG` | Path to kubeconfig *(required)* |
| `PIPELINE_VERSION` | Expected Pipelines version |
| `TRIGGERS_VERSION` | Expected Triggers version |
| `OPERATOR_VERSION` | Expected Operator version |
| `CHAINS_VERSION` | Expected Chains version |
| `PAC_VERSION` | Expected PAC version |
| `GITLAB_TOKEN` | GitLab API token *(PAC tests)* |
| `GITHUB_TOKEN` | GitHub token *(resolver tests)* |
| `KO_DOCKER_REPO` | Registry for built test images |

## Running Tests

### Quick start — sanity suite
```bash
./scripts/run-tests.sh sanity
```

### Run a specific area
```bash
ginkgo run --timeout=30m ./tests/pipelines/
ginkgo run --timeout=30m ./tests/triggers/
ginkgo run --timeout=30m ./tests/operator/
```

### Run with a label filter
```bash
# All e2e, skip disconnected
ginkgo run --label-filter='e2e && !disconnected' --timeout=30m ./tests/...

# Admin-only tests
ginkgo run --label-filter=admin --timeout=30m ./tests/...
```

### Run in parallel
```bash
ginkgo run -p --timeout=30m ./tests/...
```

### Generate a JUnit report
```bash
ginkgo run --label-filter=sanity --timeout=30m --junit-report=report.xml ./tests/...
```

### `run-tests.sh` modes

| Mode | Description |
|------|-------------|
| `sanity` | Sanity-labelled tests |
| `smoke` | Smoke-labelled tests |
| `e2e` | Full e2e suite |
| `e2e-connected` | e2e, excluding disconnected scenarios |
| `disconnected` | Disconnected-only tests |
| `all` | No label filter — every test |
| `<any string>` | Used directly as `--label-filter` |

```bash
./scripts/run-tests.sh e2e
LABEL_FILTER='triggers && sanity' ./scripts/run-tests.sh
```

## Test Areas

| Area | Labels |
|------|--------|
| Pipelines Core | `pipelines`, `non-admin` |
| Operator | `operator`, `admin` |
| Triggers | `triggers`, `non-admin` |
| Ecosystem Tasks | `ecosystem`, `buildah`, `s2i`, `git-clone` |
| PAC | `pac` |
| Chains | `chains` |
| Results | `results` |
| Manual Approval Gate | `approvalgate` |
| OLM / Install | `install` |
| Hub | `hub` |
| Metrics | `metrics` |
| Versions | `versions` |

## Project Layout

```
tests/          # Ginkgo test suites (one directory per area)
pkg/            # Shared helper packages
testdata/       # YAML fixtures used by tests
scripts/        # Test runner and parity validation scripts
env/default/    # Default configuration / component versions
```
