# Phase 11: Parity Validation - Research

**Researched:** 2026-03-31
**Domain:** Test suite equivalence validation, JUnit XML comparison, label filtering parity, Polarion compatibility
**Confidence:** HIGH

## Summary

Phase 11 is the capstone validation phase that proves the Ginkgo migration is functionally equivalent to the original Gauge suite. Unlike prior phases that built or migrated code, this phase produces evidence artifacts -- comparison reports, delta analyses, and Polarion import confirmations -- that demonstrate the migration preserved test coverage, test behavior, and CI/reporting compatibility.

The validation covers four dimensions: (1) test count parity via `--dry-run -v` comparison, (2) pass/fail result parity on an identical cluster, (3) label filtering equivalence between `--label-filter` and `--tags`, and (4) Polarion uploader acceptance of Ginkgo JUnit XML. Each dimension maps to a requirement (PAR-01 through PAR-04).

**Primary recommendation:** Execute validation in two waves. Wave 1 performs the dry-run count comparison and label filtering equivalence (no cluster required, fast feedback). Wave 2 performs the live cluster pass/fail comparison and Polarion import validation (requires cluster access and Polarion credentials). This ordering lets us catch structural issues (missing tests, wrong labels) before investing cluster time.

**Key insight:** The ~25 dropped upstream tests must be accounted for explicitly. The validation script must produce a manifest of dropped tests so the count delta is justified, not just "roughly equal."

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| PAR-01 | Same test count -- Ginkgo suite reports same number of tests as Gauge suite (minus dropped upstream tests) | Use `ginkgo run --dry-run -v ./tests/...` to list all specs without executing. Compare against `gauge run --list ./specs/...` or equivalent Gauge listing command. The ~25 dropped upstream tests must be enumerated in a DROPPED-TESTS.md manifest so the delta is accounted for. |
| PAR-02 | Same pass/fail results on identical cluster state | Run both suites against the same cluster in sequence (Gauge first, then Ginkgo, or vice versa). Produce a delta report comparing test outcomes. Any difference must be investigated and documented as either a known/justified divergence or a migration bug to fix. Cluster state should be clean between runs (fresh namespace, clean TektonConfig). |
| PAR-03 | Label filtering equivalence -- `ginkgo run --label-filter=sanity` matches `gauge run --tags sanity` | Test all four label categories (sanity, smoke, e2e, disconnected) plus compound expressions (`e2e && !disconnected`). Compare test lists from both suites. Phase 2 established that suite-level Labels match directory names; this validates that per-spec labels also match Gauge tags. |
| PAR-04 | JUnit XML compatible with Polarion uploader (same test case IDs, same format) | Submit both Gauge and Ginkgo JUnit XML to Polarion JUMP importer. Compare import reports: same test case IDs created, same pass/fail recorded, no "Project id not specified" errors. Phase 3 established the Polarion test case ID naming convention (PIPELINES-XX-TCXX in test names). |
</phase_requirements>

## Standard Stack

### Core (No new libraries needed)

| Tool | Purpose | Usage |
|------|---------|-------|
| Ginkgo CLI (`ginkgo`) | `--dry-run -v` for test listing, `--label-filter` for filtered runs, `--junit-report` for XML output | Already installed (v2.28.1) from Phase 1 |
| Gauge CLI (`gauge`) | `--tags` for filtered runs, JUnit plugin for XML output | Available in reference-repo or CI image |
| `diff` / `comm` | Compare sorted test lists and JUnit outputs | Standard Unix utilities |
| `xmllint` / `yq` | Parse and compare JUnit XML structure | Standard tools for XML comparison |
| `jq` | Parse JSON outputs from Ginkgo dry-run | Standard tool |

### No New Dependencies

Phase 11 is a validation phase. It uses existing CLIs and standard Unix tools. No Go code changes, no library additions.

## Architecture Patterns

### Pattern 1: Dry-Run Test Count Comparison (PAR-01)

**What:** Extract test lists from both suites and compare counts.
**Confidence:** HIGH

Ginkgo side:
```bash
# List all test specs without executing
ginkgo run --dry-run -v ./tests/... 2>&1 | grep -E "^.*\[It\]" | sort > ginkgo-tests.txt
wc -l ginkgo-tests.txt
```

Gauge side (from reference repo):
```bash
# List all scenarios from spec files
cd reference-repo/release-tests
gauge list scenarios specs/ | sort > gauge-tests.txt
wc -l gauge-tests.txt
```

Comparison:
```bash
# The delta should equal exactly the dropped upstream tests
diff <(wc -l < ginkgo-tests.txt) <(echo "$(($(wc -l < gauge-tests.txt) - DROPPED_COUNT))")
```

The dropped upstream tests (~25) must be documented in a DROPPED-TESTS.md file listing each test name and the reason it was dropped (e.g., "covered by Tekton CI in Konflux on OpenShift").

### Pattern 2: Pass/Fail Delta Report (PAR-02)

**What:** Run both suites against the same cluster and compare outcomes.
**Confidence:** HIGH

Protocol:
1. Record cluster state (OpenShift version, Tekton operator version, TektonConfig state)
2. Run Gauge suite, capture JUnit XML with pass/fail per test
3. Reset cluster state (clean namespaces, restore TektonConfig)
4. Run Ginkgo suite, capture JUnit XML with pass/fail per test
5. Parse both JUnit XMLs, extract test name + status pairs
6. Produce delta report: tests that pass in one but fail in the other

Delta report format:
```
## Parity Delta Report
Cluster: [version info]
Date: [timestamp]

### Matching Results: N/M tests (XX%)
### Divergent Results: K tests

| Test Name | Gauge Result | Ginkgo Result | Justification |
|-----------|-------------|---------------|---------------|
| [name]    | PASS        | FAIL          | [reason or "INVESTIGATE"] |
```

A successful validation has zero unjustified divergences. Known differences (e.g., timing-sensitive tests, test order dependencies resolved in Ginkgo) are documented with justifications.

### Pattern 3: Label Filtering Equivalence (PAR-03)

**What:** Verify that label-based test selection produces the same test sets.
**Confidence:** HIGH

Test matrix:
```bash
# For each label/tag:
for label in sanity smoke e2e disconnected; do
  ginkgo run --dry-run -v --label-filter="$label" ./tests/... 2>&1 | grep -E "\[It\]" | sort > "ginkgo-$label.txt"
  # Compare against Gauge tag listing:
  gauge list scenarios --tags "$label" specs/ | sort > "gauge-$label.txt"
  diff "ginkgo-$label.txt" "gauge-$label.txt"
done

# Compound expression:
ginkgo run --dry-run -v --label-filter="e2e && !disconnected" ./tests/... 2>&1 | grep -E "\[It\]" | sort > ginkgo-compound.txt
gauge list scenarios --tags "e2e & !disconnected" specs/ | sort > gauge-compound.txt
diff ginkgo-compound.txt gauge-compound.txt
```

Note: Gauge uses `&` for AND, Ginkgo uses `&&`. Gauge uses `!` for NOT, Ginkgo uses `!`. The expressions are syntactically similar but must be verified.

### Pattern 4: Polarion JUnit XML Comparison (PAR-04)

**What:** Submit both JUnit XMLs to Polarion and compare import results.
**Confidence:** MEDIUM (depends on Polarion instance access)

Steps:
1. Generate Ginkgo JUnit XML: `ginkgo run --junit-report=ginkgo-junit.xml ./tests/...`
2. Generate Gauge JUnit XML: `gauge run --env=default specs/` (JUnit plugin configured)
3. Compare XML structure:
   - Both have `<testsuite>` with matching `tests` count
   - Both have `<testcase>` entries with Polarion test case IDs in `name` attribute
   - Ginkgo `classname` format matches what Polarion expects
   - `<properties>` section present (may need post-processing from Phase 10 CI-03)
4. Submit both to Polarion JUMP importer
5. Compare import reports: same test cases created, same results recorded

If Polarion instance is not available for direct validation, the comparison can be done structurally by comparing XML elements that Polarion parses: `testcase[@name]`, `testcase[@classname]`, and `testsuite/properties/property` elements.

### Anti-Patterns to Avoid

- **Do NOT compare test names character-by-character.** Ginkgo test names include the Describe/Context/It hierarchy (e.g., `[ecosystem] PIPELINES-30-TC01: Verify buildah task ...`). Gauge test names are flat scenario names. Comparison must normalize to Polarion test case IDs.
- **Do NOT run both suites simultaneously on the same cluster.** Many tests modify cluster-wide state (TektonConfig, operator settings). Run sequentially with state reset between suites.
- **Do NOT ignore flaky tests in the delta.** A test that passes in Gauge but flakes in Ginkgo is a migration bug (likely timing, ordering, or state isolation issue). Mark as "INVESTIGATE" not "known flaky."
- **Do NOT skip the dropped test manifest.** Without an explicit list of ~25 dropped upstream tests, a count difference of 25 could hide 5 missing tests and 20 unintentionally dropped tests.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Test list extraction | Custom Go program to parse test tree | `ginkgo --dry-run -v` + grep/awk | Ginkgo CLI already outputs structured test lists |
| JUnit XML comparison | Custom XML parser | `xmllint --xpath` or Python `xml.etree` | Standard XML tools handle the comparison |
| Delta report generation | Complex reporting framework | Shell script with `diff`/`comm`/`awk` | Simple text comparison is sufficient |

## Common Pitfalls

### Pitfall 1: Gauge and Ginkgo Test Name Formats Differ
**What goes wrong:** Direct string comparison of test names shows every test as "different" because Ginkgo prefixes test names with Describe/Context hierarchy while Gauge uses flat scenario names.
**How to avoid:** Extract Polarion test case IDs (PIPELINES-XX-TCXX) from both outputs and compare those. The ID is the stable identifier across both frameworks.

### Pitfall 2: Test Count Includes Pending/Skipped Tests Differently
**What goes wrong:** Ginkgo's `--dry-run` counts include `PDescribe`/`PIt` (pending) specs. Gauge's listing may or may not include pending scenarios. The counts don't match even though the real test coverage is identical.
**How to avoid:** Filter out pending specs from Ginkgo output (`grep -v "P \["` or similar). Count only executable specs.

### Pitfall 3: Cluster State Drift Between Suite Runs
**What goes wrong:** Running Gauge suite first leaves cluster state (orphaned namespaces, modified TektonConfig, leftover CRDs) that affects Ginkgo suite results. The delta report shows false failures.
**How to avoid:** Run a cluster reset script between suites: delete test namespaces, restore TektonConfig to defaults, wait for operator reconciliation. Document the reset procedure.

### Pitfall 4: Polarion Import Succeeds But Creates Wrong Entries
**What goes wrong:** Polarion JUMP importer accepts the JUnit XML without error but maps test cases to wrong project, creates duplicate entries, or misses the test case ID because `classname` format changed.
**How to avoid:** After import, query Polarion API to verify: correct project, correct test case IDs, correct pass/fail status. Don't just check "import succeeded."

### Pitfall 5: Label Compound Expressions Have Different Semantics
**What goes wrong:** Gauge `--tags "sanity & smoke"` and Ginkgo `--label-filter="sanity && smoke"` appear equivalent but may differ in edge cases (e.g., how they handle specs with multiple labels, or specs inheriting labels from parent containers).
**How to avoid:** Test compound expressions specifically. Verify not just the count but the actual test names match.

## Recommended Execution Order

1. **Wave 1: Structural validation (no cluster required)**
   - Extract Ginkgo test list via `--dry-run -v`
   - Extract Gauge test list from reference repo
   - Create DROPPED-TESTS.md manifest
   - Compare counts (PAR-01)
   - Compare label-filtered test sets for all labels (PAR-03)

2. **Wave 2: Runtime validation (cluster required)**
   - Run pass/fail comparison on identical cluster (PAR-02)
   - Generate and compare JUnit XML for Polarion (PAR-04)
   - Produce final parity report

## Open Questions

1. **How to access Polarion instance for PAR-04 validation?**
   - What we know: Polarion JUMP importer is used by the team. JUnit XML format requirements were identified in research (SUMMARY.md pitfall #5).
   - Recommendation: If Polarion access is not available, validate structurally by comparing XML elements. Create a checkpoint for human verification of actual Polarion import.
   - **Impact if deferred:** PAR-04 becomes partially validated (structural only, not end-to-end).

2. **Is Gauge CLI available in the current environment?**
   - What we know: The reference repo is cloned. Gauge may or may not be installed.
   - Recommendation: Use `gauge list scenarios` from the reference repo if Gauge is available. If not, parse the `.spec` files directly to extract scenario names and tags.
   - **Impact if deferred:** PAR-01 and PAR-03 need an alternative parsing approach.

## Sources

### Primary (HIGH confidence)
- Ginkgo v2 CLI documentation: `--dry-run`, `--label-filter`, `--junit-report` flags
- Gauge CLI documentation: `--tags`, `list scenarios`, JUnit plugin
- ROADMAP.md Phase 11 requirements (PAR-01 through PAR-04)
- REQUIREMENTS.md parity validation section
- SUMMARY.md pitfall #5 (JUnit XML incompatibility with Polarion)
- Phase 2 decisions: suite-level Labels match directory names, per-spec labels established
- Phase 3 requirements: Polarion test case ID naming convention (PIPELINES-XX-TCXX)

### Secondary (MEDIUM confidence)
- Submariner Shipyard Issue #48: Polarion JUnit XML parsing requirements
- Gauge spec file format documentation: scenario extraction and tag parsing

## Metadata

**Confidence breakdown:**
- PAR-01 (test count): HIGH -- mechanical comparison with well-defined outputs
- PAR-02 (pass/fail): HIGH -- standard JUnit comparison, main risk is cluster state management
- PAR-03 (label filtering): HIGH -- dry-run comparison, no cluster needed
- PAR-04 (Polarion): MEDIUM -- depends on Polarion instance access and exact XML format requirements

**Research date:** 2026-03-31
**Valid until:** 2026-05-31 (stable -- validation methodology doesn't change with framework versions)
