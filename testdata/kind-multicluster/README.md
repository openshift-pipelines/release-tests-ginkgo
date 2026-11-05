# Real Kind admission smoke test

```sh
ARTIFACTS_DIR="$PWD/artifacts/kind-multicluster" bash scripts/kind-multicluster.sh
```

Requires Kind v0.31.0, Docker with Linux containers and cgroup v2, kubectl
compatible with Kubernetes 1.35, curl, jq, Go selected by `go.mod` (Go 1.26),
and sha256sum or shasum. Start with 8 CPUs and 12 GiB available to Docker for
two control planes and their controller stacks; this is not a measured minimum.

## Released components

| Component | Release |
| --- | --- |
| Kind / Kubernetes | v0.31.0 / v1.35.0 (node digest pinned in driver) |
| Tekton Operator | v0.80.0 |
| Tekton Pipelines | v1.12.0 (Operator payload) |
| tekton-kueue scheduler | v0.3.1 (Operator payload) |
| Kueue | v0.16.1 |
| cert-manager | v1.19.2 |

These are reused from PR #115 source `d3235eb8`. `artifacts.lock` verifies
complete release manifest bytes with SHA256. The Kustomize overlay pins
cert-manager and Kueue images by digest; the Operator release pins its image
and embedded payload. Installation checks the Pipelines version and both
deployed scheduler images. The Operator payload lacks `scheduler-info`, so its
scheduler status version is unknown and cannot verify the scheduler release.
Review compatibility before changing any pin.

The Operator's embedded scheduler image digest is not available from its
registry. The overlay sets `IMAGE_SCHEDULER_MANAGER` and
`IMAGE_SCHEDULER_WEBHOOK` on the lifecycle controller to the digest published in
the [scheduler v0.3.1 release](https://github.com/tektoncd/tekton-kueue/releases/download/v0.3.1/release-v0.3.1.yaml).
That image supports anonymous pulls on AMD64 and ARM64. This is an explicit pin,
not a fallback to a different scheduler version.

Operator autoinstall is disabled in the rendered manifest before deployment.
Individual `TektonPipeline` and `TektonScheduler` CRs install the components.
`TektonConfig` Hub mode is deliberately not used: the v0.80.0 Kubernetes
controller map does not register the SyncerService reconciler required by that
mode. Kueue's generic adapter copies PipelineRun status for this smoke test.
cert-manager supports the controller stack; there are no proxy CRs, proxy
certificates, proxy RBAC, or proxy log requests.

## Admission contract

Both clusters receive namespace `release-multicluster`, ClusterQueue
`release-multicluster`, and two distinct LocalQueues:
`release-multicluster` (scheduler default) and `release-multicluster-explicit`
(prelabelled scenario). Both LocalQueues refer to the same ClusterQueue.
The hub ClusterQueue requires the MultiKueue admission check selecting
`release-spoke`. Hub scheduler override is enabled; spoke override is disabled.

The Go suite uses `KIND_HUB_KUBECONFIG` and `KIND_SPOKE_KUBECONFIG` and calls
`kindmulticluster.Execute(ctx, hub, spoke, prelabelled)` for default and explicit
queue selection. It verifies hub admission, spoke-only execution, direct spoke
logs, and completion/status propagation. Its FIFO gate releases execution on
an observed event instead of a fixed sleep. The shell harness supplies real
controllers and credentials; command-boundary unit tests do not prove admission.

The worker credential uses a two-hour TokenRequest, replaces all admin user
credentials, and uses Kind's internal endpoint so hub containers can reach the
spoke even on macOS. Cluster-wide reads are limited to watched Workloads,
PipelineRuns, and Jobs (Kueue also registers its built-in Job adapter).
Writes are limited to Workloads and PipelineRuns in the test namespace.
The local suite uses explicit private admin kubeconfigs for direct observation.
Neither the user's kubeconfig nor current context is changed.

## Ownership, artifacts, and CI

The driver uses unique `rtg-*-hub` and `rtg-*-spoke` names and refuses existing
clusters before registering ownership. EXIT/INT/TERM traps export diagnostics
before deleting only owned clusters, including partial creations, then remove
the private `.kind-multicluster.*` credential directory. An existing primary
failure code survives diagnostic and cleanup errors; cleanup failure fails an
otherwise successful run. SIGKILL and host failure cannot run traps; inspect
and explicitly delete any remaining owned clusters in that case.

`ARTIFACTS_DIR` defaults to `artifacts/kind-multicluster`, outside the private
directory. It contains Ginkgo `junit.xml` (or a bootstrap-failure fallback),
event identities/reasons, bounded controller logs, and sanitized Kind `.log`
files from both clusters, on success and failure. Raw Kind exports remain
private. Configs, JSON exports, Secrets, kubeconfigs, and tokens are not copied.
Log filtering removes the worker token, JWTs, credential-bearing lines, and
PEM private keys. Do not upload the private directory or broaden the workflow's
artifact path to the workspace. Use an empty artifact directory for each run.

`.github/workflows/kind-multicluster.yaml` runs unit checks before the driver.
It supports PRs to `main` and `release-*`, manual dispatch, and pushes to
`test-multicluster-workload-scenarios` so a fork can validate before upstream
workflow approval. It needs only `contents: read`, downloads checksum-verified
Kind and kubectl, has a bounded 50-minute smoke step, always uploads only the
artifact directory, and cancels older runs for the same PR/ref.

## Checks without live clusters

```sh
for script in scripts/kind-multicluster.sh scripts/kind-multicluster/*.sh; do
  bash -n "$script" || exit
done
go test -race -shuffle=on -count=1 ./pkg/kindmulticluster
go test ./tests/kindmulticluster -ginkgo.dry-run
golangci-lint run ./pkg/kindmulticluster/... ./tests/kindmulticluster/...
actionlint .github/workflows/kind-multicluster.yaml # when installed
```

Lifecycle tests use isolated command fakes, never Docker or a Kubernetes API.
A live Kind run is still required to verify the released components together.
