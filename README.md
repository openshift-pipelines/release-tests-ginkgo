# OpenShift Pipelines Release Tests

End-to-end test suite for [OpenShift Pipelines](https://docs.openshift.com/pipelines/latest/about/about-pipelines.html), written with [Ginkgo v2](https://onsi.github.io/ginkgo/).

## Prerequisites

- OpenShift 4.x cluster with cluster-admin access
- Go 1.24+
- [`ginkgo`](https://onsi.github.io/ginkgo/#installing-ginkgo) CLI — `go install github.com/onsi/ginkgo/v2/ginkgo@latest`
- `oc` CLI authenticated to the cluster

> **Note:** These tests support two scenarios:
> - **Fresh cluster** — operator not yet installed: run the `install` suite first to install it via OLM (see [Operator Installation](#operator-installation)).
> - **Pre-installed operator** — `TektonConfig` CR already in a ready state: skip the `olm` suite and run feature tests directly (see [Running Tests on a Pre-installed Cluster](#running-tests-on-a-pre-installed-cluster)).

## Configuration

Component versions and cluster settings are read from `env/default/default.properties`.  
Update this file for each release branch before running tests.

Key environment variables:

| Variable | Description |
|----------|-------------|
| `KUBECONFIG` | Optional kubeconfig file or platform-separated list; defaults to `~/.kube/config` |
| `PIPELINE_VERSION` | Expected Pipelines version |
| `TRIGGERS_VERSION` | Expected Triggers version |
| `OPERATOR_VERSION` | Expected Operator version |
| `CHAINS_VERSION` | Expected Chains version |
| `PAC_VERSION` | Expected PAC version |
| `GITLAB_TOKEN` | GitLab API token *(PAC tests)* |
| `GITHUB_TOKEN` | GitHub token *(resolver tests)* |
| `MAG_USE_IMPERSONATION` | `true`/`false`: impersonate the MAG test users instead of logging in as them; defaults to `true` on hosted control plane clusters *(MAG group-user tests, see below)* |
| `KO_DOCKER_REPO` | Registry for built test images |
| `CHAINS_REPOSITORY` | OCI repo to push Kaniko-built images to *(Chains TC02)* |
| `CHAINS_DOCKER_CONFIG_JSON` | Raw JSON contents of `docker/config.json` with push access to `CHAINS_REPOSITORY` *(Chains TC02)* |
| `KUEUE_NAMESPACE` | Kueue operator namespace; defaults to `openshift-kueue-operator` |
| `PIPELINE_NAMESPACE` | Pipelines operator namespace; defaults to `openshift-operators` |
| `CERTMANAGER_NAMESPACE` | cert-manager operator namespace; defaults to `cert-manager-operator` |
| `CHANNEL_KUEUE` | Kueue operator channel; defaults to `stable-v1.4` |
| `CHANNEL_PIPELINE` | Pipelines operator channel; defaults to `latest` |
| `CHANNEL_CERTMANAGER` | cert-manager operator channel; defaults to `stable-v1` |

OLM subscription defaults (in `env/default/default.properties`):

| Property | Default Value |
|----------|---------------|
| `SUBSCRIPTION_NAME` | `openshift-pipelines-operator-rh` |
| `CHANNEL` | `latest` |
| `CATALOG_SOURCE` | `redhat-operators` |

## Operator Installation

The operator is **not** assumed to be pre-installed. The `olm` test suite (`tests/olm/`) is the install step and must be run first on a fresh cluster.

### How it works

The install test (`Install openshift-pipelines operator`) is an `Ordered` Ginkgo block that runs the following steps sequentially:

1. **Create OLM Subscription** — renders `template/subscription.yaml.tmp` with values from `config.Flags` and applies it via `oc apply`.
2. **Wait for CSV** — polls until `Subscription.Status.InstalledCSV` is set and the `ClusterServiceVersion` phase reaches `Succeeded` (timeout: 8 minutes).
3. **Wait for TektonConfig CR** — ensures the `TektonConfig` CR named `config` exists and is ready.
4. **Wait for webhook deployments** — explicitly waits for `tekton-operator-proxy-webhook` and `tekton-pipelines-webhook` (both in `openshift-pipelines`) to have `AvailableReplicas ≥ 1`. This prevents a race condition on fresh CI clusters where the webhook pods are still starting when the first `oc patch TektonConfig` is attempted.
5. **Post-install configuration** — a series of ordered steps:
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
./scripts/run-tests.sh install
# or equivalently:
ginkgo run --label-filter=install --timeout=30m ./tests/olm/
```

To upgrade the operator, set `UPGRADE_CHANNEL` and run with the `upgrade` label:

```bash
UPGRADE_CHANNEL=pipelines-1.18 ./scripts/run-tests.sh upgrade
# or equivalently:
UPGRADE_CHANNEL=pipelines-1.18 ginkgo run --label-filter=upgrade --timeout=30m ./tests/olm/
```

To uninstall:

```bash
./scripts/run-tests.sh uninstall
# or equivalently:
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

### Select a kubeconfig or context

`KUBECONFIG` uses the standard client-go loading rules, including merged path lists. Test flags must follow `--` so Ginkgo passes them to the test binary.

```bash
# Standard environment loading, including multiple files on Linux and macOS
export KUBECONFIG=/path/to/hub:/path/to/additional-config
./scripts/run-tests.sh sanity

# Explicit file and context with the wrapper
./scripts/run-tests.sh sanity -- --kubeconfig=/path/to/config --context=my-context

# Equivalent direct Ginkgo invocation
ginkgo run ./tests/pipelines/ -- --kubeconfig=/path/to/config --context=my-context
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

| Mode | Label filter | Test path |
|------|-------------|-----------|
| `install` | `install` | `tests/olm/` |
| `upgrade` | `upgrade` | `tests/olm/` |
| `uninstall` | `uninstall` | `tests/olm/` |
| `sanity` | `sanity` | `tests/...` |
| `smoke` | `smoke` | `tests/...` |
| `e2e` | `e2e` | `tests/...` |
| `e2e-connected` | `e2e && !disconnected` | `tests/...` |
| `disconnected` | `disconnected` | `tests/...` |
| `all` | *(none)* | `tests/...` |
| `<any string>` | used directly as `--label-filter` | `tests/...` |

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
| Tekton Kueue | `tekton-kueue`, `install`, `prerequisites`, `workload`, `proxy-logs`, `admin` | `tests/tektonkueue/` |

### Running the Tekton Kueue suite

Provide one hub kubeconfig and at least one spoke kubeconfig. Test-binary flags must follow `--`.

#### OpenShift Multicluster prerequisite readiness

Run the prerequisite Story independently of PipelineRun execution:

```bash
ginkgo run --procs=1 --timeout=6h --grace-period=25m \
  --label-filter='tekton-kueue && prerequisites' ./tests/tektonkueue -- \
  --kubeconfig /path/to/hub --context hub-context \
  --spoke-kubeconfig /path/to/spoke-1 --spoke-context spoke-1-context \
  --spoke-kubeconfig /path/to/spoke-2 --spoke-context spoke-2-context
```

Every explicit hub/spoke target requires cluster-admin credentials and OLM with
the configured operator catalogs. Distinct API servers are required; a hub
registered as its own worker does not satisfy this spec. Context overrides are
optional, but if supplied there must be one per spoke kubeconfig.

The `install`, `workload`, and `prerequisites` specs share target construction and
discovery-aware `EnsurePrerequisites`: they install or reuse OpenShift Pipelines,
cert-manager, and Red Hat Kueue on each target, waiting for each API to be served.
Existing OLM subscriptions retain their channels and catalog sources.

The prerequisite spec uses the existing `Environment` to enable Hub/Spoke
Multicluster roles, configure Kueue and queues, and register real spoke workers
with scoped credentials. `Environment.AssertReady(ctx)` checks the current state:

* Kueue `Available`, TektonConfig and TektonScheduler `Ready` on every target,
  plus hub TektonMulticlusterProxyAAE and SyncerService `Ready`, with no degraded
  or terminating resources. Kueue and SyncerService must report current
  `status.observedGeneration`; queues, admission checks and workers must report
  current condition `observedGeneration`. The pinned v0.80 TektonConfig,
  TektonScheduler and proxy controllers do not publish generation observations:
  their evidence is the desired Hub/Spoke configuration, scheduler spec
  propagation, component target namespace, Deployment rollouts and live proxy
  readiness, not an unsupported CR-generation requirement.
* Each component's matching Deployments in its CR's target namespace are
  observed, updated, and available with a nonzero desired replica count.
* Test ClusterQueues and LocalQueues are `Active`, the hub AdmissionCheck and
  every registered MultiKueueCluster are `Active`, and ResourceFlavors and the
  MultiKueueConfig exist.
* Only after worker readiness, the hub proxy's actual `/ready` endpoint returns
  HTTP 200 through the API pod proxy, using its Deployment selector and HTTP
  readiness probe port.

Failures are test failures, not successful skips. This readiness spec does not
submit a PipelineRun; use `workload` below for execution coverage. Cleanup uses
the existing restoration and ownership checks to restore scheduler/Kueue
configuration and remove test-owned resources. OLM installations are kept.

Bootstrap the required operators:

```bash
ginkgo run --procs=1 --label-filter='tekton-kueue && install' ./tests/tektonkueue -- \
  --kubeconfig /path/to/hub \
  --spoke-kubeconfig /path/to/spoke-1
```

#### OpenShift execution, returned status and console proxy live logs

This draft depends on prerequisite readiness commit `3d68ad4`. The execution
spec independently bootstraps its environment and calls `WaitReady` and
`AssertReady`; running the prerequisite spec in a previous process is not required.

Provide `MULTICLUSTER_CONSOLE_TOKEN_FILE`, a local file containing only an explicit
hub **user OAuth access token** (protect it with mode 0600). The test does not use
the kubeconfig's credential for console requests. The administrator hub kubeconfig
must be able to read the corresponding `oauth.openshift.io/v1` OAuthAccessToken:
the test checks its server-side creation time and `expiresIn`, rejects expired or
unbounded tokens, and requires a total lifetime of at most three hours. Provision
that bounded user credential before running the spec; the test neither mints
tokens nor changes OAuth configuration. Every proxy request/stream is bounded by
the token's expiration and the observation's ten-minute deadline.

The token must use OpenShift's supported `sha256~payload` form. Object lookup
strips that prefix, hashes only the payload, and prefixes the encoded hash.
The bearer is not sent to the console as an authentication substitute.

Also provide `MULTICLUSTER_CONSOLE_SESSION_FILE`, exported **out-of-band from an
already authenticated console session** for this exact Console Route host. It is
a JSON array of Go `http.Cookie` objects: `Name` and opaque `Value` are required,
`Path` must be `/`, `Secure` must be true, and optional `Domain` must name this
console host. Preserve the session's actual cookie names/values, `HttpOnly` and
optional RFC3339 `Expires`. Protect both files with mode 0600. The test does not
perform browser automation, acquire a session, or fabricate cookies from a bearer.

Both HTTP and WebSocket requests use this cookie jar. Before arming resource
watches or allowing a trigger, the test requests
`/api/kubernetes/apis/user.openshift.io/v1/users/~` through the authenticated
console and requires its User identity to match `OAuthAccessToken.UserName`.
This proves the same user, not that two independently supplied credentials are
the same token. The OAuth expiration bounds the observer even if the session has
a longer lifetime; the console still enforces its own session validity. The
console's UserToken middleware supplies backend Authorization from that session.
Bearer-only incoming requests cannot pass this console authentication boundary.

The authenticated console user needs hub namespace `get` access to PipelineRuns and Pods,
`list` access to TaskRuns and Pods, and `get` access to Pod logs. The worker
credential gets only the additional namespaced TaskRun/Pod/log reads
needed by the proxy; cluster-wide reads stay unchanged and Secret access remains
denied. Pod exec for the two test gates uses the spoke administrator client, not
the worker credential.

The test discovers the admitted TLS Console Route and requires a managed Console
with `pipelines-console-plugin` enabled. Its ConsolePlugin must register exactly
one `multicluster-proxy` Service endpoint with `UserToken` authorization. HTTP and
WebSocket requests use the exact console-facing base
`/api/proxy/plugin/pipelines-console-plugin/multicluster-proxy` and proxy v0.1.1
`resolve`, `taskruns`, `pods` and `logs/stream` endpoints. The runner must trust
the console ingress certificate (use the standard `SSL_CERT_FILE`/`SSL_CERT_DIR`
trust configuration for a private ingress CA). TLS verification is never
disabled. Redirects, login HTML, invalid JSON and failed upgrades fail the spec.

Live-cluster invocation:

```bash
export MULTICLUSTER_CONSOLE_TOKEN_FILE=/secure/path/hub-user-oauth-token
export MULTICLUSTER_CONSOLE_SESSION_FILE=/secure/path/console-session-cookies.json
go run github.com/onsi/ginkgo/v2/ginkgo run \
  --procs=1 --timeout=8h --grace-period=25m --fail-on-empty \
  --label-filter='tekton-kueue && proxy-logs' ./tests/tektonkueue -- \
  --kubeconfig /path/to/hub \
  --spoke-kubeconfig /path/to/spoke-1 \
  --spoke-kubeconfig /path/to/spoke-2
```

Repeat `--spoke-kubeconfig` for additional spokes and optionally provide one matching
`--spoke-context` per spoke. The test subscribes to namespace resource watches before
hub creation, requires exactly one executing spoke and no hub TaskRuns, and
resolves the same spoke through the console proxy. It retains PipelineRun,
TaskRun, Pod and Workload UIDs/owners, including transient create/delete events
and duplicates, on every target. Execution watch close, error or compaction
permanently fails closed: unlike readiness it never relists to repair history.
Unowned candidate Workload copies on eligible workers are UID-pinned for cleanup,
not counted as execution on those workers.
The producer emits readiness,
blocks on FIFO gate 1, echoes a newly generated random challenge after gate 1 is
released, then blocks on gate 2. Both markers must arrive on the same WebSocket.
Only after receiving the new marker and verifying nonterminal hub/spoke status
does the test release gate 2. It requires independently observed spoke success,
returned hub success, hub Workload completion and worker cleanup. Observation and
execution cleanup use list/watch snapshots, not sleep-based timing windows.
Direct spoke clients control gates and verify state but never supply accepted
log bytes.

For external creation, call `armed, err := environment.Arm(ctx)` **before** the
trigger, and call `armed.Observe(actualHubRun)` afterward. Arm establishes the
authenticated console session, all list/watch histories, and environment cleanup
before returning. Observe binds the actual server-verified hub UID; creation
already present before arming and replacement identities are rejected. The caller
must stop/finish its trigger before environment cleanup. Failure before binding
still permits guarded deletion of test-owned hub runs; cleanup uses the original
histories and UID preconditions, never a repaired execution history. The run must
implement the same single-task two-gate producer protocol. No PAC scenario is
included here.

Console JSON responses have a hard four-MiB cap, including trailing whitespace.
Errors expose bounded categories/status or close codes, not typed JSON payloads,
close reason strings, bearer values or session cookies. Cancellation preserves
its context error identity.

Cluster-free verification:

```bash
go test -race -shuffle=on -count=1 ./pkg/tektonkueue
go test -run='^$' -count=0 ./tests/tektonkueue
go run github.com/onsi/ginkgo/v2/ginkgo run --dry-run --fail-on-empty \
  --label-filter='tekton-kueue && proxy-logs' ./tests/tektonkueue
```

Live OpenShift execution is **unverified** in this draft. TLS/WebSocket/SPDY wire
tests, a real shell FIFO producer test, and deterministic API watch tests do not
replace a run against provisioned hub/spoke clusters. This is console-facing
endpoint coverage, not browser rendering or console UI coverage.

The workload test temporarily configures and restores `TektonConfig.spec.scheduler`, Kueue, scoped worker credentials, RBAC, queues, and test-owned network access. Test namespaces and resources are removed even after failure. OLM operator installations remain available, matching the install suite. Secret synchronization, CEL routing, and NetworkPolicy behavior are separate coverage.

### Running the Chains suite

The Chains suite has two test cases with different requirements:

| Test case | What it tests | Extra env vars required |
|-----------|---------------|------------------------|
| TaskRun signing | Signature stored as annotation (`tekton` storage) | None |
| Image signing + attestation | Kaniko build → sign + attest via Rekor | `CHAINS_REPOSITORY`, `CHAINS_DOCKER_CONFIG_JSON` |

**TaskRun signing only (no registry needed):**
```bash
export KUBECONFIG=/path/to/kubeconfig
ginkgo run --label-filter='chains && sanity' --timeout=10m ./tests/chains/
```

**Full Chains suite (image signing, requires a registry):**
```bash
export KUBECONFIG=/path/to/kubeconfig
export CHAINS_REPOSITORY=quay.io/<your-org>/chainstest        # repo to push signed images to
export CHAINS_DOCKER_CONFIG_JSON=$(cat ~/.docker/config.json) # raw JSON — do NOT base64-encode

ginkgo run --label-filter=chains --timeout=30m ./tests/chains/
```

> **`CHAINS_DOCKER_CONFIG_JSON`** must be the **raw JSON** contents of a `docker/config.json`
> (i.e. the output of `cat ~/.docker/config.json`) that has push access to `CHAINS_REPOSITORY`.
> Do **not** base64-encode it — the test passes it directly to `oc create secret` as
> `--from-literal=.dockerconfigjson=<value>` which requires plain JSON.

### Running the Manual Approval Gate suite

The group-user specs (`mag-group-user`, PIPELINES-37) approve and reject ApprovalTasks as
five different users, `user1`–`user5`, who are placed in OpenShift Groups by the tests:

- On a standalone cluster the users must exist in an HTPasswd identity provider. The
  tests log in as each user with `oc login`, using `<USER>_PASS` (e.g. `USER1_PASS`) as
  the password and falling back to the user name.
- On hosted control plane clusters (HyperShift / ROSA HCP) the OAuth configuration is
  owned by the management cluster and no identity provider can be added, so the tests
  instead impersonate the users with the test runner's own credentials (which therefore
  need `impersonate` rights on users and groups, as cluster-admin has). This is detected
  automatically from `infrastructure/cluster`; set `MAG_USE_IMPERSONATION=true|false`
  to force either mode.

```bash
export KUBECONFIG=/path/to/kubeconfig
ginkgo run --label-filter=approvalgate --timeout=30m ./tests/mag/
```

## Local Testing Walkthrough

This section walks through the full cycle for testing changes on your own OpenShift cluster.

### 1 — One-time setup

```bash
# Clone and enter the repo
git clone https://github.com/openshift-pipelines/release-tests-ginkgo
cd release-tests-ginkgo

# Install the ginkgo CLI (if not already installed)
go install github.com/onsi/ginkgo/v2/ginkgo@latest

# Log in to your local cluster
oc login --server=https://<api-url>:6443 -u kubeadmin -p <password>
# or point KUBECONFIG at an existing kubeconfig file:
export KUBECONFIG=$HOME/.kube/config

# Verify cluster access
oc whoami
oc get nodes
```

### 2 — Configure component versions

Edit `env/default/default.properties` to match the operator version installed on your cluster:

```bash
# Check what's currently installed
oc get csv -n openshift-operators | grep pipelines

# Then update env/default/default.properties accordingly, e.g.:
OPERATOR_VERSION = 0.79
OSP_VERSION = 1.22
PIPELINE_VERSION = v1.9
```

### 3 — Compile-check your changes

```bash
go build ./...
# or just the packages you changed:
go build ./pkg/operator/... ./pkg/diagnostics/... ./tests/olm/...
```

### 4a — Testing on a fresh cluster (operator not yet installed)

Use this path when running on a cluster where the operator has never been installed (mirrors CI exactly):

```bash
# Install the operator via OLM — runs the full ordered install sequence
# including the webhook readiness wait added in this PR
./scripts/run-tests.sh install

# Watch the install progress in another terminal:
oc get pods -n openshift-operators -w
oc get tektonconfig config -w

# Once install completes, run sanity tests to validate everything works
./scripts/run-tests.sh sanity
```

To simulate CI failure diagnostics: introduce a deliberate failure then inspect the JUnit report:

```bash
ARTIFACTS_DIR=./test-reports ./scripts/run-tests.sh install
# On failure, test-reports/junit-report.xml will contain an <operator-diagnostics>
# attachment with logs from openshift-operators pods
cat test-reports/junit-report.xml | grep -A 50 "operator-diagnostics"
```

### 4b — Testing on a cluster with the operator already installed

```bash
# Quick sanity pass — runs all sanity-labelled tests across all suites
./scripts/run-tests.sh sanity

# Run a specific suite
ginkgo run --timeout=30m ./tests/operator/
ginkgo run --timeout=30m ./tests/pipelines/
ginkgo run --timeout=30m ./tests/triggers/

# Run only the webhook-wait step in isolation
ginkgo run --label-filter=install --focus="waits for operator and pipelines webhook" \
    --timeout=5m ./tests/olm/

# Run a single named test by focusing on its description
ginkgo run --focus="validates RBAC" --timeout=5m ./tests/operator/
```

### 5 — Running with verbose output

```bash
# -v streams each spec name and GinkgoWriter output as it runs
ginkgo run -v --timeout=30m ./tests/operator/

# Show full failure detail including the cluster-diagnostics report entry
ginkgo run -v --label-filter=install --timeout=30m ./tests/olm/
```

### 6 — Running the full suite

```bash
# All feature tests — excludes install/upgrade/uninstall and disconnected
ginkgo run \
    --label-filter='!install && !upgrade && !uninstall && !disconnected' \
    --timeout=60m \
    ./tests/...
```

### 7 — Generating a JUnit report

```bash
ARTIFACTS_DIR=./test-reports ./scripts/run-tests.sh sanity
# report written to: ./test-reports/junit-report.xml
```

### 8 — Cleanup

The test framework automatically creates per-`Describe` namespaces and deletes them after each block (unless the block failed — failed namespaces are preserved for debugging).

To manually clean up any leftover test namespaces:

```bash
oc get projects | grep -E 'eco-|chains-|results-|triggers-|pipelines-' | awk '{print $1}' | xargs oc delete project
```

### Troubleshooting common local failures

| Symptom | Likely cause | Fix |
|---|---|---|
| `webhook not ready` / `connection refused` on patch | Operator webhook pod not up | The `WaitForOperatorWebhooks` step now handles this; if it still fails, check `oc get pods -n openshift-operators` |
| `TektonConfig CR not found` | OLM install slow | Increase `GINKGO_TIMEOUT`; check `oc get csv -n openshift-operators` |
| `namespace already exists` | Previous test run leaked namespaces | Run the manual cleanup command above |
| `oc: command not found` | `oc` not in PATH | Install from https://mirror.openshift.com/pub/openshift-v4/clients/ocp/ |
| Chains TC02 fails with auth error | `CHAINS_DOCKER_CONFIG_JSON` not set / wrong format | Must be raw JSON from `cat ~/.docker/config.json`, not base64 |

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
  tektonkueue/  #   Multi-cluster operator bootstrap
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
