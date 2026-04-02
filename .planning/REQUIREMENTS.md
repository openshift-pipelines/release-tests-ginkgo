# Requirements: OpenShift Pipelines Ginkgo Migration

**Defined:** 2026-04-01
**Core Value:** Every OpenShift-specific release test that currently passes in Gauge must pass identically in Ginkgo, with the same cluster coverage and JUnit XML output for Polarion.

## v1 Requirements

### Foundation

- [x] **FOUND-01**: Fix dual store import — `pkg/oc` must reference local `pkg/store`, not the original Gauge repo's store
- [x] **FOUND-02**: Rename module path from `github.com/srivickynesh/release-tests-ginkgo` to `github.com/openshift-pipelines/release-tests-ginkgo`
- [x] **FOUND-03**: Upgrade Go version from 1.23 to 1.24+
- [x] **FOUND-04**: Upgrade Ginkgo from v2.13.0 to v2.27+ and Gomega from v1.29.0 to v1.38+
- [x] **FOUND-05**: Clone original `openshift-pipelines/release-tests` repo as local reference source
- [x] **FOUND-06**: Remove or replace any transitive Gauge dependencies from go.mod

### Suite Structure

- [x] **SUITE-01**: Create `suite_test.go` entry points for each of the 11 test areas (ecosystem, triggers, operator, pipelines, PAC, chains, results, MAG, metrics, versions, OLM)
- [x] **SUITE-02**: Implement `BeforeSuite` for cluster client initialization via `pkg/clients`
- [x] **SUITE-03**: Implement `AfterSuite` for global cleanup
- [x] **SUITE-04**: Implement Label system mapping Gauge tags — `Label("sanity")`, `Label("smoke")`, `Label("e2e")`, `Label("disconnected")`
- [x] **SUITE-05**: Implement `DeferCleanup` pattern for automatic resource teardown co-located with creation
- [x] **SUITE-06**: Implement `Eventually`/`Consistently` async assertions replacing sleep-based polling
- [x] **SUITE-07**: Implement `Ordered` containers for multi-step workflow tests (replacing Gauge's implicit step ordering)
- [x] **SUITE-08**: Implement `DescribeTable`/`Entry` for data-driven tests (replacing Gauge data tables)
- [x] **SUITE-09**: Implement `Skip` decorator for conditional tests (disconnected cluster, arch-specific)

### Test Migration — Sanity

- [x] **SNTY-01**: Migrate ~9 sanity tests with side-by-side validation against Gauge results
- [x] **SNTY-02**: Validate JUnit XML output is compatible with Polarion uploader using sanity test results
- [x] **SNTY-03**: Preserve Polarion test case IDs (e.g., `PIPELINES-XX-TCXX`) in Ginkgo test names

### Test Migration — Ecosystem Tasks

- [ ] **ECO-01**: Migrate ~36 ecosystem task tests (buildah, s2i, git-clone, etc.) using `DescribeTable`/`Entry`
- [ ] **ECO-02**: Validate data table parameter passing works correctly (tree construction time evaluation)

### Test Migration — Triggers

- [ ] **TRIG-01**: Migrate ~23 triggers tests (EventListeners, TriggerBindings, TriggerTemplates, etc.)

### Test Migration — Operator

- [ ] **OPER-01**: Migrate ~33 operator tests (auto-install, auto-prune, addon, RBAC, TektonConfig, etc.)
- [ ] **OPER-02**: Mark cluster-wide state-modifying tests with `Serial` decorator

### Test Migration — Pipelines Core

- [ ] **PIPE-01**: Migrate ~16 pipelines core tests (PipelineRuns, TaskRuns, workspaces, etc.)

### Test Migration — PAC

- [ ] **PAC-01**: Expand existing PAC test scaffolding to cover all ~7 PAC tests
- [ ] **PAC-02**: Migrate GitLab webhook configuration and validation tests

### Test Migration — Remaining Areas

- [ ] **MISC-01**: Migrate ~2 chains tests
- [ ] **MISC-02**: Migrate ~2 results tests
- [ ] **MISC-03**: Migrate ~2 manual approval gate tests
- [ ] **MISC-04**: Migrate ~1 metrics test
- [ ] **MISC-05**: Migrate ~2 versions/sanity tests
- [ ] **MISC-06**: Migrate ~3 OLM/install tests
- [ ] **MISC-07**: Migrate ~1 console icon test

### CI & Reporting

- [ ] **CI-01**: Create Docker image with `ginkgo` CLI replacing `gauge` binary
- [ ] **CI-02**: Configure `ginkgo run` with label filtering (`--label-filter`)
- [ ] **CI-03**: JUnit XML post-processing for Polarion compatibility (format transformation if needed)
- [ ] **CI-04**: Implement `ReportAfterEach` for diagnostic collection on failure (pod logs, events, resource state)
- [ ] **CI-05**: Enable parallel test execution with `SynchronizedBeforeSuite` and namespace-per-process isolation
- [ ] **CI-06**: Set explicit suite timeout (`--timeout`) based on baseline runtime measurement
- [ ] **CI-07**: Configure `--fail-on-focused` to prevent `FDescribe`/`FIt` from being committed

### Parity Validation

- [ ] **PAR-01**: Same test count — Ginkgo suite reports same number of tests as Gauge suite (minus dropped upstream tests)
- [ ] **PAR-02**: Same pass/fail results on identical cluster state
- [ ] **PAR-03**: Label filtering equivalence — `ginkgo run --label-filter=sanity` matches `gauge run --tags sanity`
- [ ] **PAR-04**: JUnit XML compatible with Polarion uploader (same test case IDs, same format)

## v2 Requirements

### Dependency Upgrades (post-migration)

- **DEP-01**: Upgrade Tekton dependencies (pipeline v0.68→v1.6+, operator v0.75→v0.78+)
- **DEP-02**: Migrate deprecated GitLab client (`xanzy/go-gitlab` → `gitlab.com/gitlab-org/api/client-go`)
- **DEP-03**: Upgrade Kubernetes client dependencies to latest LTS

### Advanced Features

- **ADV-01**: `DescribeTableSubtree` for complex data-driven tests needing multiple assertions per entry
- **ADV-02**: `MustPassRepeatedly` for stability validation during development
- **ADV-03**: Custom report entries for CI dashboards (HTML reports, Slack notifications)
- **ADV-04**: Ginkgo CLI wrapper script (`scripts/run-tests.sh`) for standardized invocation

## Out of Scope

| Feature | Reason |
|---------|--------|
| Upstream Tekton tests (~25) | Already covered by Tekton CI in Konflux on OpenShift |
| envtest / fake control plane | Tests run against real OpenShift clusters; envtest lacks operator behavior |
| OpenShift Ginkgo fork | Only for `openshift-tests`, not operator test suites |
| Global `--flake-attempts` | Masks real bugs (Kubernetes/Podman learned this painfully) |
| Custom Ginkgo reporter interface | Removed in Ginkgo v2; use `ReportAfterEach`/`ReportAfterSuite` instead |
| Separate spec files | Ginkgo keeps spec and implementation together; no BDD separation layer |
| Gauge framework maintenance | This repo is Ginkgo-only |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| FOUND-01 | Phase 1 | Complete |
| FOUND-02 | Phase 1 | Complete |
| FOUND-03 | Phase 1 | Complete |
| FOUND-04 | Phase 1 | Complete |
| FOUND-05 | Phase 1 | Complete |
| FOUND-06 | Phase 1 | Complete |
| SUITE-01 | Phase 2 | Complete |
| SUITE-02 | Phase 2 | Complete |
| SUITE-03 | Phase 2 | Complete |
| SUITE-04 | Phase 2 | Complete |
| SUITE-05 | Phase 2 | Complete |
| SUITE-06 | Phase 2 | Complete |
| SUITE-07 | Phase 2 | Complete |
| SUITE-08 | Phase 2 | Complete |
| SUITE-09 | Phase 2 | Complete |
| SNTY-01 | Phase 3 | Complete |
| SNTY-02 | Phase 3 | Complete |
| SNTY-03 | Phase 3 | Complete |
| ECO-01 | Phase 4 | Pending |
| ECO-02 | Phase 4 | Pending |
| TRIG-01 | Phase 5 | Pending |
| OPER-01 | Phase 6 | Pending |
| OPER-02 | Phase 6 | Pending |
| PIPE-01 | Phase 7 | Pending |
| PAC-01 | Phase 8 | Pending |
| PAC-02 | Phase 8 | Pending |
| MISC-01 | Phase 9 | Pending |
| MISC-02 | Phase 9 | Pending |
| MISC-03 | Phase 9 | Pending |
| MISC-04 | Phase 9 | Pending |
| MISC-05 | Phase 9 | Pending |
| MISC-06 | Phase 9 | Pending |
| MISC-07 | Phase 9 | Pending |
| CI-01 | Phase 10 | Pending |
| CI-02 | Phase 10 | Pending |
| CI-03 | Phase 10 | Pending |
| CI-04 | Phase 10 | Pending |
| CI-05 | Phase 10 | Pending |
| CI-06 | Phase 10 | Pending |
| CI-07 | Phase 10 | Pending |
| PAR-01 | Phase 11 | Pending |
| PAR-02 | Phase 11 | Pending |
| PAR-03 | Phase 11 | Pending |
| PAR-04 | Phase 11 | Pending |

**Coverage:**
- v1 requirements: 42 total
- Mapped to phases: 42
- Unmapped: 0

---
*Requirements defined: 2026-04-01*
*Last updated: 2026-04-01 after initial definition*
