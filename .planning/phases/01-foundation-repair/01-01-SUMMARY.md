---
phase: 01-foundation-repair
plan: 01
subsystem: infra
tags: [go-modules, imports, migration]

# Dependency graph
requires: []
provides:
  - "Correct module path (github.com/openshift-pipelines/release-tests-ginkgo) across all Go files and go.mod"
  - "Local imports in pkg/oc/oc.go pointing to release-tests-ginkgo packages (not Gauge repo)"
  - "config.Path() with single-return string signature"
  - "Cloned reference-repo/release-tests for side-by-side migration comparison"
affects: [01-foundation-repair, 02-scaffolding, 03-sanity-validation]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "config.Path() panics on missing test data path instead of returning error (setup failure = programmer error)"

key-files:
  created:
    - ".gitignore"
  modified:
    - "pkg/oc/oc.go"
    - "pkg/config/config.go"
    - "go.mod"
    - "pkg/cmd/cmd.go"
    - "pkg/store/store.go"
    - "pkg/opc/opc.go"
    - "pkg/pac/pac.go"
    - "tests/pac/pac_test.go"

key-decisions:
  - "config.Path() panics on missing path instead of returning error -- test data missing is a setup/programmer error, not a runtime condition"

patterns-established:
  - "Module path: github.com/openshift-pipelines/release-tests-ginkgo for all internal imports"
  - "Reference repo cloned to reference-repo/release-tests (gitignored) for migration comparison"

requirements-completed: [FOUND-01, FOUND-02, FOUND-05]

# Metrics
duration: 2min
completed: 2026-04-02
---

# Phase 1 Plan 01: Foundation Import Fix Summary

**Fixed dual store import corruption in pkg/oc/oc.go, renamed module path from srivickynesh to openshift-pipelines, and cloned Gauge source repo for migration reference**

## Performance

- **Duration:** 2 min
- **Started:** 2026-04-02T09:37:42Z
- **Completed:** 2026-04-02T09:40:11Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments
- Eliminated the dual store import corruption: pkg/oc/oc.go now imports cmd, config, and store from the local release-tests-ginkgo module instead of the Gauge repo (release-tests)
- Fixed the MustSuccedIncreasedTimeout typo (2 occurrences) to MustSucceedIncreasedTimeout, matching the actual function signature in pkg/cmd/cmd.go
- Changed config.Path() from (string, error) to string return, simplifying all callers and panicking on missing test data path
- Renamed module path from github.com/srivickynesh/release-tests-ginkgo to github.com/openshift-pipelines/release-tests-ginkgo across all Go files and go.mod
- Cloned openshift-pipelines/release-tests as reference-repo for side-by-side migration comparison

## Task Commits

Each task was committed atomically:

1. **Task 1: Fix pkg/oc/oc.go imports and config.Path() signature** - `98a2505` (fix)
2. **Task 2: Rename module path and clone reference repo** - `30f6b16` (chore)

## Files Created/Modified
- `pkg/oc/oc.go` - Fixed 3 imports from Gauge repo to local, fixed typo in 2 function calls
- `pkg/config/config.go` - Changed Path() from (string, error) to string return with panic
- `go.mod` - Updated module declaration to openshift-pipelines org
- `pkg/cmd/cmd.go` - Updated import path from srivickynesh to openshift-pipelines
- `pkg/store/store.go` - Updated 2 import paths from srivickynesh to openshift-pipelines
- `pkg/opc/opc.go` - Updated import path from srivickynesh to openshift-pipelines
- `pkg/pac/pac.go` - Updated 2 import paths from srivickynesh to openshift-pipelines
- `tests/pac/pac_test.go` - Updated import path from srivickynesh to openshift-pipelines
- `.gitignore` - Created with reference-repo/ exclusion

## Decisions Made
- config.Path() panics on missing test data path instead of returning error -- test data missing is a setup/programmer error, not a recoverable runtime condition

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- All internal imports now point to the correct local module path
- No srivickynesh references remain anywhere in the codebase
- No Gauge repo imports remain in project Go files (reference-repo excluded)
- Ready for Plan 02 (Go toolchain upgrade and go mod tidy)
- Note: go build will not succeed yet because go.sum and dependency versions need updating (Plan 02 scope)

## Self-Check: PASSED

All artifacts verified:
- All 8 modified files exist
- reference-repo/release-tests directory exists
- Commit 98a2505 (Task 1) found
- Commit 30f6b16 (Task 2) found

---
*Phase: 01-foundation-repair*
*Completed: 2026-04-02*
