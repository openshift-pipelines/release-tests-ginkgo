# Roadmap: OpenShift Pipelines Ginkgo Migration

## Overview

This roadmap migrates 110+ OpenShift Pipelines release tests from Gauge to Ginkgo v2. The journey starts by fixing foundational codebase issues (dual store corruption, broken imports, outdated toolchain), then establishes Ginkgo suite scaffolding, validates the migration pattern with sanity tests and JUnit/Polarion compatibility, migrates all test areas incrementally, configures CI with parallel execution, and concludes with full parity validation against the original Gauge suite. Phases 1-3 are strictly sequential (each unblocks the next); test migration phases (4-9) are independent and can be worked in any order; phases 10-11 require all migrations complete.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 1: Foundation Repair** - Fix dual store corruption, rename module path, upgrade Go/Ginkgo toolchain, clone source repo
- [ ] **Phase 2: Suite Scaffolding** - Create Ginkgo suite entry points, lifecycle hooks, label system, and test patterns for all 11 areas
- [ ] **Phase 3: Sanity Test Migration** - Migrate first 9 tests, validate JUnit-to-Polarion pipeline, establish Polarion test case ID convention
- [ ] **Phase 4: Ecosystem Task Migration** - Migrate 36 ecosystem task tests using DescribeTable/Entry pattern
- [ ] **Phase 5: Triggers Migration** - Migrate 23 triggers tests (EventListeners, TriggerBindings, TriggerTemplates)
- [ ] **Phase 6: Operator Migration** - Migrate 33 operator tests with Serial decorator for cluster-wide state modifications
- [ ] **Phase 7: Pipelines Core Migration** - Migrate 16 pipelines core tests (PipelineRuns, TaskRuns, workspaces)
- [ ] **Phase 8: PAC Migration** - Expand PAC scaffolding to all 7 tests with Ordered workflows and GitLab integration
- [ ] **Phase 9: Remaining Areas Migration** - Migrate chains, results, MAG, metrics, versions, OLM, and console tests (13 tests total)
- [ ] **Phase 10: CI and Reporting** - Docker image, label filtering, diagnostics, parallel execution, suite timeout configuration
- [ ] **Phase 11: Parity Validation** - Prove identical test count, pass/fail results, label filtering, and Polarion output vs Gauge suite

## Phase Details

### Phase 1: Foundation Repair
**Goal**: The codebase compiles cleanly with all internal imports resolved, no Gauge dependencies, and current Go/Ginkgo toolchain
**Depends on**: Nothing (first phase)
**Requirements**: FOUND-01, FOUND-02, FOUND-03, FOUND-04, FOUND-05, FOUND-06
**Success Criteria** (what must be TRUE):
  1. Running `go build ./...` succeeds with zero import errors -- no references to `github.com/openshift-pipelines/release-tests/pkg/*` remain in any Go file
  2. The module path in `go.mod` reads `github.com/openshift-pipelines/release-tests-ginkgo` and all internal imports use this path
  3. `go version` in the project reports Go 1.24+ and `go list -m github.com/onsi/ginkgo/v2` reports v2.27+ (or v2.28+ if Go 1.24 is used)
  4. The cloned `openshift-pipelines/release-tests` source repo exists locally as a reference and is accessible for file comparison during migration
  5. `go mod tidy` completes without errors and `go.sum` contains no transitive Gauge framework dependencies
**Plans**: 2 plans

Plans:
- [ ] 01-01-PLAN.md — Fix dual store imports, rename module path, clone reference repo
- [ ] 01-02-PLAN.md — Upgrade Go/Ginkgo/Gomega, remove Gauge deps, verify build

### Phase 2: Suite Scaffolding
**Goal**: Every test area has a working Ginkgo suite entry point with lifecycle hooks and all shared test patterns (labels, cleanup, async assertions, ordered containers, data tables, conditional skip) are established and documented by example
**Depends on**: Phase 1
**Requirements**: SUITE-01, SUITE-02, SUITE-03, SUITE-04, SUITE-05, SUITE-06, SUITE-07, SUITE-08, SUITE-09
**Success Criteria** (what must be TRUE):
  1. Running `ginkgo run ./tests/ecosystem/` (and each of the other 10 areas) executes the suite bootstrap without error -- `BeforeSuite` initializes cluster clients, `AfterSuite` runs cleanup, and the suite reports 0 specs (no tests yet, but the runner works)
  2. A sample spec using `Label("sanity")` is filterable via `ginkgo run --label-filter=sanity` and excluded via `--label-filter="e2e && !sanity"`
  3. A sample spec demonstrates `DeferCleanup` executing resource teardown even when the spec fails (verified by creating and cleaning up a test namespace)
  4. A sample spec demonstrates `Eventually` polling a Kubernetes resource condition (e.g., deployment readiness) instead of using `time.Sleep`
  5. Sample specs demonstrate `Ordered` container (multi-step sequential workflow), `DescribeTable`/`Entry` (data-driven test), and `Skip` (conditional execution) -- all three patterns produce correct Ginkgo output
**Plans**: 3 plans

Plans:
- [ ] 02-01-PLAN.md -- Create suite_test.go entry points for all 11 test areas with BeforeSuite/AfterSuite and labels
- [ ] 02-02-PLAN.md -- DeferCleanup, Eventually/Consistently, and Ordered pattern reference specs
- [ ] 02-03-PLAN.md -- DescribeTable/Entry and Skip pattern reference specs

### Phase 3: Sanity Test Migration
**Goal**: The first real tests are migrated and running, JUnit XML output is validated against the Polarion uploader, and the Gauge-to-Ginkgo migration pattern is proven end-to-end
**Depends on**: Phase 2
**Requirements**: SNTY-01, SNTY-02, SNTY-03
**Success Criteria** (what must be TRUE):
  1. Running `ginkgo run --label-filter=sanity ./tests/...` executes all 9 migrated sanity tests and produces pass/fail results matching the Gauge sanity suite on the same cluster
  2. The JUnit XML file produced by `--junit-report` is successfully imported into Polarion with all test case IDs (e.g., `PIPELINES-XX-TCXX`) correctly mapped -- zero "Project id not specified" or empty import errors
  3. Every migrated sanity test name contains its Polarion test case ID, following the established naming convention (e.g., `Describe("PIPELINES-30-TC01: Verify pipeline version", ...)`)
**Plans**: 2 plans

Plans:
- [ ] 03-01-PLAN.md -- Port wait/pipelines helpers and create 9 sanity test specs with Polarion IDs
- [ ] 03-02-PLAN.md -- JUnit XML validation and Polarion transform utility

### Phase 4: Ecosystem Task Migration
**Goal**: All 36 ecosystem task tests (buildah, s2i, git-clone, etc.) run in Ginkgo using DescribeTable/Entry with correct data parameter passing
**Depends on**: Phase 3
**Requirements**: ECO-01, ECO-02
**Success Criteria** (what must be TRUE):
  1. Running `ginkgo run ./tests/ecosystem/` executes all 36 ecosystem task tests and each generates a separate JUnit entry with its Polarion test case ID
  2. DescribeTable entries correctly receive their parameters at spec execution time (not tree construction time) -- verified by at least one table test where Entry parameters include runtime-evaluated values
  3. Resource cleanup completes for all 36 tests without orphaned namespaces or resources remaining on the cluster after the suite finishes
**Plans**: 2 plans

Plans:
- [ ] 04-01-PLAN.md -- Migrate 21 simple/multiarch ecosystem tests via DescribeTable (buildah, git-cli, tkn, maven, helm, kn, jib-maven, etc.)
- [ ] 04-02-PLAN.md -- Migrate 15 complex ecosystem tests (secret-link, cache, S2I imagestream patterns)

### Phase 5: Triggers Migration
**Goal**: All 23 triggers tests (EventListeners, TriggerBindings, TriggerTemplates, Interceptors) run in Ginkgo with proper namespace isolation
**Depends on**: Phase 3
**Requirements**: TRIG-01
**Success Criteria** (what must be TRUE):
  1. Running `ginkgo run ./tests/triggers/` executes all 23 triggers tests with pass/fail results matching the Gauge triggers suite
  2. Each test creates and tears down its own resources via DeferCleanup -- no cross-test resource leaks between EventListener, TriggerBinding, and TriggerTemplate specs
**Plans**: 3 plans

Plans:
- [ ] 05-01-PLAN.md -- Port pkg/triggers/, pkg/pipelines/, pkg/k8s/ helper packages from reference repo
- [ ] 05-02-PLAN.md -- Migrate 17 EventListener tests (PIPELINES-05-TC01 through TC17) with testdata fixtures
- [ ] 05-03-PLAN.md -- Migrate TriggerBinding (3), CronJob (1), and Tutorial (2) tests

### Phase 6: Operator Migration
**Goal**: All 33 operator tests (auto-install, auto-prune, addon, RBAC, TektonConfig) run in Ginkgo with cluster-wide state-modifying tests marked Serial
**Depends on**: Phase 3
**Requirements**: OPER-01, OPER-02
**Success Criteria** (what must be TRUE):
  1. Running `ginkgo run ./tests/operator/` executes all 33 operator tests with pass/fail results matching the Gauge operator suite
  2. Tests that modify cluster-wide state (TektonConfig, addon configuration, RBAC changes) are decorated with `Serial` and `Ordered` -- verified by running with `--dry-run -v` and confirming these specs are flagged as serial
  3. Operator tests that modify TektonConfig restore the original configuration in DeferCleanup, leaving the cluster in a clean state for subsequent test areas
**Plans**: 2 plans

Plans:
- [ ] 06-01-PLAN.md -- Migrate addon, RBAC, roles, and HPA tests (10 tests)
- [ ] 06-02-PLAN.md -- Migrate auto-prune and pre/post upgrade tests (23 tests), copy pruner testdata

### Phase 7: Pipelines Core Migration
**Goal**: All 16 pipelines core tests (PipelineRuns, TaskRuns, workspaces, results) run in Ginkgo
**Depends on**: Phase 3
**Requirements**: PIPE-01
**Success Criteria** (what must be TRUE):
  1. Running `ginkgo run ./tests/pipelines/` executes all 16 pipelines core tests with pass/fail results matching the Gauge pipelines suite
  2. PipelineRun and TaskRun completion is validated using `Eventually` polling (not `time.Sleep`) with appropriate timeouts for long-running runs
**Plans**: 2 plans

Plans:
- [ ] 07-01-PLAN.md -- Migrate pkg/pipelines helpers, copy testdata, create pipelinerun and failure test specs (8 tests)
- [ ] 07-02-PLAN.md -- Create resolver test specs and copy resolver testdata (8 tests)

### Phase 8: PAC Migration
**Goal**: All 7 PAC tests run in Ginkgo with Ordered workflows for multi-step GitLab webhook configuration and validation
**Depends on**: Phase 3
**Requirements**: PAC-01, PAC-02
**Success Criteria** (what must be TRUE):
  1. Running `ginkgo run ./tests/pac/` executes all 7 PAC tests with pass/fail results matching the Gauge PAC suite
  2. PAC tests that require sequential steps (create webhook, trigger pipeline, validate result) use `Ordered` containers and execute in the correct order
  3. GitLab webhook configuration and cleanup work correctly -- webhooks created during tests are deleted in DeferCleanup even on test failure
**Plans**: 2 plans

Plans:
- [ ] 08-01-PLAN.md -- Port PAC helper functions to pkg/pac, pkg/pipelines, pkg/k8s
- [ ] 08-02-PLAN.md -- Migrate all 7 PAC test specs with Ordered/Serial containers

### Phase 9: Remaining Areas Migration
**Goal**: All remaining small test areas (chains, results, MAG, metrics, versions, OLM, console) are migrated, completing 100% test coverage
**Depends on**: Phase 3
**Requirements**: MISC-01, MISC-02, MISC-03, MISC-04, MISC-05, MISC-06, MISC-07
**Success Criteria** (what must be TRUE):
  1. Running `ginkgo run ./tests/chains/ ./tests/results/ ./tests/mag/ ./tests/metrics/ ./tests/versions/ ./tests/olm/` executes all 13 remaining tests with correct pass/fail results
  2. Every migrated test across all areas has a Polarion test case ID in its test name and produces a JUnit XML entry
  3. The total test count across all 11 areas matches the expected 110+ OpenShift-specific tests (excluding the ~25 dropped upstream tests)
**Plans**: 3 plans

Plans:
- [ ] 09-01-PLAN.md — Migrate chains (2 tests) and results (2 tests) with helper packages and testdata
- [ ] 09-02-PLAN.md — Migrate MAG (2 tests) and metrics (1 test) with helper packages and testdata
- [ ] 09-03-PLAN.md — Migrate versions (2 tests), OLM (3 tests), and console icon (1 test) with helper packages

### Phase 10: CI and Reporting
**Goal**: The Ginkgo test suite runs in CI with a purpose-built Docker image, label-based filtering, diagnostic collection on failure, parallel execution, and proper timeout configuration
**Depends on**: Phase 4, Phase 5, Phase 6, Phase 7, Phase 8, Phase 9 (all test migrations complete)
**Requirements**: CI-01, CI-02, CI-03, CI-04, CI-05, CI-06, CI-07
**Success Criteria** (what must be TRUE):
  1. A CI pipeline runs the full Ginkgo suite using a Docker image that contains the `ginkgo` CLI binary (no `gauge` binary) and succeeds end-to-end
  2. CI pipelines can filter tests by label -- `ginkgo run --label-filter="sanity"` runs only sanity tests, `--label-filter="e2e && !disconnected"` excludes disconnected tests
  3. When a test fails in CI, the JUnit report includes diagnostic information (pod logs, events, resource state) attached via `ReportAfterEach` / `AddReportEntry`
  4. Running with `ginkgo run -p` (parallel mode) completes the suite faster than sequential mode with `SynchronizedBeforeSuite` initializing clients once and namespace-per-process preventing collisions
  5. The suite has an explicit `--timeout` set based on measured baseline runtime, and `--fail-on-focused` prevents accidental `FDescribe`/`FIt` commits from passing CI
**Plans**: 3 plans

Plans:
- [ ] 10-01-PLAN.md -- Docker image with Ginkgo CLI and test runner wrapper script
- [ ] 10-02-PLAN.md -- Diagnostic collection (ReportAfterEach) and JUnit-to-Polarion post-processor
- [ ] 10-03-PLAN.md -- SynchronizedBeforeSuite upgrade for parallel execution across all 11 suites

### Phase 11: Parity Validation
**Goal**: Proven equivalence between Ginkgo and Gauge suites -- same test count, same results, same label behavior, same Polarion output
**Depends on**: Phase 10
**Requirements**: PAR-01, PAR-02, PAR-03, PAR-04
**Success Criteria** (what must be TRUE):
  1. The Ginkgo suite reports the same number of tests as the Gauge suite (minus the ~25 explicitly dropped upstream tests) -- counts match when run with `--dry-run -v`
  2. Running both Ginkgo and Gauge suites against the same cluster state produces the same set of passing and failing tests (delta report is empty or contains only known/justified differences)
  3. Label filtering produces equivalent test sets -- `ginkgo run --label-filter=sanity` matches `gauge run --tags sanity` in test count and test names
  4. The Polarion uploader accepts the Ginkgo JUnit XML and creates the same test case entries as the Gauge JUnit XML -- verified by comparing Polarion import reports from both suites
**Plans**: 2 plans

Plans:
- [ ] 11-01-PLAN.md -- Structural parity: test count comparison, dropped test manifest, label filtering equivalence
- [ ] 11-02-PLAN.md -- Runtime parity: pass/fail delta report, Polarion JUnit XML validation, final parity report

## Progress

**Execution Order:**
Phases 1-3 are strictly sequential. Phases 4-9 depend on Phase 3 and can be worked in any order. Phase 10 requires all of 4-9. Phase 11 requires Phase 10.

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Foundation Repair | 2/2 | Complete | 2026-04-02 |
| 2. Suite Scaffolding | 3/3 | Complete | 2026-04-02 |
| 3. Sanity Test Migration | 2/2 | Complete | 2026-04-02 |
| 4. Ecosystem Task Migration | 0/TBD | Not started | - |
| 5. Triggers Migration | 0/3 | Not started | - |
| 6. Operator Migration | 0/TBD | Not started | - |
| 7. Pipelines Core Migration | 0/TBD | Not started | - |
| 8. PAC Migration | 0/2 | Not started | - |
| 9. Remaining Areas Migration | 0/3 | Not started | - |
| 10. CI and Reporting | 0/3 | Not started | - |
| 11. Parity Validation | 0/2 | Not started | - |
