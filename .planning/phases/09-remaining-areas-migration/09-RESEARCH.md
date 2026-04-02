# Phase 9 Research: Remaining Areas Migration

**Phase:** 09-remaining-areas-migration
**Researched:** 2026-03-31
**Confidence:** HIGH

## Overview

Phase 9 migrates 7 small test areas (chains, results, MAG, metrics, versions, OLM, console) comprising ~13 tests total. These are the remaining Gauge specs not covered by Phases 4-8. Each area has 1-3 tests and existing `suite_test.go` scaffolding from Phase 2.

## Test Inventory

| Area | Spec File | Tests | Polarion IDs | Tags | Key Patterns |
|------|-----------|-------|--------------|------|--------------|
| Chains | chains/chains.spec | 2 | PIPELINES-27-TC01, TC02 | chains, e2e, sanity | Ordered (TektonConfig update, cosign verify) |
| Results | results/results.spec | 2 | PIPELINES-26-TC01, TC02 | results, e2e, sanity | Eventually (annotation polling), Ordered (apply, verify, check) |
| MAG | manualapprovalgate/manual-approval-gate.spec | 2 | PIPELINES-28-TC01, TC02 | approvalgate, e2e, sanity | Ordered (start pipeline, approve/reject, verify) |
| Metrics | metrics/metrics.spec | 1 | PIPELINES-01-TC01 | e2e, metrics, admin, sanity | DescribeTable (job health checks), Eventually (Prometheus polling) |
| Versions | versions.spec | 2 | PIPELINES-22-TC01, TC02 | e2e, sanity | Env-var-based Skip, CLI execution |
| OLM | olm.spec | 3 | PIPELINES-09-TC01, TC02, TC03 | install, upgrade, uninstall, admin, sanity | Serial+Ordered (cluster-wide mutations), DeferCleanup |
| Console | icon.spec | 1 | PIPELINES-08-TC01 | icon, console, manualonly | Skip (manualonly -- not automatable) |

**Total: 13 tests** (12 automatable + 1 manual-only console icon test)

## Area Analysis

### 1. Chains (2 tests) -- MISC-01

**Gauge spec:** `specs/chains/chains.spec`
**Reference helpers:** `pkg/operator/tektonchains.go`

**TC01 (TaskRun signature):** Ordered workflow:
1. Update TektonConfig (taskrun format=in-toto, storage=tekton, oci="", transparency=false)
2. Store cosign public key from signing-secrets
3. Apply `testdata/chains/task-output-image.yaml`
4. Verify taskrun signature (get UID, decode base64 signature, cosign verify-blob-attestation)

**TC02 (Image signature + provenance):** Ordered workflow:
1. Update TektonConfig (taskrun format=in-toto, storage=oci, oci storage=oci, transparency=true)
2. Store cosign public key
3. Verify CHAINS_REPOSITORY env var exists (Skip if not)
4. Create image registry secret
5. Apply PVC and kaniko task YAMLs
6. Start kaniko-chains task
7. Verify image signature (cosign verify)
8. Check attestation exists (rekor-cli search)
9. Verify attestation (cosign verify-attestation)

**Key dependencies:** `cosign` and `rekor-cli` binaries, CHAINS_REPOSITORY env var, image registry credentials
**Ginkgo patterns:** `Ordered` container for both tests, `Skip` for missing env vars, `DeferCleanup` to restore TektonConfig
**Helper migration:** `pkg/operator/tektonchains.go` uses `testsuit.T.Errorf` (Gauge) -- must replace with Gomega assertions

### 2. Results (2 tests) -- MISC-02

**Gauge spec:** `specs/results/results.spec`
**Reference helpers:** `pkg/operator/tektonresults.go`

**TC01 (TaskRun results):** Ordered workflow:
1. Verify golang imagestream exists
2. Apply `testdata/results/taskrun.yaml`
3. Verify taskrun completes successfully (Eventually polling)
4. Verify results annotation `results.tekton.dev/stored` is true (Eventually polling)
5. Verify results records via `opc results records get`
6. Verify results logs via `opc results logs get`

**TC02 (PipelineRun results):** Same flow with pipeline.yaml + pipelinerun.yaml

**Key dependencies:** Tekton Results configured with Loki, Results route available
**Ginkgo patterns:** `Ordered` container, `Eventually` for annotation polling, `DescribeTable` potential (TC01/TC02 differ only by resource type)
**Helper migration:** `pkg/operator/tektonresults.go` uses `store.Clients()` global state and `testsuit.T.Fail` -- must refactor to accept `*clients.Clients` parameter and use Gomega

### 3. Manual Approval Gate (2 tests) -- MISC-03

**Gauge spec:** `specs/manualapprovalgate/manual-approval-gate.spec`
**Reference helpers:** `pkg/manualapprovalgate/manualapprovalgate.go`

**TC01 (Approve):** Ordered workflow:
1. Validate MAG deployment
2. Create manual-approval-pipeline
3. Start pipeline with workspace
4. Approve the approval task (`opc approvaltask approve`)
5. Validate pipeline for "Approved" state
6. Verify latest pipelinerun "successful"

**TC02 (Reject):** Same flow but reject instead of approve:
1. Create pipeline
2. Start pipeline
3. Reject approval task (`opc approvaltask reject`)
4. Validate pipeline for "Rejected" state
5. Verify latest pipelinerun "failed"

**Key dependencies:** ManualApprovalGate CR deployed (from OLM install)
**Ginkgo patterns:** `Ordered` container, `Eventually` for pipeline status polling
**Helper migration:** `pkg/manualapprovalgate/manualapprovalgate.go` uses `store.Clients()` -- must refactor to accept clients parameter

### 4. Metrics (1 test) -- MISC-04

**Gauge spec:** `specs/metrics/metrics.spec`
**Reference helpers:** `pkg/monitoring/prometheus.go`

**TC01 (Monitoring acceptance):** Two-part test:
1. Verify job health status metrics for 6 services via Prometheus API (DescribeTable candidate)
2. Verify 15 pipelines controlPlane metrics exist (all tekton_* metrics)

**Key dependencies:** Prometheus accessible via OpenShift monitoring route, bearer token from prometheus-k8s SA
**Ginkgo patterns:** `DescribeTable` for job health checks, `Eventually` for Prometheus polling
**Helper migration:** `pkg/monitoring/prometheus.go` imports old Gauge repo paths, uses `wait.PollUntilContextTimeout` (can keep) but log.Printf should become GinkgoWriter

### 5. Versions (2 tests) -- MISC-05

**Gauge spec:** `specs/versions.spec`
**Reference helpers:** `pkg/opc/opc.go` (AssertComponentVersion, AssertClientVersion, AssertServerVersion, DownloadCLIFromCluster)

**TC01 (Server-side versions):** Check versions of 9 components:
- pipeline, triggers, operator, chains, pac, hub, results, manual-approval-gate, OSP
- Uses env vars: PIPELINE_VERSION, TRIGGERS_VERSION, etc.

**TC02 (Client versions):** Ordered workflow:
1. Download and extract CLI from cluster (`consoleclidownloads` resource)
2. Check tkn client version
3. Check tkn-pac version
4. Check opc client version
5. Check opc server version

**Key dependencies:** Environment variables for expected versions, consoleclidownloads resource available
**Ginkgo patterns:** `DescribeTable` for TC01 (9 identical checks, different component), `Ordered` for TC02 (download first), `Skip` if version env vars not set
**Helper migration:** `pkg/opc/opc.go` uses `testsuit.T.Errorf` -- must replace with Gomega assertions

### 6. OLM (3 tests) -- MISC-06

**Gauge spec:** `specs/olm.spec`
**Reference helpers:** `pkg/olm/subscription.go`

**TC01 (Install):** Complex Ordered workflow (~25 steps):
- Subscribe to operator, wait for TektonConfig, configure features, validate deployments
- This is essentially the cluster setup test -- most comprehensive single test

**TC02 (Upgrade):** Ordered workflow:
- Upgrade operator subscription channel, wait for TektonConfig, validate

**TC03 (Uninstall):** Single step:
- Uninstall operator

**Key dependencies:** OLM available, operator not pre-installed (or specific version installed for upgrade)
**Ginkgo patterns:** `Serial` + `Ordered` (cluster-wide mutations), `DeferCleanup` for operator cleanup
**Critical note:** OLM tests modify cluster-wide state and must NOT run in parallel with any other tests. The install test has ~25 sequential steps and is the most complex single test in the suite.
**Helper migration:** `pkg/olm/subscription.go` uses `testsuit.T.Errorf` and `testsuit.T.Fail` from Gauge -- must replace with Gomega. Also imports template system for subscription creation.

### 7. Console Icon (1 test) -- MISC-07

**Gauge spec:** `specs/icon.spec`
**Tags:** `icon, console, manualonly`

**TC01 (Icon verification):** Manual-only test:
- Login to OpenShift console
- Navigate to OperatorHub
- Search for OpenShift Pipelines Operator
- Verify icon displays correctly

**Key note:** This test is tagged `manualonly` -- it requires browser interaction and cannot be automated in a headless E2E suite. In Ginkgo, implement as a `PIt` (pending) or `It` with `Skip("manual verification only")` so it appears in test inventory but does not execute.

## Existing Scaffolding

All 6 test area directories have `suite_test.go` from Phase 2:
- `tests/chains/suite_test.go` -- package chains_test, Label("chains")
- `tests/results/suite_test.go` -- package results_test, Label("results")
- `tests/mag/suite_test.go` -- package mag_test, Label("mag")
- `tests/metrics/suite_test.go` -- package metrics_test, Label("metrics")
- `tests/versions/suite_test.go` -- package versions_test, Label("versions")
- `tests/olm/suite_test.go` -- package olm_test, Label("olm")

The `tests/versions/` directory also has `versions_patterns_test.go` (Skip pattern reference from Phase 2).

No `tests/console/` directory exists -- console icon test needs a home. Options:
1. Add to `tests/olm/` (icon is operator-related, tagged `console`)
2. Create `tests/console/` with its own suite -- unnecessary overhead for 1 manual-only test
3. Add to `tests/operator/` -- icon verification is operator-scoped

**Recommendation:** Place in `tests/operator/` as a separate `console_test.go` file with `Label("console", "manualonly")`. No need for a dedicated suite for a single manual-only test.

## Helper Migration Strategy

All reference helpers use Gauge's `testsuit.T.Errorf/Fail` pattern. Migration approach:

1. **Copy helper files** from reference-repo to local `pkg/` (new subdirectories as needed)
2. **Replace imports:** `github.com/openshift-pipelines/release-tests/pkg/*` with `github.com/openshift-pipelines/release-tests-ginkgo/pkg/*`
3. **Remove Gauge dependency:** Replace `testsuit.T.Errorf(msg)` with `return fmt.Errorf(msg)` -- let callers use Gomega `Expect(err).NotTo(HaveOccurred())`
4. **Refactor global state:** Replace `store.Clients()` calls with explicit `*clients.Clients` parameter
5. **Keep wait.PollUntilContextTimeout:** It works fine without Gauge -- already uses standard k8s.io/apimachinery

## TestData Files Needed

From reference repo `testdata/`:
- `testdata/chains/task-output-image.yaml`
- `testdata/chains/kaniko.yaml`
- `testdata/pvc/chains-pvc.yaml`
- `testdata/results/taskrun.yaml`
- `testdata/results/pipeline.yaml`
- `testdata/results/pipelinerun.yaml`
- `testdata/manualapprovalgate/manual-approval-gate.yaml`
- `testdata/manualapprovalgate/manual-approval-pipeline.yaml`

These must be copied from the reference repo into the Ginkgo repo's testdata directory.

## Dependency Analysis

| Area | Depends On | Cluster-Wide? | Serial? |
|------|------------|---------------|---------|
| Chains | TektonConfig (modifies it), cosign, rekor-cli | Yes (TektonConfig) | Yes |
| Results | Tekton Results configured, Results route | No (read-only after setup) | No |
| MAG | ManualApprovalGate deployed | No (creates own pipelines) | No |
| Metrics | Prometheus/monitoring stack | No (read-only) | No |
| Versions | Version env vars | No (read-only) | No |
| OLM | OLM, operator subscription | Yes (install/upgrade/uninstall) | Yes |
| Console | Browser access | N/A (manual) | N/A |

## Plan Decomposition

**Wave 1 (parallel):**
- Plan 09-01: Chains + Results migration (both use Ordered workflows, require testdata, modify/query Tekton state)
- Plan 09-02: MAG + Metrics migration (both independent, smaller scope)
- Plan 09-03: Versions + OLM + Console migration (versions is read-only, OLM is Serial, console is manual-only Skip)

Three plans in Wave 1 with no cross-plan file dependencies. Each plan: 2-3 tasks, 2-3 test files each.

## Sources

- Reference specs: `reference-repo/release-tests/specs/{chains,results,manualapprovalgate,metrics,olm}/` and `versions.spec`, `icon.spec`
- Reference helpers: `reference-repo/release-tests/pkg/{operator/tektonchains.go,operator/tektonresults.go,manualapprovalgate/,monitoring/,olm/,opc/}`
- Existing scaffolding: `tests/{chains,results,mag,metrics,versions,olm}/suite_test.go`
- Phase 2 patterns: `tests/versions/versions_patterns_test.go`, `tests/ecosystem/ecosystem_patterns_test.go`
