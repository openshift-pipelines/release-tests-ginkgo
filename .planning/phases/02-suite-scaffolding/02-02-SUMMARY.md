---
phase: 02-suite-scaffolding
plan: 02
subsystem: testing
tags: [ginkgo, gomega, defer-cleanup, eventually, consistently, ordered, pattern-reference]

# Dependency graph
requires:
  - phase: 02-suite-scaffolding
    provides: 11 Ginkgo suite entry points with BeforeSuite/AfterSuite and sharedClients
provides:
  - Reference implementation of DeferCleanup pattern with LIFO cleanup and correct form
  - Reference implementation of Eventually/Consistently async polling patterns
  - Reference implementation of Ordered container with BeforeAll and sequential Its
  - Pattern comment documentation for WRONG vs RIGHT cleanup context capture
affects: [02-03, 03-sanity-test-migration, 04-ecosystem, 05-triggers, 06-operator, 07-pipelines, 08-pac, 09-remaining]

# Tech tracking
tech-stack:
  added: []
  patterns: [DeferCleanup co-located cleanup, Eventually with config.APITimeout/APIRetry, Consistently with config.ConsistentlyDuration, Ordered container with BeforeAll and shared closure vars]

key-files:
  created:
    - tests/operator/operator_patterns_test.go
  modified: []

key-decisions:
  - "Used PDescribe (pending) for all pattern specs to avoid requiring a live cluster during scaffolding"
  - "Placed all three patterns in a single file in operator suite since operator tests will use all patterns heavily"
  - "Added sharedClients accessibility spec (non-pending) to verify cross-file package variable sharing compiles"

patterns-established:
  - "DeferCleanup pattern: create resource then immediately DeferCleanup(func() { teardown() }) for LIFO cleanup"
  - "Eventually pattern: Eventually(func(g Gomega) { g.Expect(...) }).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Succeed())"
  - "Consistently pattern: Consistently(func() T { ... }).WithTimeout(config.ConsistentlyDuration).WithPolling(config.APIRetry).Should(Equal(...))"
  - "Ordered container pattern: PDescribe('...', Ordered, func() { var shared; BeforeAll(func() { init; DeferCleanup(...) }); It(...); It(...) })"

requirements-completed: [SUITE-05, SUITE-06, SUITE-07]

# Metrics
duration: 3min
completed: 2026-04-02
---

# Phase 2 Plan 2: Pattern Reference Specs Summary

**DeferCleanup, Eventually/Consistently, and Ordered container reference implementations in operator suite using PDescribe with config constants and oc helper functions**

## Performance

- **Duration:** 3 min
- **Started:** 2026-04-02T10:36:18Z
- **Completed:** 2026-04-02T10:39:18Z
- **Tasks:** 1
- **Files modified:** 1

## Accomplishments
- Created reference DeferCleanup pattern with LIFO cleanup ordering, correct form documentation, and WRONG vs RIGHT context capture comments
- Created reference Eventually pattern using config.APITimeout and config.APIRetry with func(g Gomega) signature
- Created reference Consistently pattern using config.ConsistentlyDuration for stability assertion window
- Created reference Ordered container with BeforeAll, shared closure variables, sequential Its, and automatic skip-on-failure documentation
- Added sharedClients accessibility verification spec

## Task Commits

Each task was committed atomically:

1. **Task 1: Create DeferCleanup, Eventually/Consistently, and Ordered pattern specs** - `daf9228` (feat)

## Files Created/Modified
- `tests/operator/operator_patterns_test.go` - Reference implementations of DeferCleanup (SUITE-05), Eventually/Consistently (SUITE-06), and Ordered (SUITE-07) patterns with comprehensive comments

## Decisions Made
- Used PDescribe for pattern specs to keep them pending -- avoids requiring a live cluster while still compiling and serving as copy-paste references
- Placed all patterns in a single file (operator_patterns_test.go) rather than separate files -- easier to find and cross-reference
- Used actual project helpers (oc.CreateNewProject, oc.CheckProjectExists, config constants) for realistic examples rather than toy functions
- Added a non-pending sharedClients accessibility spec to prove the package-level variable from suite_test.go is accessible

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- All three pattern references (SUITE-05, SUITE-06, SUITE-07) are in place and compile cleanly
- Ready for 02-03-PLAN.md (DescribeTable/Entry and Skip patterns)
- Future migration phases can copy these patterns directly into their test files
- The operator suite now has both the suite entry point (suite_test.go) and pattern references (operator_patterns_test.go)

## Self-Check: PASSED

- tests/operator/operator_patterns_test.go: FOUND (170 lines, exceeds min_lines: 60)
- Commit daf9228 (Task 1): FOUND
- go vet ./tests/operator/...: PASSED (exit 0)
- go build ./tests/operator/...: PASSED (exit 0)

---
*Phase: 02-suite-scaffolding*
*Completed: 2026-04-02*
