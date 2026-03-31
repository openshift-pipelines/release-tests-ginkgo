# Architecture Patterns

**Domain:** Ginkgo-based Kubernetes/OpenShift operator E2E test suite (Gauge-to-Ginkgo migration)
**Researched:** 2026-03-31

## Recommended Architecture

The architecture follows a **layered test framework** pattern used by mature Kubernetes operator test suites (OpenShift HyperShift, OCS Operator, Kubernetes upstream e2e). The system has five distinct layers: test specifications, test framework (suite bootstrap + lifecycle), helper utilities, client abstraction, and configuration/fixtures.

```
                    CI / ginkgo CLI
                         |
            ginkgo run --label-filter="sanity" --junit-report=report.xml
                         |
         +-------------------------------+
         |     Suite Bootstrap Layer     |
         |  (suite_test.go per package)  |
         |  BeforeSuite / AfterSuite     |
         +-------------------------------+
                         |
         +-------------------------------+
         |    Test Specification Layer    |
         |  Describe / Context / It      |
         |  DescribeTable / Entry        |
         |  Label("sanity","smoke","e2e")|
         +-------------------------------+
                         |
         +-------------------------------+
         |    Test Framework Layer       |
         |  pkg/framework/               |
         |  - Namespace lifecycle        |
         |  - Client setup per test      |
         |  - Fixture loading            |
         |  - Common assertions          |
         +-------------------------------+
                    |           |
    +---------------+           +----------------+
    |                                            |
+-------------------+              +-------------------+
| Helper Utilities  |              | Client Abstraction|
| pkg/oc, pkg/opc   |              | pkg/clients       |
| pkg/pac, pkg/k8s  |              | K8s + Tekton +    |
|                   |              | OpenShift clients  |
+-------------------+              +-------------------+
         |                                  |
+-------------------+              +-------------------+
| Command Execution |              | Configuration     |
| pkg/cmd           |              | pkg/config        |
|                   |              | testdata/          |
+-------------------+              +-------------------+
```

### Component Boundaries

| Component | Responsibility | Communicates With | Location |
|-----------|---------------|-------------------|----------|
| **Suite Bootstrap** | Registers Ginkgo suite with `TestXxx(t)` + `RunSpecs`; houses `BeforeSuite`/`AfterSuite` for one-time cluster-level setup | Test Specs, Framework | `tests/<area>/suite_test.go` |
| **Test Specifications** | BDD specs (`Describe`/`It`/`DescribeTable`) for each test area; owns labels and test logic | Framework, Helpers | `tests/<area>/*_test.go` |
| **Test Framework** | Per-test namespace creation/deletion, shared client distribution, fixture loading, common assertions, `DeferCleanup` registration | Clients, Config, Helpers | `pkg/framework/` (NEW) |
| **Helper Utilities** | Domain-specific operations: `oc` commands, `opc` CLI, PAC/GitLab setup, K8s resource validation | Cmd, Clients, Store, Config | `pkg/oc/`, `pkg/opc/`, `pkg/pac/`, `pkg/k8s/` (NEW) |
| **Client Abstraction** | Creates and holds typed Kubernetes, Tekton, OpenShift, OLM clientsets | Config (kubeconfig) | `pkg/clients/` |
| **Command Execution** | Runs CLI commands with timeouts, asserts exit codes | Config (timeouts) | `pkg/cmd/` |
| **Configuration** | Constants, environment flags, timeout values | None (leaf) | `pkg/config/` |
| **State Management** | Thread-safe scenario/suite data storage (Gauge legacy) | Clients | `pkg/store/` (REFACTOR) |
| **Test Fixtures** | YAML manifests for Kubernetes resources used in tests | None (static files) | `testdata/` (NEW) |

### Data Flow

**Suite Initialization (once per `ginkgo run` invocation):**

```
1. ginkgo CLI parses --label-filter, --junit-report flags
2. Suite bootstrap (suite_test.go) calls RunSpecs()
3. BeforeSuite executes:
   a. Load kubeconfig from environment / flags
   b. Create shared Clients struct (K8s, Tekton, OpenShift, OLM)
   c. Verify cluster connectivity and operator readiness
   d. Store shared clients in suite-level variable
   e. Register AfterSuite / DeferCleanup for global cleanup
4. Ginkgo runs specs matching label filter
```

**Per-Test Execution:**

```
1. BeforeEach (from framework):
   a. Generate unique namespace name (e.g., "release-test-<random>")
   b. Create OpenShift project/namespace via oc
   c. Register DeferCleanup to delete namespace after spec
   d. Create namespace-scoped client set (PipelineClient, TaskRunClient, etc.)
   e. Store namespace + clients in test-local variables (NOT global store)
2. It block:
   a. Load YAML fixtures from testdata/ via config.File()
   b. Create resources via oc.Create() or client API
   c. Validate state with Gomega Eventually() for async operations
   d. Assert results with Gomega matchers
3. DeferCleanup (automatic, LIFO order):
   a. Delete test resources (if not in namespace)
   b. Delete namespace (cascading delete of namespaced resources)
```

**Reporting Flow:**

```
1. Each spec result recorded by Ginkgo reporter
2. On suite completion: --junit-report generates XML
3. XML compatible with Polarion uploader (test case IDs in spec names)
4. Optional: ReportAfterSuite for custom JUnit formatting via JunitReportConfig
```

## Recommended Directory Layout

```
release-tests-ginkgo/
|-- pkg/
|   |-- clients/          # [EXISTS] Kubernetes/Tekton client abstraction
|   |   +-- clients.go
|   |-- cmd/               # [EXISTS] CLI command execution with timeouts
|   |   +-- cmd.go
|   |-- config/            # [EXISTS] Constants, env flags, path helpers
|   |   +-- config.go
|   |-- framework/         # [NEW] Test lifecycle framework
|   |   |-- framework.go   #   - TestContext struct, namespace management
|   |   |-- cleanup.go     #   - DeferCleanup helpers
|   |   +-- matchers.go    #   - Custom Gomega matchers for Tekton resources
|   |-- k8s/               # [NEW] Kubernetes resource helpers (wait, validate)
|   |   +-- k8s.go         #   - WaitForPodReady, ValidateDeployments, etc.
|   |-- oc/                # [EXISTS] OpenShift CLI wrappers
|   |   +-- oc.go
|   |-- opc/               # [EXISTS] OpenShift Pipelines CLI wrappers
|   |   +-- opc.go
|   |-- pac/               # [EXISTS] Pipelines as Code helpers
|   |   +-- pac.go
|   +-- store/             # [EXISTS, REFACTOR] Thread-safe state (reduce usage)
|       +-- store.go
|-- tests/
|   |-- ecosystem/         # ~36 tests: buildah, s2i, git-clone
|   |   |-- suite_test.go
|   |   +-- ecosystem_test.go
|   |-- triggers/          # ~23 tests: EventListeners, TriggerBindings
|   |   |-- suite_test.go
|   |   +-- triggers_test.go
|   |-- operator/          # ~33 tests: auto-install, auto-prune, RBAC, addon
|   |   |-- suite_test.go
|   |   +-- operator_test.go
|   |-- pipelines/         # ~16 tests: PipelineRuns, TaskRuns, workspaces
|   |   |-- suite_test.go
|   |   +-- pipelines_test.go
|   |-- pac/               # ~7 tests: GitLab PAC integration
|   |   |-- suite_test.go
|   |   +-- pac_test.go
|   |-- chains/            # ~2 tests
|   |   |-- suite_test.go
|   |   +-- chains_test.go
|   |-- results/           # ~2 tests
|   |   |-- suite_test.go
|   |   +-- results_test.go
|   |-- mag/               # ~2 tests: Manual Approval Gate
|   |   |-- suite_test.go
|   |   +-- mag_test.go
|   |-- metrics/           # ~1 test
|   |   |-- suite_test.go
|   |   +-- metrics_test.go
|   |-- versions/          # ~2 tests: sanity version checks
|   |   |-- suite_test.go
|   |   +-- versions_test.go
|   +-- olm/               # ~3 tests: install, upgrade, uninstall
|       |-- suite_test.go
|       +-- olm_test.go
|-- testdata/              # [NEW] YAML fixtures organized by area
|   |-- ecosystem/
|   |   |-- buildah-pipeline.yaml
|   |   +-- s2i-java-pipeline.yaml
|   |-- triggers/
|   |   |-- eventlistener.yaml
|   |   +-- triggerbinding.yaml
|   |-- pipelines/
|   |   |-- workspace-pipeline.yaml
|   |   +-- pipeline-run.yaml
|   +-- operator/
|       +-- tektonconfig-patch.json
+-- go.mod
```

**Key structural decisions:**

1. **One suite_test.go per test area** -- Each area gets its own Ginkgo suite entry point. This enables running areas independently (`ginkgo run ./tests/operator/...`) and parallelizing across areas while keeping tests within an area sequential when needed.

2. **Flat test files within area directories** -- Avoid deep nesting. A single `*_test.go` file per area works for the current test count (2-36 tests per area). Split into multiple files only when a single file exceeds ~500 lines.

3. **testdata/ mirrors tests/ structure** -- Fixtures live alongside the areas they serve but in a separate top-level directory, consistent with Go convention and the existing `config.File()` path resolution pattern.

## Patterns to Follow

### Pattern 1: Suite Bootstrap with Shared Client (HIGH confidence)

**What:** Each suite_test.go bootstraps Ginkgo and initializes shared Kubernetes clients in BeforeSuite.
**When:** Every test area suite.
**Why:** Clients are expensive to create. One initialization per suite, shared across all specs.

```go
package operator_test

import (
    "testing"
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

var sharedClients *clients.Clients

func TestOperator(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Operator Suite", Label("operator"))
}

var _ = BeforeSuite(func() {
    var err error
    sharedClients, err = clients.NewClients(
        config.Flags.Kubeconfig,
        config.Flags.Cluster,
        config.TargetNamespace,
    )
    Expect(err).NotTo(HaveOccurred(), "Failed to create Kubernetes clients")
})
```

**Source:** Kubebuilder test scaffolding pattern, Ginkgo official docs, OCS Operator pattern.

### Pattern 2: Per-Test Namespace with DeferCleanup (HIGH confidence)

**What:** Create a fresh namespace before each test, automatically clean up after.
**When:** Tests that create Kubernetes resources (most tests except operator-level config tests).
**Why:** Test isolation. Failed tests leave no artifacts. Namespace deletion cascades to all namespaced resources.

```go
var _ = Describe("Pipeline Runs", Label("pipelines", "e2e"), func() {
    var (
        namespace string
        cs        *clients.Clients
    )

    BeforeEach(func() {
        namespace = fmt.Sprintf("release-test-%s", uuid.New().String()[:8])
        oc.CreateNewProject(namespace)
        DeferCleanup(func() {
            oc.DeleteProjectIgnoreErors(namespace)
        })
        // Create namespace-scoped clients
        cs = sharedClients // reuse shared clients
        cs.NewClientSet(namespace) // rebind namespace-scoped interfaces
    })

    It("should run a basic pipeline successfully", func() {
        // Test using namespace and cs
    })
})
```

**Source:** Kubernetes e2e best practices, Ginkgo DeferCleanup docs.

### Pattern 3: Ordered Container for Sequential Workflows (HIGH confidence)

**What:** Use `Ordered` decorator for Gauge scenarios where steps depend on prior state (e.g., PAC: setup GitLab -> create MR -> validate PipelineRun -> cleanup).
**When:** Multi-step integration tests where later steps depend on earlier step results.
**Why:** Gauge scenarios were inherently sequential. Some test workflows cannot be split into independent specs.

```go
Describe("PAC GitLab Integration", Ordered, Label("pac", "e2e"), func() {
    var (
        gitlabClient *gitlab.Client
        projectID    int
        mrIID        int
    )

    BeforeAll(func() {
        // One-time setup for this ordered group
        gitlabClient = pac.InitGitLabClient()
        DeferCleanup(func() {
            pac.CleanupPAC(sharedClients, namespace)
        })
    })

    It("creates GitLab project configuration: PIPELINES-30-TC01", func() {
        projectID = pac.SetupGitLabProject(gitlabClient)
    })

    It("creates merge request with PipelineRun YAML", func() {
        mrIID = pac.CreateMergeRequest(gitlabClient, projectID)
    })

    It("validates PipelineRun completes successfully", func() {
        pipelineName := pac.GetPipelineNameFromMR(gitlabClient, mrIID)
        Eventually(func() string {
            return pipelines.GetPipelineRunStatus(sharedClients, pipelineName, namespace)
        }, 5*time.Minute, 10*time.Second).Should(Equal("success"))
    })
})
```

**Source:** Ginkgo v2 Ordered containers docs, Gauge scenario mapping from PROJECT.md.

### Pattern 4: DescribeTable for Parameterized Tests (HIGH confidence)

**What:** Use `DescribeTable`/`Entry` for tests that repeat the same flow with different parameters (e.g., ecosystem tasks: buildah, s2i-java, s2i-python with different YAML fixtures).
**When:** Gauge data tables and repeated scenarios with different inputs.
**Why:** Gauge data tables map directly to DescribeTable. Each Entry generates a separate test with its own JUnit entry.

```go
DescribeTable("Ecosystem Task Pipelines",
    func(taskName, yamlFixture string, expectedStatus string) {
        oc.Create(yamlFixture, namespace)
        Eventually(func() string {
            return pipelines.GetPipelineRunStatus(sharedClients, taskName, namespace)
        }, 10*time.Minute, 15*time.Second).Should(Equal(expectedStatus))
    },
    Label("ecosystem", "e2e"),
    Entry("buildah pipeline", "buildah", "ecosystem/buildah-pipeline.yaml", "success"),
    Entry("s2i-java pipeline", "s2i-java", "ecosystem/s2i-java-pipeline.yaml", "success"),
    Entry("s2i-python pipeline", "s2i-python", "ecosystem/s2i-python-pipeline.yaml", "success"),
    Entry("git-clone task", "git-clone", "ecosystem/git-clone-pipeline.yaml", "success"),
)
```

**Source:** Ginkgo official docs, Gauge data table -> DescribeTable mapping from PROJECT.md.

### Pattern 5: Label-Based Test Filtering (HIGH confidence)

**What:** Apply Ginkgo `Label` decorators to replace Gauge tags (sanity, smoke, e2e, disconnected).
**When:** All tests. Labels applied at Describe and It levels.
**Why:** Direct replacement for Gauge tags. CLI filtering via `--label-filter` replaces Gauge tag filtering. Labels are inherited from parent containers.

```go
// Suite-level label (applies to all specs in suite)
RunSpecs(t, "Operator Suite", Label("operator"))

// Container-level label
Describe("Auto-prune configuration", Label("e2e", "admin"), func() {
    It("enables auto-prune with valid schedule", Label("sanity"), func() { ... })
    It("handles disconnected cluster", Label("disconnected"), func() { ... })
})

// CI usage:
// ginkgo run --label-filter="sanity" ./tests/...          # sanity only
// ginkgo run --label-filter="e2e && !disconnected" ./tests/...  # e2e minus disconnected
// ginkgo run --label-filter="operator && sanity" ./tests/...    # operator sanity
```

**Label taxonomy mapping from Gauge:**

| Gauge Tag | Ginkgo Label | Purpose |
|-----------|--------------|---------|
| `@sanity` | `Label("sanity")` | Quick smoke/sanity checks |
| `@smoke` | `Label("smoke")` | Broader smoke test set |
| `@e2e` | `Label("e2e")` | Full end-to-end tests |
| `@disconnected` | `Label("disconnected")` | Disconnected cluster tests |
| `@admin` | `Label("admin")` | Tests requiring cluster-admin |
| `@tls` | `Label("tls")` | TLS-related tests |

**Source:** Ginkgo v2 Label docs, Kubernetes e2e label migration, PROJECT.md Gauge tag mapping.

### Pattern 6: JUnit Reporting for Polarion (HIGH confidence)

**What:** Use `--junit-report` CLI flag with optional `ReportAfterSuite` for custom formatting.
**When:** CI pipeline execution.
**Why:** Polarion uploader requires JUnit XML. Spec descriptions must include test case IDs (e.g., "PIPELINES-30-TC01") for Polarion mapping.

```go
// Option A: CLI-only (simplest)
// ginkgo run --junit-report=junit-report.xml --output-dir=./artifacts ./tests/...

// Option B: Custom report formatting in suite_test.go
var _ = ReportAfterSuite("Generate Polarion-compatible JUnit report", func(report Report) {
    reporters.GenerateJUnitReportWithConfig(
        report,
        "junit-report.xml",
        reporters.JunitReportConfig{
            OmitTimelinesForSpecState: types.SpecStatePassed,
            OmitCapturedStdOutErr:     true,
        },
    )
})
```

**Source:** Ginkgo v2 reporter docs, PROJECT.md Polarion requirement.

### Pattern 7: Framework TestContext for State Management (MEDIUM confidence)

**What:** Replace the global `store` package with a framework-level `TestContext` struct passed through Ginkgo closures.
**When:** Gradual refactoring alongside migration.
**Why:** The current `store` package uses global mutable state with mutex locks -- a pattern from Gauge's step-function architecture. Ginkgo specs use closures with local variables, making shared mutable state unnecessary and error-prone, especially under parallelism.

```go
// pkg/framework/framework.go
type TestContext struct {
    Clients   *clients.Clients
    Namespace string
    // Add fields as needed per test area
}

// Usage in tests -- closures capture local state naturally
var _ = Describe("Pipeline Runs", func() {
    var tc framework.TestContext

    BeforeEach(func() {
        tc = framework.NewTestContext(sharedClients)
        DeferCleanup(tc.Cleanup)
    })

    It("runs pipeline", func() {
        // tc.Namespace, tc.Clients available directly
    })
})
```

**Note:** The `store` package should remain available during migration for backward compatibility with existing helper functions that reference it. Migrate away incrementally.

**Source:** Kubernetes e2e framework pattern, analysis of current store.go usage.

## Anti-Patterns to Avoid

### Anti-Pattern 1: Global Mutable State for Per-Test Data
**What:** Using `store.PutScenarioData()` / `store.GetScenarioData()` to pass data between test steps.
**Why bad:** Breaks test isolation. Impossible to parallelize. Race conditions under concurrent access. Leftover state from failed tests pollutes subsequent tests.
**Instead:** Use closure-scoped variables within `Describe`/`Context` blocks. Each spec captures its own state via Go closures. The `Ordered` decorator with shared variables handles sequential workflows.

### Anti-Pattern 2: One Giant Suite for All Tests
**What:** Putting all ~110 tests into a single `tests/suite_test.go`.
**Why bad:** Cannot run test areas independently. Cannot parallelize across areas. Slow CI feedback -- must run everything even for area-specific changes.
**Instead:** One `suite_test.go` per test area directory. Run specific areas with `ginkgo run ./tests/operator/...`.

### Anti-Pattern 3: Importing from Original Gauge Repo
**What:** `pkg/oc/oc.go` currently imports `github.com/openshift-pipelines/release-tests/pkg/cmd` -- the original Gauge repo.
**Why bad:** Creates a dependency on the repo being replaced. Two different `config` and `store` packages in the same binary. Confusing import paths.
**Instead:** All imports must reference the local module (`github.com/openshift-pipelines/release-tests-ginkgo` or updated module path). Port any needed functions to local packages.

### Anti-Pattern 4: Nested BeforeSuite
**What:** Trying to put `BeforeSuite` inside a `Describe` block.
**Why bad:** Ginkgo enforces that `BeforeSuite` is top-level only. Will cause a compilation/runtime error.
**Instead:** Use `BeforeEach` or `BeforeAll` (inside `Ordered` containers) for non-suite-level setup.

### Anti-Pattern 5: Using ctx from It in DeferCleanup
**What:** Capturing the `SpecContext` from an `It` closure and using it inside a `DeferCleanup` callback.
**Why bad:** The context is cancelled when the spec completes, before cleanup runs. Cleanup operations will fail with "context cancelled".
**Instead:** Register `DeferCleanup` functions that accept their own `context.Context` parameter -- Ginkgo provides a fresh one.

### Anti-Pattern 6: Manual Sleep for Async Waits
**What:** Using `time.Sleep(30 * time.Second)` to wait for resources to become ready.
**Why bad:** Either waits too long (slow tests) or not long enough (flaky tests). No feedback on what is being waited for.
**Instead:** Use `Eventually(func() ... , timeout, interval).Should(matcher)` for polling-based assertions. This provides clear failure messages and respects SpecTimeout.

## Build Order (Dependencies Between Components)

The migration must build components in this order because each layer depends on the ones below it:

```
Phase 1: Foundation (no test-area dependencies)
  |-- Fix pkg/oc imports (remove release-tests dependency)
  |-- Create pkg/framework/ (TestContext, namespace management)
  |-- Create testdata/ directory structure
  |-- Update module path if needed
  |
Phase 2: Suite Infrastructure (depends on Phase 1)
  |-- Create suite_test.go for each test area
  |-- Implement BeforeSuite/AfterSuite patterns
  |-- Implement Label taxonomy
  |-- Configure JUnit reporting
  |
Phase 3: Migrate Test Areas (depends on Phase 2, areas are independent)
  |-- Sanity/versions tests (smallest, validates framework)
  |-- Ecosystem tasks (largest count, uses DescribeTable heavily)
  |-- Triggers tests (moderate, uses namespace + event listeners)
  |-- Operator tests (complex, uses Ordered containers + TektonConfig)
  |-- Pipelines core tests (moderate, standard CRUD)
  |-- PAC tests (complex Ordered workflows, GitLab integration)
  |-- Chains, Results, MAG, Metrics, OLM tests (small, independent)
  |
Phase 4: Validation & CI
  |-- Parity testing against Gauge results
  |-- CI pipeline integration
  |-- Polarion JUnit report validation
```

**Why this order:**
- Phase 1 must come first because the existing `pkg/oc` package has broken imports from the original Gauge repo. No tests can work until local imports are correct.
- Phase 2 builds the scaffolding that every test area needs. Without suite_test.go files and the framework, no specs can execute.
- Phase 3 areas are independent of each other (ecosystem tests do not depend on operator tests). They can be migrated in parallel by different contributors if desired. Starting with sanity tests validates the framework with minimal complexity.
- Phase 4 is a verification phase that requires all test areas to be migrated.

## Scalability Considerations

| Concern | At current scale (~110 tests) | At 2x scale (~220 tests) | Notes |
|---------|-------------------------------|--------------------------|-------|
| **Suite run time** | Run areas sequentially, ~30-60 min total | Parallelize across areas with `ginkgo -p` | Each area is its own Go test binary |
| **Namespace exhaustion** | ~110 namespaces created/deleted per run | DeferCleanup ensures cleanup; add namespace prefix for easy bulk cleanup | Use `release-test-` prefix for all test namespaces |
| **Client connections** | 1 shared client per suite | 1 shared client per suite (parallelism uses separate processes) | QPS/Burst already set high (100/200) |
| **Fixture management** | ~50 YAML files in testdata/ | Organize by area subdirectory | Current `config.File()` pattern scales well |
| **CI feedback time** | Run sanity subset (~9 tests) for fast feedback | Use `--label-filter="sanity"` for PR checks, full e2e on merge | Label system enables flexible CI configuration |

## Sources

- [Ginkgo v2 Official Documentation](https://onsi.github.io/ginkgo/) -- HIGH confidence, primary reference for all Ginkgo patterns
- [Kubebuilder: Writing Tests](https://book.kubebuilder.io/cronjob-tutorial/writing-tests.html) -- HIGH confidence, suite_test.go scaffolding
- [Kubernetes E2E Testing Best Practices, Reloaded](https://www.kubernetes.dev/blog/2023/04/12/e2e-testing-best-practices-reloaded/) -- HIGH confidence, namespace management, DeferCleanup
- [OpenShift HyperShift Ginkgo v2 E2E Suite (PR #7192)](https://github.com/openshift/hypershift/pull/7192) -- MEDIUM confidence, OpenShift-specific Ginkgo patterns
- [OpenShift Service CA Operator E2E Migration (PR #297)](https://github.com/openshift/service-ca-operator/pull/297) -- MEDIUM confidence, Ginkgo migration in OpenShift
- [OCS Operator](https://github.com/red-hat-storage/ocs-operator) -- MEDIUM confidence, 3-phase test pattern
- [Ginkgo Labels for Kubernetes E2E](https://groups.google.com/a/kubernetes.io/g/dev/c/DTFEng143NY) -- HIGH confidence, label-based filtering
- [Testing Kubernetes Operators with Ginkgo (ITNEXT)](https://itnext.io/testing-kubernetes-operators-with-ginkgo-gomega-and-the-operator-runtime-6ad4c2492379) -- MEDIUM confidence, DescribeTable patterns
- [Kueue Testing Guidelines](https://kueue.sigs.k8s.io/docs/contribution_guidelines/testing/) -- MEDIUM confidence, label organization
- [Ginkgo v2 reporters package](https://pkg.go.dev/github.com/onsi/ginkgo/v2/reporters) -- HIGH confidence, JUnit configuration
- Analysis of existing codebase: `tests/pac/pac_test.go`, `pkg/store/store.go`, `pkg/clients/clients.go`, `pkg/config/config.go`, `pkg/cmd/cmd.go`, `pkg/oc/oc.go` -- HIGH confidence, direct code inspection

---

*Architecture research: 2026-03-31*
