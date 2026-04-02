---
phase: 10-ci-and-reporting
plan: 02
subsystem: testing
tags: [diagnostics, junit, polarion, ginkgo, xml]

requires:
  - phase: 09-remaining-areas-migration
    provides: All tests migrated with consistent suite_test.go pattern
provides:
  - Diagnostic collection package for ReportAfterEach (pkg/diagnostics/)
  - JUnit XML to Polarion format transformer (cmd/junit-polarion/)
affects: [10-ci-and-reporting]

tech-stack:
  added: []
  patterns: [report-after-each-diagnostics, junit-xml-transform, deferred-namespace-pointer]

key-files:
  created:
    - pkg/diagnostics/diagnostics.go
    - cmd/junit-polarion/main.go
  modified: []

key-decisions:
  - "CollectOnFailure accepts *string pointer for deferred namespace read at report time"
  - "Uses report.Failed() instead of checking individual SpecState constants"
  - "Uses exec.CommandContext directly instead of pkg/cmd to avoid test assertions in reporter"
  - "JUnit Polarion IDs embedded in classname field since standard JUnit lacks per-case properties"

patterns-established:
  - "Diagnostic collection via ReportAfterEach with namespace pointer pattern"
  - "JUnit XML post-processing as standalone Go CLI tool"

requirements-completed: [CI-03, CI-04]

duration: 3min
completed: 2026-04-02
---

# Phase 10 Plan 02: Diagnostics and JUnit-to-Polarion Summary

**ReportAfterEach diagnostic collector for cluster events/pods/logs on failure, and JUnit XML post-processor injecting Polarion project IDs and test case mappings**

## Performance

- **Duration:** 3 min
- **Started:** 2026-04-02T12:01:30Z
- **Completed:** 2026-04-02T12:04:30Z
- **Tasks:** 2
- **Files created:** 2

## Accomplishments
- Diagnostic collector that gathers events, resource state, and pod logs only on test failure
- JUnit-to-Polarion transformer extracting PIPELINES-XX-TCXX IDs from spec names
- Both packages compile successfully with go build ./...

## Task Commits

Each task was committed atomically:

1. **Task 1: Create diagnostic collection package** - `8f5ae71` (feat)
2. **Task 2: Create JUnit-to-Polarion post-processing tool** - `12d797d` (feat)

## Files Created/Modified
- `pkg/diagnostics/diagnostics.go` - ReportAfterEach diagnostic collector with events, resources, pod logs
- `cmd/junit-polarion/main.go` - Standalone JUnit XML to Polarion format transformer

## Decisions Made
- CollectOnFailure uses `*string` pointer for namespace so the current value is read at report time, not registration time
- Uses `report.Failed()` method instead of individual SpecState constants (simpler, avoids types package import)
- Uses `exec.CommandContext` directly rather than `pkg/cmd` to avoid test framework assertions in reporter context
- JUnit Polarion test case IDs embedded in classname since standard JUnit schema lacks per-case properties

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Changed SpecState constants to report.Failed() method**
- **Found during:** Task 1 (Diagnostic collection package)
- **Issue:** Plan specified checking individual SpecState constants (SpecStateFailed, SpecStatePanicked, etc.) but these are in types sub-package and not re-exported by Ginkgo v2 dot import
- **Fix:** Used `report.Failed()` method which returns true for all failure states
- **Files modified:** pkg/diagnostics/diagnostics.go
- **Verification:** `go build ./pkg/diagnostics/` succeeds
- **Committed in:** 8f5ae71 (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Necessary fix for compilation. Functionally equivalent behavior.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Ready for 10-03 (SynchronizedBeforeSuite upgrade and diagnostics wiring)
- pkg/diagnostics ready for import in all 11 suite_test.go files

---
*Phase: 10-ci-and-reporting*
*Completed: 2026-04-02*
