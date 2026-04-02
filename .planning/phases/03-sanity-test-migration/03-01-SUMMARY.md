---
phase: 03-sanity-test-migration
plan: 01
subsystem: testing
tags: [ginkgo, tekton, pipelinerun, taskrun, polarion, sanity]

# Dependency graph
requires:
  - phase: 02-suite-scaffolding
    provides: suite_test.go entry points with BeforeSuite/AfterSuite and sharedClients
provides:
  - pkg/wait package with WaitForPipelineRunState, WaitForTaskRunState, and condition accessors
  - pkg/pipelines package with ValidatePipelineRun dispatcher and ValidateTaskRun
  - pkg/k8s package with GetWarningEvents and dynamic resource Watch
  - 9 sanity test specs across versions and pipelines suites with Polarion IDs
  - 8 testdata YAML fixtures for pipeline sanity tests
affects: [04-ecosystem, 05-triggers, 06-operator, 07-pipelines-core, 08-pac, 09-remaining]

# Tech tracking
tech-stack:
  added: [knative.dev/pkg/apis (ConditionAccessor), k8s.io/apimachinery/pkg/util/wait]
  patterns: [ValidatePipelineRun dispatcher, ConditionAccessorFn polling, DeferCleanup namespace teardown, uniqueNS counter]

key-files:
  created:
    - pkg/wait/wait.go
    - pkg/pipelines/pipelines.go
    - pkg/pipelines/taskrun.go
    - pkg/k8s/k8s.go
    - tests/versions/versions_test.go
    - tests/pipelines/pipelines_sanity_test.go
  modified: []

key-decisions:
  - "Replaced tektoncd/cli log retrieval with oc CLI approach to avoid adding heavy dependency"
  - "Used GinkgoParallelProcess-based counter for unique namespace names instead of UUID"
  - "Simplified getPipelinerunLogs to use oc logs --selector instead of tektoncd CLI pkg"

patterns-established:
  - "ValidatePipelineRun(clients, name, status, ns) dispatches to success/failed/timeout/cancel validation"
  - "DeferCleanup(func() { oc.DeleteProjectIgnoreErrors(ns) }) for namespace teardown in every test"
  - "All Describe blocks have Label('sanity') for --label-filter=sanity selection"
  - "Polarion test case ID in every It() description string"

requirements-completed: [SNTY-01, SNTY-03]

# Metrics
duration: 5min
completed: 2026-04-02
---

# Phase 3 Plan 01: Sanity Test Migration Summary

**Ported wait/pipelines/k8s helper packages and created 9 sanity test specs with Polarion IDs across versions and pipelines suites**

## Performance

- **Duration:** 5 min
- **Started:** 2026-04-02T11:23:33Z
- **Completed:** 2026-04-02T11:28:33Z
- **Tasks:** 3
- **Files modified:** 14

## Accomplishments
- Ported pkg/wait with all condition functions (Succeed, Failed, FailedWithReason, FailedWithMessage, Running) and wait functions (WaitForPipelineRunState, WaitForTaskRunState, WaitForDeploymentState, WaitForPodState)
- Ported pkg/pipelines with ValidatePipelineRun dispatcher (success/failed/timeout/cancel) and ValidateTaskRun
- Created 9 sanity tests: 2 versions tests (PIPELINES-22-TC01, TC02) and 7 pipeline tests (PIPELINES-03-TC04, TC05, TC06, TC08, PIPELINES-02-TC01, PIPELINES-25-TC02, PIPELINES-24-TC01)
- All tests have Label("sanity") and pipeline tests use DeferCleanup for namespace teardown

## Task Commits

Each task was committed atomically:

1. **Task 1: Port pkg/wait and pkg/pipelines from reference repo** - `70609bc` (feat)
2. **Task 2: Create 9 sanity test specs with Polarion IDs and DeferCleanup** - `02d0200` (feat)
3. **Task 3: Copy testdata YAML fixtures from reference repo and verify full compilation** - `e87901b` (feat)

## Files Created/Modified
- `pkg/wait/wait.go` - Polling functions for PipelineRun/TaskRun/Deployment/Pod state transitions
- `pkg/pipelines/pipelines.go` - ValidatePipelineRun dispatching to success/failed/timeout/cancel validation
- `pkg/pipelines/taskrun.go` - ValidateTaskRun with name matching and status validation
- `pkg/k8s/k8s.go` - GetWarningEvents, Watch, GetGroupVersionResource for diagnostics
- `tests/versions/versions_test.go` - 2 versions sanity tests with Polarion IDs
- `tests/pipelines/pipelines_sanity_test.go` - 7 pipeline sanity tests with Polarion IDs
- `testdata/v1beta1/pipelinerun/*.yaml` - 4 pipelinerun YAML fixtures
- `testdata/pvc/pvc.yaml` - PVC fixture for cancel test
- `testdata/negative/v1beta1/pipelinerun.yaml` - Negative test fixture
- `testdata/resolvers/pipelineruns/*.yaml` - 2 resolver test fixtures

## Decisions Made
- Replaced tektoncd/cli dependency for log retrieval with `oc logs --selector` approach -- avoids adding a heavy Go dependency just for log streaming
- Used `GinkgoParallelProcess()` with a per-file counter for unique namespace names instead of UUID -- simpler and deterministic
- Kept `cast2pipelinerun` and `prGroupResource` in pipelines.go for future WatchForPipelineRun usage even though not yet called by sanity tests

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Replaced tektoncd/cli with oc CLI for log retrieval**
- **Found during:** Task 1 (Port pkg/pipelines)
- **Issue:** tektoncd/cli was not in go.mod and adding it would pull in a large dependency tree
- **Fix:** Replaced `clipr.Run` and `clitr.Run` with `cmd.Run("oc", "logs", "--selector=...")` for both pipeline and task run logs
- **Files modified:** pkg/pipelines/pipelines.go, pkg/pipelines/taskrun.go
- **Verification:** go build ./pkg/pipelines/... passes, log functions compile correctly
- **Committed in:** 70609bc (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Essential to avoid dependency bloat. Log output format is equivalent; actual content differs only in formatting.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- pkg/wait and pkg/pipelines are ready for use by all future test migration phases (4-9)
- 9 sanity tests are ready for cluster execution once JUnit validation (03-02) is complete
- pkg/k8s provides GetWarningEvents for diagnostic output in test failures

---
*Phase: 03-sanity-test-migration*
*Completed: 2026-04-02*
