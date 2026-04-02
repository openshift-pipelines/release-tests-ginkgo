---
phase: 11-parity-validation
plan: 01
subsystem: testing
tags: [parity, validation, shell-scripts, polarion, labels]

requires:
  - phase: 10-ci-and-reporting
    provides: Complete Ginkgo test suite with all 137 tests migrated across 11 areas
provides:
  - DROPPED-TESTS.md manifest documenting 1 dropped test and 13 pending tests
  - parity-count.sh script for Polarion ID count comparison
  - parity-labels.sh script for label filtering equivalence validation
affects: [11-02-PLAN]

tech-stack:
  added: []
  patterns: [polarion-id-comparison, static-label-analysis, gauge-spec-parsing]

key-files:
  created:
    - .planning/phases/11-parity-validation/DROPPED-TESTS.md
    - scripts/parity-count.sh
    - scripts/parity-labels.sh
  modified: []

key-decisions:
  - "Polarion ID comparison is the authoritative parity metric, not raw spec count"
  - "PIPELINES-21-TC01 (Hub test) subsumed into OLM ordered container, not truly dropped"
  - "Static label analysis documents limitations vs runtime label-filter"

patterns-established:
  - "Polarion ID extraction: grep -roh 'PIPELINES-[0-9]*-TC[0-9]*' for cross-suite comparison"
  - "Multi-method validation: Polarion IDs, scenario count, ginkgo dry-run in single script"

requirements-completed: [PAR-01, PAR-03]

duration: 4min
completed: 2026-04-02
---

# Phase 11 Plan 01: Structural Parity Validation Summary

**DROPPED-TESTS.md manifest with 1 documented drop plus test count and label filtering parity scripts proving 126/127 Polarion ID coverage**

## Performance

- **Duration:** 4 min
- **Started:** 2026-04-02T12:13:58Z
- **Completed:** 2026-04-02T12:22:00Z
- **Tasks:** 2
- **Files created:** 3

## Accomplishments
- Audited all 129 Gauge scenarios across 31 spec files and documented that only 1 Polarion ID (PIPELINES-21-TC01) is missing as standalone test -- subsumed into OLM install ordered container
- Created parity-count.sh that validates Polarion ID equivalence (127 Gauge vs 126 Ginkgo = 1 documented drop) and exits 0 on confirmed parity
- Created parity-labels.sh that validates label filtering across sanity, e2e, disconnected, tls labels plus compound expressions, with both static analysis and ginkgo dry-run integration

## Task Commits

Each task was committed atomically:

1. **Task 1: Create dropped tests manifest and test count comparison script** - `fbae624` (feat)
2. **Task 2: Create label filtering equivalence validation script** - `297d8c5` (feat)

## Files Created/Modified
- `.planning/phases/11-parity-validation/DROPPED-TESTS.md` - Manifest of all dropped/pending/restructured tests with justifications
- `scripts/parity-count.sh` - Executable script comparing Polarion ID counts between suites (3 methods)
- `scripts/parity-labels.sh` - Executable script comparing label-filtered test sets between suites

## Decisions Made
- **Polarion ID as canonical metric:** Raw spec counts differ significantly (233 Ginkgo specs vs 129 Gauge scenarios) due to Ordered container decomposition. Polarion IDs provide the 1:1 mapping between test coverage in both frameworks.
- **Hub test (PIPELINES-21-TC01) subsumed not dropped:** The Hub installation steps are embedded in the OLM ordered container. Documented as a structural change rather than a true coverage gap.
- **Static label analysis with documented limitations:** Ginkgo's label inheritance from Describe/Context parents means static grep-based analysis understates matches. Scripts document this clearly and recommend runtime dry-run for authoritative validation.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
- Ginkgo dry-run requires a live cluster for BeforeSuite to initialize clients, so dry-run spec counts show 0 in local environment. The Polarion ID comparison method works without a cluster and is the primary validation method.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- DROPPED-TESTS.md is ready for Plan 11-02 to reference for the final PARITY-REPORT.md
- Both parity scripts are ready for runtime validation on a live cluster
- Plan 11-02 can now create the results and Polarion comparison scripts and the final parity report

---
*Phase: 11-parity-validation*
*Completed: 2026-04-02*
