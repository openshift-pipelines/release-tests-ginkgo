# Pipelines-in-Pipelines backend test coverage

This is the scenario and assertion inventory for the backend suite. A test
being implemented or selected by a dry-run is not evidence that it executed
successfully on a release build.

## Coverage matrix

The row numbers retain the supplied gap analysis ordering. Rows 8 and 30 refer
to the same test, not two independent scenarios.

| Row | Behavior | Automated check |
|---|---|---|
| 1 | Inline child spec, parameters, metadata and exact descendant set | PIPELINES-44-TC01 |
| 2 | Named child and service-account inheritance into the actual owned Pod | PIPELINES-44-TC02 |
| 3 | Sibling inputs, grandchild ownership and dependent join ordering | PIPELINES-44-TC03 |
| 4 | Shared PVC and exact parent-child-parent byte round trip | PIPELINES-44-TC04 |
| 5 | False/true conditions and ordinary continuation | PIPELINES-44-TC05 |
| 6 | Finally child after successful main work | PIPELINES-44-TC06 |
| 7 | Failed child, original exit code and absent dependent execution | PIPELINES-45-TC01 |
| 8 | Missing child diagnostic, parent-only run set and zero TaskRuns | PIPELINES-45-TC02 |
| 9 | Direct self-cycle rejection without descendants | PIPELINES-45-TC03 |
| 10 | Missing required workspace fails child and parent | PIPELINES-45-TC04 |
| 11 | Parent-main failure survives successful child-local cleanup | PIPELINES-45-TC05 |
| 12 | Child-main failure survives cleanup and prevents dependent execution | PIPELINES-45-TC06 |
| 13 | Same-namespace cluster resolution and execution identity | PIPELINES-47-TC01 |
| 14 | Cross-namespace allow, block-over-allow and unlisted denial | PIPELINES-47-TC02 through TC04 |
| 15 | Child Pipeline parameter default when the parent omits a value | PIPELINES-48-TC01 |
| 16 | Parent override differs from and wins over child/leaf defaults | PIPELINES-48-TC02 |
| 17 | Array/object boundaries preserve spaces, tabs and empty strings | PIPELINES-48-TC03 |
| 18 | Child-local result producer orders and supplies its consumer | PIPELINES-48-TC04 |
| 19 | Unsupported parent consumption of a child Pipeline result is rejected | PIPELINES-49-TC01 |
| 20 | Parent timeout reports the current unsupported-cascade limitation | PIPELINES-50-TC01; see limitations below |
| 21 | Parent cancellation reports the current unsupported-cascade limitation | PIPELINES-50-TC02; see limitations below |
| 22 | Nested onError continue retains the failure but permits continuation | PIPELINES-49-TC04 |
| 23 | Alpha-off rejection for named/inline nesting with successful ordinary controls | PIPELINES-49-TC02 and TC03 |
| 24 | Indirect A-to-B-to-A cycle is diagnosed on B without another A run | PIPELINES-49-TC05 |
| 25 | Ordinary resolver regression on the same build | Existing PIPELINES-17, 23, 24, 25 and 31 cases; regression label below |
| 26 | Ordinary inline run has only its own TaskRun and no child PipelineRuns | Existing PIPELINES-03-TC07 |
| 27 | Closed/expired completion watches relist and resume within the original deadline | TestResumeNestedPipelineRun and TestResumeNestedPipelineRunRejectsFatalFailures |
| 28 | An actual assertion failure stops live descendants while retaining diagnostics | PIPELINES-51-TC01 and its isolated failure probe |
| 29 | Resolver/leaf authorization separation and actual leaf Pod identity | PIPELINES-47 success cases; named inheritance also checked by PIPELINES-44-TC02 |
| 30 | Missing-child full match set and existing-child control | Same PIPELINES-45-TC02 as row 8; PIPELINES-44-TC02 is the positive control |

Sibling dependency ordering does not establish that their execution overlaps.
Service-account names and SubjectAccessReviews do not establish broader
in-Pod workload authorization.

## Current implementation limitations

The contract baseline is Tekton Pipelines v1.14.0, source revision
`8afd0f7d5aae3504267eb42bf342847911b7b4d1`. Record the deployed controller and
webhook images separately: the Go client dependency does not identify those
images or prove downstream release support.

* Parent consumption of a nested Pipeline's results is unsupported. The webhook
  rejects the reference with its child-result propagation diagnostic.
* Parent timeout and cancellation do not cascade to child PipelineRuns in this
  implementation. Its child-kind dispatch supports TaskRuns and CustomRuns,
  not nested PipelineRuns. The parent remains `Succeeded=Unknown` with reason
  `PipelineRunCouldntTimeOut` or `PipelineRunCouldntCancel`; the leaf stays active.
  PIPELINES-50 verifies this limitation, not successful cascade support.
* The lifecycle fixture sets a three-minute parent timeout. Its leaf must become
  Running within the two-minute startup deadline, and child/TaskRun defaults
  must allow it to outlive the parent deadline. These cases watch API events;
  they do not use sleeps to coordinate the test.
* Cleanup explicitly stops the owned TaskRuns and observes their Pods reaching
  terminal state or deletion. It does not rely on the unsupported parent
  cascade. Failed per-case namespaces and diagnostic resources are preserved.

Relevant upstream source:

* [Child-result rejection](https://github.com/tektoncd/pipeline/blob/8afd0f7d5aae3504267eb42bf342847911b7b4d1/pkg/apis/pipeline/v1/pipeline_validation.go#L752-L787)
* [Cancellation and child-kind dispatch](https://github.com/tektoncd/pipeline/blob/8afd0f7d5aae3504267eb42bf342847911b7b4d1/pkg/reconciler/pipelinerun/cancel.go#L110-L208)
* [Timeout behavior](https://github.com/tektoncd/pipeline/blob/8afd0f7d5aae3504267eb42bf342847911b7b4d1/pkg/reconciler/pipelinerun/timeout.go#L75-L103)

Update the contract and these tests together when the target implementation
adds support. Do not reinterpret a passing limitation test as feature support.

## Running the validation set

Use a dedicated cluster. Besides the requirements in the root README, these
tests temporarily change alpha/inline-spec flags. Resolver policy cases also
change the allowed/blocked namespaces and replace resolver Pods before restoring
the original configuration. Other resolution requests can be affected.

Run the nested scenarios:

```bash
go run github.com/onsi/ginkgo/v2/ginkgo run \
  --label-filter=pipelines-in-pipelines --timeout=90m \
  --junit-report=pipelines-in-pipelines.xml ./tests/pipelines/ -- \
  --context=my-context
```

Include the existing ordinary controls and resolver regressions on that same
build:

```bash
go run github.com/onsi/ginkgo/v2/ginkgo run \
  --label-filter='pipelines-in-pipelines || pipelines-in-pipelines-regression' \
  --timeout=120m --junit-report=pipelines-in-pipelines-regression.xml \
  ./tests/pipelines/ -- --context=my-context
```

The ordinary resolver fixtures retain their existing endpoint, token, registry,
storage and policy prerequisites. The private Git case requires `GITHUB_TOKEN`.
A skipped case is not executed coverage. Retain the actual resolved revisions,
bundle digests, endpoints, namespace policies and image digests with the report;
selection alone does not pin mutable external inputs.

PIPELINES-51 launches the same test binary in an isolated process, induces one
expected failure after a leaf Pod is Running, and verifies the real failure hook
stopped the workload without deleting its diagnostics. The outer test requires
both the failure marker and the stopped resources. The internal probe is not a
separate normal-suite case. A sentinel also verifies that the child process
does not remove its parent's repository scratch files. After successful verification the outer test deletes
its probe namespace; a failed verification preserves it.

Local checks without a cluster:

```bash
go test -race -shuffle=on -count=1 ./pkg/pipelines/
go test -run '^$' ./tests/pipelines/
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4 run --timeout=10m
go run github.com/onsi/ginkgo/v2/ginkgo run --dry-run \
  --label-filter='pipelines-in-pipelines || pipelines-in-pipelines-regression' \
  ./tests/pipelines/
```

For release sign-off, retain the exact operator/controller/webhook versions and
JUnit report showing each required selected case ran without failures, errors
or skips. Compilation, unit tests and dry-runs do not replace that evidence.
Matrix fan-out and inherited timeout/retry budgets are not included in this inventory.
