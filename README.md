# OpenShift Pipelines Release Tests

End-to-end test suite for [OpenShift Pipelines](https://docs.openshift.com/pipelines/latest/about/about-pipelines.html), written with [Ginkgo v2](https://onsi.github.io/ginkgo/).

## Prerequisites

- OpenShift 4.x cluster with cluster-admin access
- Go 1.24+
- [`ginkgo`](https://onsi.github.io/ginkgo/#installing-ginkgo) CLI — `go install github.com/onsi/ginkgo/v2/ginkgo@latest`
- `oc` CLI authenticated to the cluster

> **Note:** These tests support two scenarios:
> - **Fresh cluster** — operator not yet installed: run the `olm` suite first to install it via OLM (see [Operator Installation](#operator-installation)).
> - **Pre-installed operator** — `TektonConfig` CR already in a ready state: skip the `olm` suite and run feature tests directly (see [Running Tests on a Pre-installed Cluster](#running-tests-on-a-pre-installed-cluster)).

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

OLM subscription defaults (in `env/default/default.properties`):

| Property | Default Value |
|----------|---------------|
| `SUBSCRIPTION_NAME` | `openshift-pipelines-operator-rh` |
| `CHANNEL` | `latest` |
| `CATALOG_SOURCE` | `redhat-operators` |

## Operator Installation

The operator is **not** assumed to be pre-installed. The `olm` test suite (`tests/olm/`) is the install step and must be run first on a fresh cluster.

### How it works

The install test (`PIPELINES-09-TC01`) is an `Ordered` Ginkgo block that runs the following steps sequentially:

1. **Create OLM Subscription** — renders `template/subscription.yaml.tmp` with values from `config.Flags` and applies it via `oc apply`.
2. **Wait for CSV** — polls until `Subscription.Status.InstalledCSV` is set and the `ClusterServiceVersion` phase reaches `Succeeded` (timeout: 8 minutes).
3. **Wait for TektonConfig CR** — ensures the `TektonConfig` CR named `config` exists and is ready.
4. **Post-install configuration** — a series of ordered steps:
   - Define `artifact-hub-api` variable
   - Apply `testdata/hub/tektonhub.yaml`
   - Configure GitHub token for the git resolver
   - Configure the bundles resolver
   - Enable console plugin
   - Enable StatefulSet mode for Pipelines, Chains, and Results
   - Enable Chains signing secret generation
   - Configure Results with Loki and create the Results route
   - Apply Manual Approval Gate (`testdata/manualapprovalgate/manual-approval-gate.yaml`)
   - Validate all component deployments (Triggers, PAC, Hub, MAG, StatefulSets, CLI)
   - Validate TektonInstallerSets, RBAC, quickstarts, and auto-prune cronjob

### Subscription template (`template/subscription.yaml.tmp`)

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: {{.SubscriptionName}}
  namespace: openshift-operators
spec:
  channel: {{.Channel}}
  installPlanApproval: Automatic
  name: {{.SubscriptionName}}
  source: {{.CatalogSource}}
  sourceNamespace: openshift-marketplace
```

### Running the install, upgrade, and uninstall

```bash
# Install the operator on a fresh cluster
ginkgo run --label-filter=install --timeout=30m ./tests/olm/
```

To upgrade the operator, set `UPGRADE_CHANNEL` and run with the `upgrade` label:

```bash
UPGRADE_CHANNEL=pipelines-1.18 ginkgo run --label-filter=upgrade --timeout=30m ./tests/olm/
```

To uninstall:

```bash
ginkgo run --label-filter=uninstall --timeout=30m ./tests/olm/
```

## Running Tests on a Pre-installed Cluster

If the OpenShift Pipelines Operator is already installed and the `TektonConfig` CR is in a ready state, skip the `olm` suite entirely and run the feature tests directly. Each suite creates and cleans up its own isolated namespaces — there is no shared state with the install step.

### Quick start — sanity suite
```bash
export KUBECONFIG=/path/to/kubeconfig
./scripts/run-tests.sh sanity
```

### Run a specific suite
```bash
ginkgo run --timeout=30m ./tests/pipelines/
ginkgo run --timeout=30m ./tests/triggers/
ginkgo run --timeout=30m ./tests/operator/
```

### Run all feature tests (exclude install/upgrade/uninstall)
```bash
ginkgo run --label-filter='!install && !upgrade && !uninstall' --timeout=60m ./tests/...
```

## Running Tests on a Fresh Cluster

Run the `olm` suite first to install the operator via OLM, then run the feature suites.

```bash
# Step 1 — Install the operator
export KUBECONFIG=/path/to/kubeconfig
ginkgo run --label-filter=install --timeout=30m ./tests/olm/

# Step 2 — Run feature tests
ginkgo run --label-filter='e2e && !disconnected' --timeout=60m ./tests/...
```

## Running Tests (general options)

### Run a specific area

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

| Area | Labels | Test Suite |
|------|--------|------------|
| OLM / Install | `install`, `upgrade`, `uninstall` | `tests/olm/` |
| Pipelines Core | `pipelines`, `non-admin` | `tests/pipelines/` |
| Operator | `operator`, `admin` | `tests/operator/` |
| Triggers | `triggers`, `non-admin` | `tests/triggers/` |
| Ecosystem Tasks | `ecosystem`, `buildah`, `s2i`, `git-clone` | `tests/ecosystem/` |
| PAC | `pac` | `tests/pac/` |
| Chains | `chains` | `tests/chains/` |
| Results | `results` | `tests/results/` |
| Manual Approval Gate | `approvalgate` | `tests/mag/` |
| Hub | `hub` | `tests/hub/` |
| Metrics | `metrics` | `tests/metrics/` |
| Versions | `versions` | `tests/versions/` |

## Project Layout

```
tests/          # Ginkgo test suites (one directory per area)
  olm/          #   OLM install / upgrade / uninstall
  pipelines/    #   Pipelines core + resolvers
  operator/     #   Operator config (RBAC, auto-prune, HPA, etc.)
  triggers/     #   Triggers & EventListeners
  ecosystem/    #   Ecosystem tasks (buildah, s2i, jib, kn, git-clone)
  chains/       #   Tekton Chains (image signing)
  results/      #   Tekton Results
  pac/          #   Pipelines as Code
  hub/          #   Tekton Hub
  mag/          #   Manual Approval Gate
  metrics/      #   Prometheus metrics
  versions/     #   Component version verification
pkg/            # Shared helper packages
  clients/      #   Kubernetes/Tekton client wrappers
  oc/           #   oc CLI wrappers (Create, Apply, Delete, ...)
  olm/          #   OLM subscription helpers
  operator/     #   TektonConfig / component validation helpers
  pipelines/    #   PipelineRun validation helpers
  hooks/        #   Auto namespace-per-Describe lifecycle hook
  store/        #   Global current-namespace store
  config/       #   Reads env vars + default.properties
  wait/         #   Polling / retry utilities
testdata/       # YAML fixtures applied by tests (oc.Create / oc.Apply)
template/       # Go text/template files (e.g. subscription.yaml.tmp)
scripts/        # run-tests.sh entry point
env/default/    # default.properties — component versions and OLM config
```
