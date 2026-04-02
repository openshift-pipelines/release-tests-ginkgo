---
phase: 06-operator-migration
plan: 02
subsystem: testing
tags: [ginkgo, operator, auto-prune, pruner, upgrade, pre-upgrade, post-upgrade]

requires:
  - phase: 06-operator-migration
    provides: pkg/operator/ package with validation helpers
provides:
  - 23 operator tests across 2 files (autoprune, upgrade) plus 6 testdata fixtures
  - Pruner testdata YAML fixtures for auto-prune testing
affects: [10-parallelism-enablement]

tech-stack:
  added: []
  patterns: [Pruner config lifecycle (remove -> create -> configure -> poll -> verify -> cleanup), Label-based CI filtering for pre/post upgrade tests, Intentional time.Sleep for resource age differentiation]

key-files:
  created:
    - tests/operator/autoprune_test.go
    - tests/operator/upgrade_test.go
    - testdata/pruner/pipeline/pipeline-for-pruner.yaml
    - testdata/pruner/pipeline/pipelinerun-for-pruner.yaml
    - testdata/pruner/task/task-for-pruner.yaml
    - testdata/pruner/task/taskrun-for-pruner.yaml
    - testdata/pruner/namespaces/namespace-one.yaml
    - testdata/pruner/namespaces/namespace-two.yaml
  modified: []

key-decisions:
  - "S2I post-upgrade test (PIPELINES-19-TC05) uses Skip() pending StartAndVerifyPipelineWithParam helper migration"
  - "Intentional time.Sleep preserved for TC04 and TC09 (resource age gap creation, not polling)"
  - "Used oc project for switching to pre-upgrade namespaces in post-upgrade tests"

patterns-established:
  - "Pruner config management helpers (removePrunerConfig, updatePrunerConfig, updatePrunerConfigWithInvalidData)"
  - "assertResourceCount with configurable timeout for async resource count verification"
  - "assertCronjobPresence for Eventually-based cronjob polling"
  - "Label('pre-upgrade') and Label('post-upgrade') for CI phase filtering"

requirements-completed: [OPER-01, OPER-02]

duration: 6min
completed: 2026-04-02
---

# Phase 6 Plan 2: Auto-Prune and Upgrade Test Migration Summary

**15 auto-prune tests with pruner config lifecycle management and 8 pre/post upgrade tests with Label-based CI filtering, plus 6 YAML testdata fixtures**

## Performance

- **Duration:** 6 min
- **Started:** 2026-04-02T11:47:44Z
- **Completed:** 2026-04-02T11:53:22Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments
- Copied 6 YAML fixtures to testdata/pruner/ for auto-prune test resource creation
- Migrated 15 auto-prune tests (PIPELINES-12-TC01 through TC15) covering pruner cronjob lifecycle, per-namespace annotation overrides, validation, stability, and container count verification
- Migrated 8 upgrade tests (4 pre-upgrade PIPELINES-18, 4 post-upgrade PIPELINES-19) with Label-based CI filtering
- Converted all async waits to Eventually polling (9 Eventually calls in autoprune_test.go)
- 13 DeferCleanup calls ensuring pruner config and namespace cleanup

## Task Commits

Each task was committed atomically:

1. **Task 1: Copy pruner testdata and migrate auto-prune tests** - `8cba01d` (feat)
2. **Task 2: Migrate pre/post upgrade tests** - `5e0bdc1` (feat)

## Files Created/Modified
- `testdata/pruner/pipeline/pipeline-for-pruner.yaml` - Pipeline and task definitions for pruner tests
- `testdata/pruner/pipeline/pipelinerun-for-pruner.yaml` - 5 PipelineRun instances for pruner count tests
- `testdata/pruner/task/task-for-pruner.yaml` - Echo task for pruner tests
- `testdata/pruner/task/taskrun-for-pruner.yaml` - 5 TaskRun instances for pruner count tests
- `testdata/pruner/namespaces/namespace-one.yaml` - Namespace with pruner annotations for TC15
- `testdata/pruner/namespaces/namespace-two.yaml` - Namespace with pruner annotations for TC15
- `tests/operator/autoprune_test.go` - 15 auto-prune tests with Serial container, Ordered Contexts, DeferCleanup, Eventually polling
- `tests/operator/upgrade_test.go` - 8 upgrade tests split into pre/post groups with Label filtering

## Decisions Made
- S2I post-upgrade test (PIPELINES-19-TC05) uses `Skip("helper not yet migrated: StartAndVerifyPipelineWithParam")` since the pipeline start-and-verify-with-param helper is not yet available in the Ginkgo codebase
- Preserved intentional `time.Sleep(120s)` for TC04 and TC09 to create resource age gaps for keep-since strategy testing (documented with comments explaining why this is not a polling scenario)
- Used `oc project <name>` via cmd.MustSucceed for switching to pre-upgrade namespaces in post-upgrade tests since SwitchProject helper doesn't exist

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Phase 6 complete -- all 33 operator tests migrated across 6 files
- 1 test (PIPELINES-19-TC05) is skipped pending helper migration from a future phase
- pkg/operator/ package fully available for all operator test needs

---
*Phase: 06-operator-migration*
*Completed: 2026-04-02*
