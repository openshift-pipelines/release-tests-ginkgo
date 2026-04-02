---
phase: 07-pipelines-core-migration
plan: 02
subsystem: testing
tags: [ginkgo, resolvers, bundles, cluster, git, http, hub, e2e]

requires:
  - phase: 07-pipelines-core-migration
    provides: pkg/pipelines helpers migrated, pipelinerun and failure test specs
provides:
  - 8 resolver test specs (bundles 2, cluster 2, git 2, HTTP 1, hub 1)
  - 20 resolver testdata YAML fixtures (10 pipelineruns, 6 pipelines, 4 tasks)
  - Complete 16-test pipelines core suite
affects: [10-ci-reporting, 11-parity-validation]

tech-stack:
  added: []
  patterns: [Ordered container for cross-namespace resolver setup, os.Getenv+Skip for optional auth tests]

key-files:
  created:
    - tests/pipelines/resolvers_test.go
    - testdata/resolvers/pipelineruns/ (10 files)
    - testdata/resolvers/pipelines/ (6 files)
    - testdata/resolvers/tasks/ (4 files)
  modified: []

key-decisions:
  - "Assigned PIPELINES-32-TC01 to hub resolver test (no Polarion ID in original Gauge spec)"
  - "Used Ordered container for cluster resolver to manage cross-namespace project lifecycle"
  - "Git resolver TC02 uses Skip rather than pending -- allows automatic re-enablement when GITHUB_TOKEN is set"
  - "Hub resolver uses oc.Apply instead of oc.Create (matches Gauge spec pattern for idempotent resources)"

patterns-established:
  - "Pattern: Ordered + BeforeAll/AfterAll for cross-namespace resource setup"
  - "Pattern: os.Getenv + Skip for tests requiring optional environment credentials"
  - "Pattern: DeferCleanup for secret cleanup in auth-dependent tests"

requirements-completed: [PIPE-01]

duration: 3min
completed: 2026-04-02
---

# Phase 7 Plan 02: Resolver Test Specs Summary

**Created 8 resolver test specs (bundles, cluster, git, HTTP, hub) with cross-namespace Ordered setup and GITHUB_TOKEN skip, completing all 16 pipelines core tests**

## Performance

- **Duration:** 3 min
- **Started:** 2026-04-02T11:44:00Z
- **Completed:** 2026-04-02T11:47:00Z
- **Tasks:** 2
- **Files modified:** 21

## Accomplishments
- Created resolvers_test.go with 8 test specs across 5 Describe blocks
- Copied 20 resolver testdata YAML fixtures (10 pipelineruns, 6 pipelines, 4 tasks)
- Cluster resolver uses Ordered container with BeforeAll/AfterAll managing cross-namespace projects
- Git resolver TC02 gracefully skips when GITHUB_TOKEN environment variable is not set
- Total pipeline core test count reaches 16 (6 pipelinerun + 2 failure + 8 resolver)

## Task Commits

Each task was committed atomically:

1. **Task 1: Copy resolver testdata YAML fixtures** - `842fcfb` (chore)
2. **Task 2: Create resolver test specs** - `1a64257` (feat)

## Files Created/Modified
- `tests/pipelines/resolvers_test.go` - 8 resolver test specs with Polarion IDs
- `testdata/resolvers/pipelineruns/*.yaml` - 10 PipelineRun YAML fixtures
- `testdata/resolvers/pipelines/*.yaml` - 6 Pipeline definition YAML fixtures
- `testdata/resolvers/tasks/*.yaml` - 4 Task YAML fixtures

## Decisions Made
- Assigned PIPELINES-32-TC01 to hub resolver (no Polarion ID in original Gauge spec, documented in code comment)
- Used Ordered container for cluster resolver cross-namespace lifecycle
- Git resolver TC02 uses Skip (not Pending) to allow automatic re-enablement when token available
- Hub resolver uses oc.Apply for idempotent resource creation (matching Gauge spec)

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- All 16 pipelines core tests created
- Phase 7 complete
- Ready for any remaining Phase 4-9 migrations

## Self-Check: PASSED

All files verified present. Both task commits (842fcfb, 1a64257) verified in git log.

---
*Phase: 07-pipelines-core-migration*
*Completed: 2026-04-02*
