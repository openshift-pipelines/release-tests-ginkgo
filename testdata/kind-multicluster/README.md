# Kind multicluster release drafts

This branch's scale story depends on approved one-spoke parent commit
`3af9d6a41ee55c7015575a87aa5f7cce30c1be32`. It reuses that pinned stack, Kubernetes
Operator component CRs, bounded tokens, TLS verification, and private proxy
transport. The one-spoke default remains available.

Run from the repository root:

```sh
bash scripts/kind-multicluster.sh
# Explicit equivalent:
bash scripts/kind-multicluster.sh --spokes=1
# Scale story: exactly one hub, two spokes, and ten hub PipelineRuns:
bash scripts/kind-multicluster.sh --spokes=2
```

Requires Kind **v0.31.0**, a running Docker daemon using Linux containers and
cgroup v2, kubectl compatible with Kubernetes 1.35, curl, jq, and the Go toolchain
selected by `go.mod` (1.26.4). Allocate enough Docker resources for two control
planes and both controller stacks; 8 CPUs and 12 GiB is a starting point, not a
measured minimum. ARM64 and AMD64 images are published.

The script creates two or three uniquely named, script-owned clusters. It never changes
the user's kubeconfig or current context. An EXIT/INT/TERM cleanup trap is
registered before downloads and provisioning; it deletes only clusters this
invocation attempted to create, including partially created clusters, and removes
its private `.kind-multicluster.*` directory. Cleanup failures fail the run.
SIGKILL and host failure cannot run a shell trap; any remaining `rtg-*-hub` or
`rtg-*-spoke` cluster must be inspected and deleted explicitly. Credentials and
temporary test binaries are kept in the private directory, not in test output.
The empty ownership-array guard works on PATH Bash and stock macOS Bash 3.2.
The selected suite label is fail-on-empty, so a misspelled or missing scenario
cannot silently pass.

## Two-spoke quota-ten story

The two-spoke option installs the same components on three clusters. Each spoke
gets its own internal Kind endpoint, bounded worker token, hub Secret, and
MultiKueueCluster (`release-spoke-1` and `release-spoke-2`). The common
MultiKueueConfig lists both. Every operational kubectl call uses an explicit
kubeconfig. `quota-ten.yaml` patches the existing queue on the hub and both
spokes, without adding extra quota-bearing queues.

The Go test reads all three ClusterQueues from the API before submission and
after completion and requires exactly one `tekton.dev/pipelineruns` nominal quota
entry equal to **10**. This is a resource quota, not a promise of ten simultaneous
executions per spoke. The pinned integration can temporarily charge both the
copied MultiKueue Workload and the PipelineRun-owned Workload. The remaining eight
runs are submitted as a batch and verified concurrently so a waiting run cannot
prevent another admitted run from being released.

Both-spoke coverage does not depend on scheduling fairness:

1. Set spoke B's ClusterQueue `stopPolicy: Hold` and wait for `Active=False`,
   reason `Stopped`, acknowledging the updated generation. Confirm A is active.
   Submit run 1 and require placement on A, a live hub-proxy marker, matching
   successful hub/spoke PipelineRun status, and a finished hub Workload.
2. Hold A, enable B, and await both controller acknowledgements. Submit and
   finish run 2 on B with the same live-log and status proof.
3. Restore both queues to `None` and await `Active=True` at their current
   generations. Submit the remaining eight runs. Each must execute on exactly
   one spoke and return its successful TaskRun reference to the hub.

These are supported [Kueue v0.16.1 ClusterQueue stop policies](https://github.com/kubernetes-sigs/kueue/blob/v0.16.1/apis/kueue/v1beta2/clusterqueue_types.go),
not Workload status patches or invented placement annotations. `Hold` prevents
new reservations; no scheduler restart, delay, or fairness assumption is involved.

Each of the ten runs owns separate UID, Workload, placement, status, and watch
state. Hub PipelineRuns, hub TaskRuns/pods, both spokes' PipelineRuns/pods, and
hub Workloads are subscribed before each create. Watches resume from listed
resource versions and remain active through batch verification. Duplicate worker
execution, local hub execution, replacement identities, failures, and mismatched
returned status fail the scenario. Only the first two representative runs open
WebSockets; all ten retain execution/status assertions.
The shared transport also accepts expected peer-close errors during teardown:
the proxy may close its completed TLS stream before the client sends its final
close notification. Unexpected close errors and all receive errors still fail.
A deterministic transport regression covers those cases.

Cleanup is registered before the first eligibility change or create. It attempts
every recorded hub run and observed worker run, using API UID delete preconditions.
A partial batch failure cannot overwrite earlier run state or delete a same-name
replacement; one cleanup conflict does not prevent later deletes. Worker queues
are restored to eligible using their original UIDs. Cluster teardown is still the
script's final cleanup, including interrupted or partial provisioning.

## Exact component set and provenance

| Component | Pin | Evidence |
| --- | --- | --- |
| Kind / Kubernetes | v0.31.0 / v1.35.0, node SHA256 in script | [Kind release node list](https://github.com/kubernetes-sigs/kind/releases/tag/v0.31.0) |
| Tekton Operator | v0.80.0 | `go.mod`, release manifest SHA256 in `artifacts.lock` |
| Tekton Pipelines | v1.12.0 | [Operator component payload](https://github.com/tektoncd/operator/blob/v0.80.0/components.yaml) |
| tekton-kueue | v0.3.1 | Same Operator payload; [module](https://github.com/tektoncd/tekton-kueue/blob/v0.3.1/go.mod) selects Kueue v0.16.1 |
| Multicluster proxy | v0.1.1 | Same Operator payload |
| Kueue | v0.16.1 | tekton-kueue module and `hack/get-kueue-version.sh`; release manifest SHA256 |
| cert-manager | v1.19.2 | [tekton-kueue Makefile](https://github.com/tektoncd/tekton-kueue/blob/v0.3.1/Makefile) |

`artifacts.lock` hashes complete release YAML bytes. The Kustomize overlay pins
cert-manager and Kueue container images by digest, and the Operator release
already pins its image and embedded component payload. The script asserts the
installed Pipelines, scheduler, and proxy versions. Do not replace this set with
`latest`, a gist, or a different scheduler image without reviewing compatibility.
The audited `openshift-pipelines/multi-cluster` script at
`4302b99f7e519405e40acaf333533344b39be141` was used only as a reference.

### Why standalone Operator CRs?

This draft installs `TektonPipeline`, `TektonScheduler`, and
`TektonMulticlusterProxyAAE` through their Kubernetes Operator controllers, with
Operator autoinstall disabled **before** deployment. It does not use
`TektonConfig` Hub mode: v0.80.0's shared TektonConfig reconciler requires a ready
`SyncerService` for a Hub, but its Kubernetes controller map does not register a
SyncerService reconciler. The generic Kueue adapter copies PipelineRun status, so
that separate results-sync component is unnecessary for this proof. Full
TektonConfig Hub lifecycle coverage requires an upstream Kubernetes SyncerService
contract and is not claimed by this test.

The proxy release expects an OpenShift serving-certificate Secret. On Kind the
fixture supplies that Secret using cert-manager, then verifies the certificate
against its explicitly loaded trust anchor and service DNS name. TLS verification
is never disabled.

## What the one-spoke test proves

1. Both clusters have pinned Pipelines, Kueue, and tekton-kueue. The hub has a
   MultiKueue admission check and a worker credential containing only a bounded
   service-account token, with writes limited to the test namespace. Cluster-wide
   reads cover Workloads, PipelineRuns, and Jobs watched by MultiKueue (Kueue
   v0.16.1 enables the built-in Job adapter even for an empty framework list).
2. A hub PipelineRun with `managedBy: kueue.x-k8s.io/multikueue` creates a Workload
   selecting `release-spoke`. Its independent spoke PipelineRun creates a running
   execution pod. No matching TaskRun or pod executes on the hub.
3. The workload prints a ready line and blocks on a FIFO, not a fixed sleep. The
   verifier creates a ten-minute hub TokenRequest with read-only log RBAC and
   opens a TLS WebSocket through a loopback-only, dynamically allocated hub proxy
   port-forward. Only after receiving the ready line does it write an unpredictable
   marker to the spoke FIFO. Receiving that marker over the hub WebSocket proves
   live streaming, rather than a direct spoke log read or historical log fetch.
4. Completion watches are registered before FIFO release. Both the spoke and hub
   must report `Succeeded=True`, with the same spoke TaskRun child reference in
   the hub status; an early failure or deletion fails the test.
   Watching the spoke before release avoids racing MultiKueue's worker cleanup.

No `oc`, OLM, console, browser, or Playwright is involved. Existing explicit
kubeconfig client construction is reused. Existing OpenShift-only setup helpers
and the fixed-sleep/direct-spoke-log workload helper are intentionally not reused.
The private transport is `pkg/multicluster/proxy.go`; the reusable behavioral
entry point is `multicluster.VerifyLiveLogs`. It knows nothing about Kind.
Token issuance and port-forward lifecycle remain in `pkg/kindmulticluster`.

## Local checks without clusters

```sh
for script in scripts/kind-multicluster.sh scripts/kind-multicluster/*.sh; do
  /bin/bash -n "$script" || exit
done
gofmt -l pkg/kindmulticluster pkg/multicluster/{proxy,logs,logs_test}.go tests/kindmulticluster
go test -race -shuffle=on -count=1 ./pkg/kindmulticluster ./pkg/multicluster
go test -run '^$' ./...
go test ./tests/kindmulticluster -args \
  -ginkgo.dry-run -ginkgo.label-filter=kind-two-spoke -ginkgo.fail-on-empty
go vet ./...
go build ./...
go mod tidy -diff
golangci-lint run --allow-serial-runners ./...
bash scripts/kind-multicluster/check-manifests.sh
```

The TLS/WebSocket tests use real loopback servers and event-triggered cancellation.
The shell lifecycle tests use command-boundary fakes and verify failure cleanup,
including refusal to delete unrelated clusters; they do not claim real installation
coverage. A successful compile or unit run does not replace the Docker/Kind run.

### Draft validation

The parent draft's offline checks passed; the scale story adds API-backed quota
and ownership tests, mixed-worker/duplicate/failure observations, subscription
ordering, and both topology lifecycle paths on both shells. Offline checks do
not establish real scheduling or proxy behavior. Live end-to-end execution has
not been verified: this task prohibits real cluster creation, and the workstation
has Kind v0.22.0 rather than v0.31.0 and kubectl v1.32.1 rather than a client
compatible with Kubernetes 1.35. The parent recorded a stopped Docker daemon;
the current read-only Docker probe now succeeds with server 27.3.1. That does not
provide live three-cluster evidence. ShellCheck and Bash/YAML language servers
are not installed.
