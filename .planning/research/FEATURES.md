# Feature Landscape

**Domain:** Ginkgo v2 test suite for Kubernetes/OpenShift operator E2E testing (Gauge-to-Ginkgo migration)
**Researched:** 2026-03-31
**Overall confidence:** HIGH (features verified against official Ginkgo docs + Kubernetes ecosystem patterns)

## Table Stakes

Features the test suite must have. Missing any of these means the migration is incomplete, tests are unmaintainable, or CI cannot consume results.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Suite entry points per area | Each test area (ecosystem, triggers, operator, etc.) needs its own `suite_test.go` with `TestXxx` function and `RunSpecs` | Low | Gauge specs are per-file; Ginkgo suites group related files. One suite per `tests/{area}/` directory |
| Describe/Context/It hierarchy | Maps directly from Gauge spec headers/scenarios/steps. Without this, 110 tests are a flat unreadable list | Low | Top-level `Describe` per feature area, nested `Describe` or `Context` per test case, `It` per assertion step |
| Ordered containers | Many tests are multi-step workflows (create resource, configure, validate, cleanup). Steps must run in sequence, not randomized | Low | Use `Ordered` decorator on `Describe` blocks that contain dependent steps. Replaces Gauge's implicit step ordering |
| Label system (sanity/smoke/e2e/disconnected) | Must replicate Gauge tags for CI job filtering. Without labels, cannot run subset of tests per pipeline stage | Medium | Map Gauge tags to `Label("sanity")`, `Label("smoke")`, `Label("e2e")`, `Label("disconnected")`. Apply at `Describe` level so all child specs inherit |
| Label-based filtering in CI | CI pipelines must select test subsets via `--label-filter="sanity"` or `--label-filter="e2e && !disconnected"` | Low | Built into `ginkgo run --label-filter`. Boolean operators supported: `&&`, `||`, `!`, `()` |
| JUnit XML reporting | Polarion uploader requires JUnit XML. Without compatible output, test results cannot be tracked in the existing test management system | Medium | Use `ReportAfterSuite` with `GenerateJUnitReportWithConfig` for control over format. `--junit-report` flag also works for basic cases |
| JUnit Polarion compatibility | JUnit XML must include correct `<properties>` block, proper `classname` format, and test case names matching Polarion test case IDs | High | Known issue: raw Ginkgo JUnit output may need post-processing or `JunitReportConfig` tuning (`OmitLeafNodeType`, `OmitSpecLabels`). Test case IDs like `PIPELINES-30-TC01` must appear in test names |
| Test case ID preservation | Each test must carry its Polarion test case ID (e.g., `PIPELINES-30-TC01`) for traceability | Low | Embed in `Describe` description string: `Describe("Configure PAC: PIPELINES-30-TC01", ...)`. Already partially done in existing code |
| BeforeSuite/AfterSuite | Suite-level cluster client initialization, namespace setup, and teardown. Running this per-spec would be prohibitively expensive | Low | Single `BeforeSuite` to initialize K8s clients and store in `pkg/store`. `AfterSuite` for global cleanup |
| DeferCleanup for resource cleanup | Tests create K8s resources (namespaces, pipelines, triggers) that must be deleted even on failure | Low | `DeferCleanup` is superior to `AfterEach` because it co-locates cleanup with creation and runs in LIFO order. Context-aware (gets fresh context even if spec context was canceled) |
| Eventually/Consistently async assertions | K8s operations are asynchronous. PipelineRun completion, pod creation, deployment readiness all require polling | Medium | Replace `time.Sleep` + manual polling with `Eventually(func() { ... }).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(...)`. `Consistently` for asserting something does NOT happen |
| Gomega matchers | Standard assertion library paired with Ginkgo. Without it, assertions are verbose and error-prone | Low | Already in go.mod. Use `Expect()`, `HaveField()`, `ContainSubstring()`, `BeEquivalentTo()` etc. |
| Data-driven tests via DescribeTable | Gauge data tables drive ~36 ecosystem task tests with varying parameters (task name, workspace type, etc.). Must have equivalent | Medium | `DescribeTable("ecosystem tasks", func(taskName, workspaceType string) { ... }, Entry("buildah", "buildah", "pvc"), Entry("git-clone", "git-clone", "emptyDir"))` |
| Serial decorator | Some tests modify cluster-wide state (operator config, TektonConfig CR, RBAC). Cannot run in parallel | Low | Mark cluster-modifying tests with `Serial`. Apply at container level, not individual specs |
| Shared test helpers in pkg/ | Common operations (create resource, wait for pipeline, check deployment) must be reusable across all test areas | Low | Already exists: `pkg/oc`, `pkg/opc`, `pkg/cmd`, `pkg/clients`, `pkg/store`, `pkg/pac`. This is the primary Gauge-to-Ginkgo bridge -- step implementations become direct Go function calls |
| Test data store (pkg/store) | Multi-step ordered tests need to share state between steps (e.g., pipeline name created in step 1 validated in step 3) | Low | Already implemented with thread-safe scenario/suite stores. Maps to Gauge's ScenarioDataStore concept |
| Environment-based configuration | Tests need cluster details, operator versions, channels from environment variables and flags | Low | Already implemented in `pkg/config`. Flags parsed via `flag` package, env vars as defaults |

## Differentiators

Features that make the Ginkgo suite better than what Gauge provided. Not strictly required for parity, but provide significant value.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Parallel test execution | Gauge runs tests sequentially. Ginkgo can run independent test areas in parallel, cutting CI time significantly (6x improvement reported in similar projects) | Medium | Use `ginkgo run -p` or `--procs=N`. Requires namespace isolation per parallel process. `SynchronizedBeforeSuite` shares cluster config across processes |
| SynchronizedBeforeSuite | Initializes cluster client once on node 1, broadcasts config to all parallel nodes. Enables parallelism without N redundant client initializations | Medium | First function runs on node 1 only (creates clients, marshals config). Second function runs on all nodes (deserializes config). Critical for parallel execution |
| Namespace-per-test isolation | Each test (or parallel node) gets its own namespace, preventing resource name collisions and cross-test interference | Medium | Generate unique namespace per test via `BeforeEach` using `fmt.Sprintf("test-%s-%d", suiteName, GinkgoParallelProcess())`. Cleanup via `DeferCleanup` |
| Progress reporting in CI | Long-running tests (10+ minute operator installs) appear hung in CI without progress. Ginkgo v2 emits periodic progress reports | Low | Built-in via `--progress` flag or `SIGINFO`/`SIGUSR1`. Shows which spec is running and how long it has been |
| Custom report entries for diagnostics | Attach cluster state, pod logs, or event dumps to failed test reports. Gauge had no equivalent | Medium | `AddReportEntry("cluster-state", collectClusterDiagnostics())` in `ReportAfterEach`. Only shown on failure if using `ReportEntryVisibilityFailureOrVerbose` |
| DescribeTableSubtree for complex data-driven tests | Some ecosystem tests need multiple assertions per parameter set (not just one `It`). `DescribeTableSubtree` generates entire spec subtrees per entry | Low | Newer Ginkgo v2 feature. Use when a single `Entry` needs setup, multiple assertions, and cleanup -- not just a single test function |
| FlakeAttempts for known flaky tests | Some cluster tests are inherently flaky (timing, network, cloud provider issues). Targeted retry prevents CI noise | Low | Use sparingly with `FlakeAttempts(2)` decorator on specific flaky specs. Do NOT use global `--flake-attempts` flag (masks real bugs, as Kubernetes and Podman projects learned) |
| Spec timeout decorators | Individual specs can have timeouts independent of suite timeout. Prevents one hung test from blocking the entire suite | Low | `It("long operation", SpecTimeout(15*time.Minute), func(ctx SpecContext) { ... })`. Use `ctx` for cancellation-aware operations |
| Focused/Pending specs for development | During migration, mark unmigrated tests as `Pending` and focus on active ones with `Focus`. Better than commenting out code | Low | `PDescribe(...)` or `XIt(...)` for pending. `FDescribe(...)` or `FIt(...)` for focus. CI should fail if `Focus` is committed (use `--fail-on-focus-pending` in linter) |
| ReportAfterSuite for aggregated reporting | Generate custom reports (HTML, Slack notifications, dashboard updates) after all specs complete. Single aggregated report even in parallel mode | Medium | `ReportAfterSuite("report", func(report types.Report) { ... })`. Ginkgo aggregates reports from all parallel nodes before calling this |
| MustPassRepeatedly for stability validation | Before declaring a migrated test "done," run it N times to confirm it is not accidentally flaky | Low | `It("stable test", MustPassRepeatedly(3), func() { ... })`. Use during migration validation phase, remove for production runs |
| oc/kubectl diagnostic collection on failure | Automatically collect pod logs, events, and resource state when a test fails. Critical for debugging CI failures on remote clusters | High | Implement via `ReportAfterEach` that checks `CurrentSpecReport().Failed()` and runs `oc get events`, `oc logs`, `oc describe` for relevant resources. Store output via `AddReportEntry` |
| Ginkgo CLI wrapper script | Standardize test invocation across developers and CI with a single entry point that sets correct flags, timeouts, labels | Low | Create `scripts/run-tests.sh` wrapping `ginkgo run` with default `--timeout`, `--junit-report`, `--label-filter`, `--procs` |
| Skip decorator for conditional tests | Skip tests based on cluster state (e.g., skip disconnected tests on connected cluster, skip arch-specific tests on wrong arch) | Low | `BeforeEach(func() { if !config.Flags.IsDisconnected { Skip("requires disconnected cluster") } })` |

## Anti-Features

Features to deliberately NOT build. These are tempting but counterproductive.

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Global shared mutable state beyond pkg/store | The existing `pkg/store` with its mutex-protected maps is adequate. Adding more global state mechanisms creates hidden coupling between tests and breaks parallelism | Keep `pkg/store` as the single state mechanism. For parallel tests, use `SynchronizedBeforeSuite` for initialization and closures/local variables within `Ordered` containers for step-to-step state |
| Custom Ginkgo reporter interface | Ginkgo v2 removed the custom reporter interface. Reimplementing it fights the framework | Use `ReportAfterEach`, `ReportAfterSuite`, and `AddReportEntry` -- the v2 replacement pattern |
| Global FlakeAttempts via CLI flag | Global `--flake-attempts=N` masks real bugs. Both Kubernetes (issue #68091) and Podman (issue #17967) learned this painfully | Apply `FlakeAttempts(N)` as a decorator on specific known-flaky specs only. Fix flakiness at the root cause level |
| envtest / fake control plane | These tests run against real OpenShift clusters with Tekton Operator. envtest lacks admission controllers, CRD controllers, and operator behavior | Continue using real cluster testing via existing `pkg/clients`. envtest is for unit-testing controllers, not operator E2E validation |
| Separate spec files (BDD-style separation of spec and implementation) | Gauge separates .spec files from step implementations. Ginkgo deliberately keeps spec and implementation in the same Go file | Write test logic directly in `It`/`Describe` blocks, calling `pkg/` helper functions. No separate spec layer |
| Wrapping every helper in Eventually | Not every operation is async. Synchronous CLI commands (`oc create -f file.yaml`) should assert immediately. Over-using `Eventually` hides real failures and adds unnecessary polling overhead | Use `Eventually` only for genuinely async operations: waiting for PipelineRun completion, pod readiness, deployment rollout. Use direct `Expect` for synchronous CLI assertions |
| Per-spec BeforeSuite | Cannot have more than one `BeforeSuite` per suite. Do not try to initialize different resources per spec in suite-level setup | Use `BeforeEach` or `BeforeAll` (in `Ordered` containers) for spec-specific setup. `BeforeSuite` is for suite-wide shared resources only |
| Deeply nested Context chains (> 3 levels) | More than 3 levels of nesting makes tests hard to read and the test output tree impossible to parse | Keep hierarchy to: `Describe` (area) > `Describe` or `Context` (test case) > `It` (assertion). Use helper functions for shared setup rather than another nesting level |
| OpenShift's Ginkgo fork | OpenShift maintains a fork of Ginkgo (`github.com/openshift/onsi-ginkgo`) with custom code location handling. Adds complexity and version drift | Use upstream `github.com/onsi/ginkgo/v2` directly. The fork is for openshift-tests specifically, not for operator test suites |

## Feature Dependencies

```
Suite entry points (suite_test.go per area)
  --> Describe/Context/It hierarchy (test organization within suites)
    --> Label system (labels applied to Describe/It nodes)
      --> Label-based filtering in CI (requires labels to exist)
    --> Ordered containers (for multi-step workflow tests)
      --> Test data store / pkg/store (state sharing between ordered steps)
    --> DescribeTable (for data-driven tests within suites)

BeforeSuite / AfterSuite (suite-level setup)
  --> DeferCleanup (cleanup registered in BeforeSuite runs after all specs)
  --> SynchronizedBeforeSuite (enables parallel execution)
    --> Namespace-per-test isolation (required for parallel safety)
    --> Parallel test execution (depends on isolation + synchronized setup)

Eventually / Consistently (async assertions)
  --> Spec timeout decorators (timeout protection for async waits)

JUnit XML reporting
  --> JUnit Polarion compatibility (format tuning for import)
  --> ReportAfterSuite (custom report generation)
    --> Custom report entries for diagnostics (depends on reporting infrastructure)
    --> oc/kubectl diagnostic collection on failure (uses ReportAfterEach)
```

## MVP Recommendation

### Phase 1: Foundation (must complete first)

Prioritize these in order -- everything else depends on them:

1. **Suite entry points per area** -- Create `suite_test.go` for each test area directory. Without this, nothing runs.
2. **Describe/Context/It hierarchy with Ordered containers** -- Port the first batch of tests (sanity tests) to validate the pattern.
3. **BeforeSuite/AfterSuite** with cluster client initialization -- Tests need clients to talk to the cluster.
4. **DeferCleanup** for resource teardown -- Every test that creates resources must clean up.
5. **Label system** with the four Gauge tags (sanity, smoke, e2e, disconnected) -- Apply from day one so CI filtering works immediately.
6. **JUnit XML reporting** with Polarion compatibility -- Validate output format against Polarion uploader before migrating more tests.

### Phase 2: Bulk migration enablers

7. **DescribeTable** for data-driven ecosystem tests (36 tests become much simpler with table patterns).
8. **Eventually/Consistently** for async K8s assertions -- Replace sleep-based waits.
9. **Serial decorator** on cluster-modifying operator tests.
10. **Test case ID preservation** in test names for Polarion traceability.

### Phase 3: CI optimization (after all tests migrated)

11. **Parallel test execution** with `SynchronizedBeforeSuite` and namespace isolation.
12. **Progress reporting** and **diagnostic collection on failure**.
13. **FlakeAttempts** on identified flaky tests (targeted, not global).
14. **Ginkgo CLI wrapper script** for standardized invocation.

**Defer:** Parallel execution until all tests are migrated and stable. Parallelism adds complexity (namespace isolation, state management) that should not block the migration itself.

**Defer:** `DescribeTableSubtree` until a concrete need emerges during ecosystem test migration. Standard `DescribeTable` covers most cases.

## Sources

### Official Documentation (HIGH confidence)
- [Ginkgo v2 official documentation](https://onsi.github.io/ginkgo/) -- Labels, Ordered, Serial, DeferCleanup, reporting, decorators
- [Ginkgo v2 Go package docs](https://pkg.go.dev/github.com/onsi/ginkgo/v2) -- API reference for all decorators and functions
- [Ginkgo v2 reporters package](https://pkg.go.dev/github.com/onsi/ginkgo/v2/reporters) -- JunitReportConfig, GenerateJUnitReportWithConfig
- [Ginkgo v2 Migration Guide](https://onsi.github.io/ginkgo/MIGRATING_TO_V2) -- Custom reporters removal, new reporting nodes
- [Gomega documentation](https://pkg.go.dev/github.com/onsi/gomega) -- Eventually, Consistently, matchers

### Kubernetes Ecosystem (HIGH confidence)
- [Kubernetes E2E Testing Best Practices, Reloaded](https://www.kubernetes.dev/blog/2023/04/12/e2e-testing-best-practices-reloaded/) -- DeferCleanup patterns, Eventually best practices
- [Kubernetes Ginkgo v2 migration umbrella issue #109744](https://github.com/kubernetes/kubernetes/issues/109744) -- Migration patterns and lessons
- [Kubernetes stop using flakeAttempts issue #68091](https://github.com/kubernetes/kubernetes/issues/68091) -- Why global flake retries are harmful
- [Kubernetes labels for test selection](https://groups.google.com/a/kubernetes.io/g/dev/c/DTFEng143NY) -- Label adoption patterns

### OpenShift Ecosystem (MEDIUM confidence)
- [OpenShift Tests Framework (DeepWiki)](https://deepwiki.com/openshift/openshift-tests/2-test-framework) -- Serial/parallel patterns, suite organization, JUnit integration
- [Submariner Polarion JUnit fix issue #48](https://github.com/submariner-io/shipyard/issues/48) -- JUnit XML compatibility with Polarion
- [Kueue testing documentation](https://kueue.sigs.k8s.io/docs/contribution_guidelines/testing/) -- Label filtering patterns per controller/feature

### Real-World Parallelism (MEDIUM confidence)
- [Speeding Up Kubernetes Controller Integration Tests with Ginkgo Parallelism](https://kev.fan/posts/04-k8s-ginkgo-parallel-tests/) -- SynchronizedBeforeSuite pattern, 6.5x speedup
- [Podman flakeAttempts removal epic #17967](https://github.com/containers/podman/issues/17967) -- Real-world consequences of global flake retry
