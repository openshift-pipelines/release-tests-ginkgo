# Project Research Summary

**Project:** Gauge-to-Ginkgo Migration for OpenShift Pipelines E2E Tests
**Domain:** Test framework migration for Kubernetes/OpenShift operator validation
**Researched:** 2026-03-31
**Confidence:** HIGH

## Executive Summary

This project migrates 110+ end-to-end tests for OpenShift Pipelines (Tekton) from Gauge to Ginkgo v2. The migration is not just a test runner swap -- it requires fundamental architectural changes from Gauge's global state model to Ginkgo's closure-based pattern, while maintaining compatibility with existing Polarion test management and CI infrastructure. Research reveals that the primary technical challenges are not in the test framework itself (Ginkgo v2 is well-documented and widely adopted in the Kubernetes ecosystem), but in correcting existing structural problems in the codebase: dual store packages, broken import paths, and global mutable state that will break parallelization.

The recommended approach follows a four-phase migration strategy: (1) fix foundational issues (import paths, store architecture, suite scaffolding), (2) migrate test areas incrementally starting with sanity tests to validate patterns, (3) enable parallel execution and CI optimization, and (4) upgrade dependencies and complete cleanup. This order is critical -- attempting test migration before fixing the dual-store corruption and import dependencies will result in silent failures and resource leaks.

Key risks include JUnit XML incompatibility with Polarion (verified as a known issue in similar projects), Tekton Results finalizers causing 300-second cleanup timeouts that cascade across the suite, and global state pollution breaking test isolation. Mitigation requires early validation of the Polarion pipeline with sample tests, implementing async deletion patterns with explicit timeouts, and strict enforcement of Ginkgo's "declare in containers, initialize in BeforeEach" pattern from the first migrated test.

## Key Findings

### Recommended Stack

The migration does not require adding new dependencies -- Ginkgo v2 and Gomega provide all functionality that Gauge offered plus additional capabilities for parallelization, label-based filtering, and async assertions. The current stack is outdated (Ginkgo v2.13.0, 13 minor versions behind; Gomega v1.29.0, 10 versions behind; Go 1.23.4 which lost support in January 2026; GitLab client deprecated December 2024).

**Core technologies:**
- **Ginkgo v2.27.5+ (or v2.28.1+ with Go 1.24 upgrade)**: BDD test framework with built-in label filtering, JUnit reporting, and parallel execution support -- the Kubernetes ecosystem standard used by Kubernetes upstream, kubebuilder, and all major OpenShift projects
- **Gomega v1.38.3+ (or v1.39.1+ with Go 1.24 upgrade)**: Matcher library with `Eventually()` and `Consistently()` for async Kubernetes resource assertions, essential for polling-based validation
- **Go 1.24+**: Upgrade from Go 1.23.4 (out of support since January 2026) to enable latest Ginkgo/Gomega versions; Go 1.24.13 is current stable
- **Ginkgo CLI**: Test runner binary required for `--label-filter`, `--junit-report`, and parallel execution (`-p`) -- `go test` alone lacks these features

**Critical version constraint:** Ginkgo v2.28.0+ and Gomega v1.39.0+ require Go 1.24. The project must upgrade Go during Phase 1 or stay on Ginkgo v2.27.5 / Gomega v1.38.3 (last Go 1.23 compatible releases).

**Action required:** Migrate deprecated `github.com/xanzy/go-gitlab` (EOL December 2024) to `gitlab.com/gitlab-org/api/client-go` v1.x. This should be a separate post-migration change to avoid interleaving framework migration with API updates.

**No upgrades needed for Tekton dependencies during migration.** Keep tektoncd/pipeline v0.68.0, tektoncd/operator v0.75.0, and k8s.io/client-go v0.29.6 at current versions. Mixing test framework changes with API version bumps creates unnecessary risk. Upgrade Tekton deps in Phase 4 (post-migration).

### Expected Features

The Ginkgo test suite must achieve feature parity with the existing Gauge suite while enabling new capabilities for CI optimization and parallel execution.

**Must have (table stakes):**
- **Suite entry points** (`suite_test.go` per test area) -- without these, tests won't run via Ginkgo runner and lose all framework features
- **Describe/Context/It hierarchy with Ordered containers** -- maps directly from Gauge spec/scenario/step structure; Ordered decorator required for multi-step sequential workflows (PAC, operator config tests)
- **Label system (sanity/smoke/e2e/disconnected)** -- direct replacement for Gauge tags; CI pipelines filter via `--label-filter="sanity"` or `--label-filter="e2e && !disconnected"`
- **JUnit XML reporting with Polarion compatibility** -- must include `<properties>` block, correct `classname` format, and test case IDs (e.g., `PIPELINES-30-TC01`) for Polarion JUMP tool import
- **BeforeSuite/AfterSuite lifecycle hooks** -- suite-level cluster client initialization and teardown; per-spec initialization would be prohibitively expensive
- **DeferCleanup for resource management** -- every test creates Kubernetes resources (namespaces, pipelines, triggers) that must be deleted even on failure; DeferCleanup ensures LIFO cleanup order and context-aware execution
- **Eventually/Consistently async assertions** -- replace `time.Sleep` polling with Gomega's purpose-built async matchers for waiting on PipelineRun completion, pod readiness, deployment rollout
- **DescribeTable for data-driven tests** -- 36 ecosystem task tests use Gauge data tables; DescribeTable provides direct equivalent with Entry(...) generating separate JUnit entries

**Should have (differentiators over Gauge):**
- **Parallel test execution** -- Gauge runs tests sequentially; Ginkgo with `ginkgo run -p` can run independent test areas in parallel, potentially reducing CI time by 6x (verified in similar Kubernetes projects)
- **SynchronizedBeforeSuite** -- enables parallel execution by initializing cluster clients once on node 1 and broadcasting config to all parallel nodes, avoiding N redundant client initializations
- **Namespace-per-test isolation** -- each test gets its own namespace, preventing resource name collisions and cross-test interference (critical for parallelism)
- **Custom report entries for diagnostics** -- attach cluster state, pod logs, and event dumps to failed test reports via `AddReportEntry()` (Gauge had no equivalent)
- **Spec timeout decorators** -- individual specs can have timeouts independent of suite timeout, preventing one hung test from blocking the entire suite
- **Progress reporting** -- long-running tests (10+ minute operator installs) emit periodic progress updates via `--progress` flag

**Defer (Phase 4+):**
- **Global FlakeAttempts** -- DO NOT use `--flake-attempts` flag globally (masks real bugs, as Kubernetes and Podman projects learned); use targeted `FlakeAttempts(N)` decorator on specific known-flaky specs only
- **GitLab client migration** -- existing `xanzy/go-gitlab` is deprecated but still functional; migrate to `gitlab.com/gitlab-org/api/client-go` as a separate focused effort post-migration
- **Tekton dependency upgrades** -- keep current LTS versions (pipeline v0.68.0, operator v0.75.0) during migration; upgrade to latest LTS (operator v0.78.x, pipeline v1.6.x) after framework migration completes

### Architecture Approach

The test suite follows a layered test framework pattern used by mature Kubernetes operator test suites (OpenShift HyperShift, OCS Operator, Kubernetes upstream e2e): test specifications at the top, test framework (lifecycle management) in the middle, helper utilities and client abstraction at the bottom, with configuration and test fixtures as leaves.

**Major components:**
1. **Suite Bootstrap Layer** (`tests/<area>/suite_test.go`) -- registers Ginkgo suite with `TestXxx(t)` + `RunSpecs`; houses `BeforeSuite`/`AfterSuite` for one-time cluster client initialization; communicates with test specifications and framework
2. **Test Specification Layer** (`tests/<area>/*_test.go`) -- BDD specs using `Describe`/`It`/`DescribeTable` for each test area; owns labels and test logic; calls framework and helpers
3. **Test Framework Layer** (`pkg/framework/` - NEW) -- per-test namespace creation/deletion, shared client distribution, fixture loading, common assertions, `DeferCleanup` registration; bridges clients and helpers
4. **Helper Utilities** (`pkg/oc/`, `pkg/opc/`, `pkg/pac/`, `pkg/k8s/` - NEW) -- domain-specific operations for OpenShift CLI, Tekton CLI, PAC/GitLab setup, Kubernetes resource validation
5. **Client Abstraction** (`pkg/clients/`) -- creates and holds typed Kubernetes, Tekton, OpenShift, OLM clientsets; initialized once per suite, reused across specs
6. **Configuration** (`pkg/config/`) -- constants, environment flags, timeout values; leaf component with no dependencies
7. **State Management** (`pkg/store/` - REFACTOR REQUIRED) -- thread-safe scenario/suite data storage inherited from Gauge; must be refactored away from global mutable state to closure-based pattern for test isolation and parallelism

**Critical architectural decision:** The directory structure must have one `suite_test.go` per test area (ecosystem, triggers, operator, pipelines, PAC, chains, results, MAG, metrics, versions, OLM). This enables running areas independently (`ginkgo run ./tests/operator/...`) and parallelizing across areas while keeping tests within an area sequential when dependencies exist.

**Data flow:** Suite initialization loads kubeconfig, creates shared clients in `BeforeSuite`, verifies cluster connectivity. Per-test execution uses `BeforeEach` to generate unique namespace, create namespace-scoped clients, register `DeferCleanup` for automatic teardown. Test specs load YAML fixtures from `testdata/`, create resources via `oc.Create()` or client API, validate state with `Eventually()`, assert results with Gomega matchers. Cleanup runs in LIFO order via `DeferCleanup`, deleting resources and namespaces even on failure.

### Critical Pitfalls

The research identified 17 pitfalls across critical/moderate/minor severity. These are the top 5 that will block migration if not addressed in Phase 1.

1. **Dual Store Package Creates Silent State Corruption** -- `pkg/oc/oc.go` imports the old Gauge repo's store (`github.com/openshift-pipelines/release-tests/pkg/store`) while test code uses the local store (`pkg/store`), causing namespace resolution to return empty strings and resource operations to fail silently. Must audit all imports of `release-tests/pkg/*` and replace with local equivalents before writing tests. This is the #1 migration blocker.

2. **Global Mutable State in Package Variables Breaks Parallel Execution** -- `pkg/store` uses package-level `var scenarioStore` and `var suiteStore` maps; `pkg/pac` uses package-level `var client *gitlab.Client`. Mutations from one spec leak into the next, and parallel execution causes `store.Clients()` to return nil and `store.Opc()` to panic. Must refactor to closure-scoped variables in `BeforeEach` blocks or create `ClearScenarioStore()` cleanup mechanism.

3. **Container-Level Variable Initialization Causes Spec Pollution** -- developers migrating from Gauge naturally initialize variables inside `Describe`/`Context` blocks (container nodes), but Ginkgo evaluates container closures once during tree construction, sharing the variable across all child specs. Must establish "declare in containers, initialize in `BeforeEach`" pattern and enforce via code review. Enable `--randomize-all` from day one to surface pollution immediately.

4. **Ginkgo v2 Default Timeout of 1 Hour Silently Kills E2E Suites** -- Ginkgo v2 changed default suite timeout from 24 hours to 1 hour (entire suite, not per-spec). With Tekton Results finalizers (150+ second waits), operator reconciliation, and 110+ tests, total runtime can exceed 1 hour. When timeout fires, Ginkgo cancels all contexts including cleanup callbacks, leaving orphaned namespaces. Must set `--timeout=4h` explicitly in CI and add per-spec timeouts via `SpecTimeout()`.

5. **JUnit XML Output Incompatible with Polarion Uploader** -- Ginkgo's `--junit-report` output lacks the `<properties>` section Polarion requires, has `<system-out>` elements absent from passed test cases, and formats test names as hierarchical paths instead of flat test IDs. Polarion import succeeds but shows zero completed tests or rejects with "Project id not specified." Must build post-processing transform step or use `ReportAfterSuite` with custom `JunitReportConfig`. Validate with sample tests in Phase 1, not after all 110 tests are migrated.

**Additional critical pitfall:** Tekton Results finalizers cause 300-second cleanup timeouts. With 110+ tests each creating and deleting resources, cumulative cleanup time alone can be 30+ minutes. Must implement async deletion with `Eventually()` polling rather than synchronous `oc delete` blocking, and account for cleanup time in suite timeout budget (add 1 minute per test as buffer).

## Implications for Roadmap

Based on research, the migration requires a strict sequential phase structure due to foundational dependencies. Test areas within Phase 2 can be migrated in parallel, but Phases 1, 2, 3, and 4 must execute in order.

### Phase 1: Foundation and Infrastructure

**Rationale:** The existing codebase has structural issues that prevent any test migration from succeeding: dual store packages with broken import paths, global mutable state that breaks test isolation, missing suite bootstrapping, and Go/Ginkgo versions significantly out of date. These must be fixed first or every migrated test will fail with silent namespace corruption, state pollution, or panic on store access.

**Delivers:**
- Fixed import paths (remove all `release-tests/pkg/*` dependencies from `pkg/oc/`, `pkg/cmd/`, etc.)
- Upgraded Go toolchain to 1.24+ and Ginkgo/Gomega to latest compatible versions
- Created `suite_test.go` entry points for all 11 test areas (ecosystem, triggers, operator, pipelines, PAC, chains, results, MAG, metrics, versions, OLM)
- Refactored `pkg/store` to add `ClearScenarioStore()` or migrated to closure-based state pattern
- Created `pkg/framework/` package for namespace lifecycle management and `TestContext` struct
- Established `testdata/` directory structure mirroring test areas
- Validated JUnit-to-Polarion pipeline with sample test output
- Configured CI with explicit `--timeout=4h`, `--fail-on-focused`, `--randomize-all` flags

**Addresses (from FEATURES.md):**
- Suite entry points per area (table stakes)
- BeforeSuite/AfterSuite lifecycle hooks (table stakes)
- JUnit XML reporting with Polarion compatibility (table stakes)

**Avoids (from PITFALLS.md):**
- Pitfall #1: Dual store package corruption
- Pitfall #2: Global mutable state breaking parallel execution
- Pitfall #3: Container-level variable initialization pollution (by establishing pattern early)
- Pitfall #5: Ginkgo v2 default timeout killing suites
- Pitfall #6: JUnit XML incompatibility (validated early)
- Pitfall #8: Missing suite bootstrap files
- Pitfall #14: Module path mismatch (update to `github.com/openshift-pipelines/release-tests-ginkgo`)

**Critical path items:**
- Fix `pkg/oc/oc.go` imports BEFORE any test uses `oc.DeleteResource()`
- Create all 11 `suite_test.go` files BEFORE writing any `*_test.go` specs
- Validate Polarion pipeline with 2-3 sample tests BEFORE migrating full areas

### Phase 2: Incremental Test Area Migration

**Rationale:** With foundation in place, test areas can be migrated incrementally. Start with sanity/versions tests (smallest, simplest) to validate framework patterns, then tackle ecosystem tasks (largest count, heavy DescribeTable usage), then remaining areas in dependency order. Each area is independent -- ecosystem tests don't depend on operator tests -- enabling parallel migration by multiple contributors if desired.

**Delivers:**
- Migrated sanity/versions tests (9 tests) -- validates framework with minimal complexity
- Migrated ecosystem tasks (36 tests) -- validates DescribeTable pattern and resource cleanup at scale
- Migrated triggers tests (23 tests) -- validates namespace isolation and EventListener patterns
- Migrated operator tests (33 tests) -- validates Ordered containers and TektonConfig modification
- Migrated pipelines core tests (16 tests) -- validates standard CRUD patterns
- Migrated PAC tests (7 tests) -- validates complex Ordered workflows and GitLab integration
- Migrated chains, results, MAG, metrics, OLM tests (remaining ~13 tests)

**Migration order within Phase 2:**
1. **Versions/Sanity** (9 tests) -- simplest, no complex dependencies, perfect framework validation
2. **Ecosystem** (36 tests) -- validates DescribeTable heavily, tests cleanup patterns at scale
3. **Triggers** (23 tests) -- moderate complexity, standard namespace + EventListener pattern
4. **Pipelines** (16 tests) -- standard CRUD operations, validates core framework
5. **Operator** (33 tests) -- complex Ordered containers, cluster-wide TektonConfig modifications, Serial decorator usage
6. **PAC** (7 tests) -- most complex Ordered workflows, GitLab API integration
7. **Chains, Results, MAG, Metrics, OLM** (13 tests total) -- small independent areas, low risk

**Addresses (from FEATURES.md):**
- Describe/Context/It hierarchy with Ordered containers (table stakes)
- Label system (sanity/smoke/e2e/disconnected) (table stakes)
- DescribeTable for data-driven tests (table stakes)
- Eventually/Consistently async assertions (table stakes)
- DeferCleanup for resource cleanup (table stakes)
- Serial decorator for cluster-modifying tests (table stakes)
- Test case ID preservation in test names (table stakes)

**Avoids (from PITFALLS.md):**
- Pitfall #3: Container-level initialization (enforced via code review per test area)
- Pitfall #4: DescribeTable entry timing (validated in ecosystem tests Phase 2.2)
- Pitfall #7: Tekton Results finalizer timeouts (async deletion pattern prototyped in ecosystem, applied to all areas)
- Pitfall #11: Context cancellation in DeferCleanup (pattern established in sanity tests)

**Uses (from STACK.md):**
- Ginkgo v2 `Describe`, `It`, `DescribeTable`, `Entry`, `Ordered`, `Serial`, `Label` decorators
- Gomega `Eventually`, `Consistently`, `Expect`, matchers
- Existing `pkg/clients`, `pkg/oc`, `pkg/opc`, `pkg/cmd` helpers (now with correct local imports)

**Implements (from ARCHITECTURE.md):**
- Test Specification Layer (all 11 test areas)
- Per-test namespace with DeferCleanup pattern
- Ordered container for sequential workflows
- DescribeTable for parameterized tests
- Label-based test filtering

### Phase 3: CI Optimization and Parallel Execution

**Rationale:** With all tests migrated and stable, enable parallel execution to reduce CI time. Parallelism requires namespace isolation (already implemented in Phase 2), SynchronizedBeforeSuite for shared client initialization, and careful handling of cluster-wide state (operator config tests must remain Serial). This phase also adds diagnostic collection on failure and progress reporting for long-running tests.

**Delivers:**
- `SynchronizedBeforeSuite` pattern for parallel-safe client initialization
- Namespace isolation per parallel node (using `GinkgoParallelProcess()` in namespace names)
- Parallel execution enabled via `ginkgo run -p` or `--procs=N` in CI
- Progress reporting via `--progress` flag
- Diagnostic collection on failure via `ReportAfterEach` (pod logs, events, resource state via `AddReportEntry`)
- FlakeAttempts applied to specific known-flaky tests (targeted, not global `--flake-attempts`)
- Ginkgo CLI wrapper script (`scripts/run-tests.sh`) standardizing invocation

**Addresses (from FEATURES.md):**
- Parallel test execution (differentiator)
- SynchronizedBeforeSuite (differentiator)
- Namespace-per-test isolation (differentiator)
- Custom report entries for diagnostics (differentiator)
- Progress reporting (differentiator)

**Avoids (from PITFALLS.md):**
- Pitfall #2: Global mutable state (final validation with parallel execution and `--race`)
- Pitfall #9: Label filtering ignoring FIt/FDescribe (enforced via `--fail-on-focused`)
- Pitfall #10: BeforeSuite running for filtered-out specs (SynchronizedBeforeSuite keeps setup lightweight)

**Performance targets:**
- Current suite runtime: 30-60 minutes sequential
- Target with parallelism: 10-15 minutes (6x improvement based on similar Kubernetes projects)
- Namespace cleanup: async deletion reduces per-test cleanup from 5 minutes to <1 minute

### Phase 4: Dependency Upgrades and Cleanup

**Rationale:** With the test framework migration complete and CI stable, upgrade Tekton/K8s dependencies and migrate the deprecated GitLab client. Keeping these separate from the framework migration prevents interleaving two large changes and allows proper regression testing after each upgrade.

**Delivers:**
- Upgraded Tekton dependencies: operator v0.78.x (latest LTS, EOL 2026-12-08), pipeline v1.6.x
- Updated k8s.io replace directives to match new Tekton deps (v0.31.x)
- Migrated `github.com/xanzy/go-gitlab` to `gitlab.com/gitlab-org/api/client-go` v1.x
- Updated module path from `github.com/srivickynesh/release-tests-ginkgo` to final org path (if needed)
- Removed Gauge dependency (`github.com/openshift-pipelines/release-tests`) from go.mod entirely
- Updated PAC dependency to latest (v0.41.1) if API types changed
- Comprehensive regression testing of all 110+ tests against new dependency versions

**Addresses (from STACK.md):**
- GitLab client migration (action required)
- Tekton operator/pipeline/triggers upgrade to latest LTS
- k8s.io/client-go upgrade to match Tekton deps
- Module path correction

**Avoids (from STACK.md anti-pattern):**
- Mixing test framework migration with API version bumps
- Dependency version drift that could introduce flakiness

### Phase Ordering Rationale

**Why this strict sequential order:**
- **Phase 1 MUST come first:** The dual store package corruption (Pitfall #1) breaks every test that uses `oc.DeleteResource()`. The missing `suite_test.go` files (Pitfall #8) prevent tests from running via Ginkgo at all. The module path mismatch (Pitfall #14) blocks proper imports. Until these are fixed, no test migration can succeed.
- **Phase 2 depends on Phase 1:** Tests need correct suite bootstrapping, fixed imports, and established patterns (declare in containers, initialize in BeforeEach) from day one. Without the JUnit-to-Polarion validation from Phase 1, teams risk migrating all 110 tests only to discover the output format is incompatible.
- **Phase 3 depends on Phase 2:** Parallelism requires all tests to be Ginkgo-native and using proper namespace isolation. Attempting parallel execution while tests are still mid-migration creates confusion (which failures are migration bugs vs. parallelism race conditions?).
- **Phase 4 depends on Phase 3:** Dependency upgrades introduce API changes and potential behavioral differences. Running these upgrades while tests are still being migrated makes it impossible to determine whether failures are framework bugs or dependency issues.

**Why test areas within Phase 2 are independent:**
- Ecosystem tests don't depend on operator tests; triggers tests don't depend on pipelines tests. Each area operates in its own namespace against independent resources.
- Multiple contributors can migrate different areas in parallel branches, merging incrementally.
- Starting with sanity tests (smallest) validates the framework before tackling ecosystem tests (largest, most complex DescribeTable usage).
- PAC tests come last within Phase 2 because they have the most complex Ordered workflows and GitLab integration -- best tackled once patterns are proven in simpler tests.

**How this avoids pitfalls discovered in research:**
- Early validation of JUnit-to-Polarion pipeline (Phase 1) prevents discovering incompatibility after 110 tests are migrated.
- Establishing "declare in containers, initialize in BeforeEach" pattern in first tests (Phase 1) and enforcing via code review prevents Pitfall #3 from spreading.
- Prototyping async deletion pattern in ecosystem tests (Phase 2.2, 36 tests with heavy resource creation) validates the approach before applying to all areas.
- Deferring parallelism to Phase 3 prevents race condition bugs from contaminating the migration process.
- Separating dependency upgrades (Phase 4) from framework migration (Phases 1-2) creates clean rollback points if regressions occur.

### Research Flags

Phases likely needing deeper research during planning:

- **Phase 2.6: PAC Tests** -- GitLab API integration patterns may need additional research for Ginkgo-specific mocking or fixture strategies; existing `pkg/pac` package uses global client variable that must be refactored
- **Phase 3: Parallel Execution** -- `SynchronizedBeforeSuite` pattern with OPC binary download and cluster client marshaling needs implementation research; namespace isolation with `GinkgoParallelProcess()` may need tuning based on cluster resource limits
- **Phase 4: Tekton Dependency Upgrades** -- API changes between tektoncd/pipeline v0.68.0 -> v1.6.x may require test updates; replace directives for k8s.io packages need careful version alignment

Phases with standard patterns (skip research-phase):

- **Phase 1: Foundation** -- well-documented Ginkgo v2 migration patterns; suite_test.go scaffolding is standard Go test setup; import path fixing is mechanical
- **Phase 2.1: Sanity/Versions Tests** -- simple CRUD patterns with no complex dependencies; direct Gauge-to-Ginkgo mapping
- **Phase 2.2-2.5: Ecosystem/Triggers/Pipelines/Operator** -- standard Kubernetes E2E patterns widely documented; DescribeTable usage is well-established; Ordered containers have official Ginkgo docs

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Ginkgo/Gomega versions verified via official GitHub releases; Tekton versions cross-referenced with operator LTS releases; GitLab client deprecation confirmed via official redirect notice; no new dependencies required |
| Features | HIGH | Feature requirements verified against official Ginkgo v2 documentation and Kubernetes E2E best practices; table stakes identified via analysis of existing Gauge test structure (110 tests, 4 tag categories); Polarion requirements confirmed via Submariner project issue #48 |
| Architecture | HIGH | Layered architecture pattern verified across multiple mature Kubernetes projects (HyperShift, OCS Operator, Kubernetes upstream); directory structure follows Go test conventions and Ginkgo best practices; data flow validated against existing `pkg/` helper implementation |
| Pitfalls | HIGH | All critical pitfalls verified via direct codebase inspection (`pkg/store/store.go`, `pkg/oc/oc.go` imports), official Ginkgo issue tracker (#440, #530, #1221, #378), and Kubernetes E2E migration docs; Polarion JUnit incompatibility documented in Submariner shipyard issue #48; Tekton Results finalizer timeouts observable in existing test runs |

**Overall confidence:** HIGH

### Gaps to Address

**1. Polarion JUnit XML transformation specifics** -- Research confirms Ginkgo's JUnit output is incompatible with Polarion JUMP importer but the exact transform required (properties format, test ID mapping, system-out placement) needs validation against the actual Polarion instance used by OpenShift Pipelines. **Handle via:** Create sample JUnit XML from 2-3 migrated sanity tests in Phase 1, attempt Polarion upload, iterate on transform script based on actual import errors.

**2. Tekton Results finalizer configuration in test environment** -- Research identifies 150+ second deletion delays from Tekton Results `store_deadline` and `forward_buffer` settings, but optimal values for test cluster are unknown. **Handle via:** Coordinate with cluster admins to tune Tekton Results config for test environment in Phase 1; implement async deletion pattern with `Eventually()` polling as mitigation regardless of settings.

**3. OPC binary download caching strategy** -- Research identifies `SynchronizedBeforeSuite` pattern for one-time download on parallel node 1, but existing `pkg/opc/opc.go` implementation details need review to determine if download is idempotent and where to cache. **Handle via:** Review `opc.go` implementation during Phase 1 foundation work; prototype caching in Phase 3 when parallelism is enabled.

**4. Suite timeout budget with 110+ tests** -- Research recommends `--timeout=4h` but actual runtime depends on cluster performance, Tekton Results config, and parallelism level. **Handle via:** Run full sequential suite in Phase 2 completion to establish baseline runtime; calculate timeout budget as `(baseline_runtime / parallel_nodes) * 1.5` safety margin; monitor CI run durations in Phase 3.

**5. Test case ID to Polarion mapping** -- Research confirms test IDs like `PIPELINES-30-TC01` must appear in test names, but existing Gauge specs may not have consistent ID format across all 110 tests. **Handle via:** Audit existing Gauge spec files for test case ID patterns during Phase 1; establish naming convention (`Describe("Feature: PIPELINES-XX-TC##", ...)`) and apply consistently during Phase 2 migration.

## Sources

### Primary (HIGH confidence)
- [Ginkgo v2 Official Documentation](https://onsi.github.io/ginkgo/) -- Labels, Ordered, Serial, DeferCleanup, reporting, decorators, suite lifecycle
- [Ginkgo v2 Go Package Docs](https://pkg.go.dev/github.com/onsi/ginkgo/v2) -- API reference for all decorators and functions
- [Ginkgo v2 Migration Guide](https://onsi.github.io/ginkgo/MIGRATING_TO_V2) -- Timeout changes, custom reporter removal, v1-to-v2 patterns
- [Gomega Documentation](https://pkg.go.dev/github.com/onsi/gomega) -- Eventually, Consistently, matchers, async assertions
- [Kubernetes E2E Testing Best Practices, Reloaded](https://www.kubernetes.dev/blog/2023/04/12/e2e-testing-best-practices-reloaded/) -- DeferCleanup, namespace isolation, context handling
- [Ginkgo GitHub Releases](https://github.com/onsi/ginkgo/releases) -- v2.28.1 latest (Jan 30, 2026), Go 1.24 requirement
- [Gomega GitHub Releases](https://github.com/onsi/gomega/releases) -- v1.39.1 latest (Jan 30, 2026), Go 1.24 requirement
- [Tekton Pipeline Releases](https://github.com/tektoncd/pipeline/blob/main/releases.md) -- v1.6.1 latest LTS, v0.68.0 EOL 2026-01-30
- [Tekton Operator Releases](https://github.com/tektoncd/operator/releases) -- v0.78.x latest LTS, EOL 2026-12-08
- [xanzy/go-gitlab Deprecation](https://github.com/xanzy/go-gitlab) -- Deprecated Dec 2024, migrated to gitlab.com/gitlab-org/api/client-go
- [Go Release History](https://go.dev/doc/devel/release) -- Go 1.24.13 current stable, Go 1.23 out of support
- Codebase direct inspection: `pkg/store/store.go`, `pkg/oc/oc.go`, `pkg/pac/pac.go`, `pkg/clients/clients.go`, `tests/pac/pac_test.go`, `go.mod` (dual store package, import paths, module path mismatch, context cancellation)

### Secondary (MEDIUM confidence)
- [Submariner Shipyard Issue #48](https://github.com/submariner-io/shipyard/issues/48) -- Polarion JUnit XML parsing failure with Ginkgo output, missing properties
- [Kubernetes Ginkgo v2 Migration Umbrella Issue #109744](https://github.com/kubernetes/kubernetes/issues/109744) -- Migration patterns and lessons from Kubernetes upstream
- [Kubernetes Stop Using FlakeAttempts Issue #68091](https://github.com/kubernetes/kubernetes/issues/68091) -- Why global flake retries mask real bugs
- [Ginkgo Issue #440](https://github.com/onsi/ginkgo/issues/440) -- Shared variables in parallel execution
- [Ginkgo Issue #530](https://github.com/onsi/ginkgo/issues/530) -- Intermittent test pollution with global Describe variables
- [Ginkgo Issue #1221](https://github.com/onsi/ginkgo/issues/1221) -- Focused specs ignored with label filtering
- [Ginkgo Issue #378](https://github.com/onsi/ginkgo/issues/378) -- BeforeEach variables in DescribeTable Entry evaluated at tree construction
- [Speeding Up Kubernetes Controller Integration Tests with Ginkgo Parallelism](https://kev.fan/posts/04-k8s-ginkgo-parallel-tests/) -- SynchronizedBeforeSuite pattern, 6.5x speedup
- [OpenShift HyperShift Ginkgo v2 E2E Suite PR #7192](https://github.com/openshift/hypershift/pull/7192) -- OpenShift-specific Ginkgo patterns
- [OpenShift Service CA Operator E2E Migration PR #297](https://github.com/openshift/service-ca-operator/pull/297) -- Ginkgo migration in OpenShift
- [Kubebuilder: Writing Tests](https://book.kubebuilder.io/cronjob-tutorial/writing-tests.html) -- suite_test.go scaffolding
- [Kueue Testing Documentation](https://kueue.sigs.k8s.io/docs/contribution_guidelines/testing/) -- Label filtering patterns per controller/feature
- [OpenShift Tests Framework (DeepWiki)](https://deepwiki.com/openshift/openshift-tests/2-test-framework) -- Serial/parallel patterns, JUnit integration

---
*Research completed: 2026-03-31*
*Ready for roadmap: yes*
