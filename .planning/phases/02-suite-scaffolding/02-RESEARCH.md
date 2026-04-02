# Phase 2: Suite Scaffolding - Research

**Researched:** 2026-03-31
**Domain:** Ginkgo v2 suite bootstrap, lifecycle hooks, labels, and shared test patterns for OpenShift Pipelines E2E tests
**Confidence:** HIGH

## Summary

Phase 2 creates the Ginkgo test infrastructure that every subsequent migration phase depends on. The work has two halves: (1) creating 11 `suite_test.go` entry points with `BeforeSuite`/`AfterSuite` lifecycle hooks that initialize `pkg/clients` and (2) establishing reference implementations of every shared test pattern (Labels, DeferCleanup, Eventually, Ordered, DescribeTable, Skip) so that future phases have working examples to copy.

The existing codebase provides a strong foundation. `pkg/clients.NewClients()` accepts kubeconfig path, cluster name, and namespace -- it returns a fully populated `*Clients` struct with Kubernetes, Tekton, Triggers, PAC, OLM, and operator clientsets. The `pkg/config` package already parses all needed flags and environment variables. The `pkg/store` package provides thread-safe scenario/suite data storage. The `tests/pac/pac_test.go` file demonstrates the existing Ginkgo spec pattern but is missing a `suite_test.go` bootstrap file.

The critical risk in this phase is getting the `BeforeSuite` pattern right. The client initialization must happen exactly once, store the result in a package-level variable accessible to all specs, and ensure `AfterSuite` runs cleanup even on failure. The existing `pkg/clients/clients.go` has a commented-out context cancellation (line 72-74) that must be addressed. Additionally, `store.Namespace()` returns empty string when the scenario store is not populated, which affects `oc.DeleteResource()` -- the namespace must be passed explicitly in tests, not relied upon from the global store.

**Primary recommendation:** Create a common suite bootstrap pattern in a shared helper (`pkg/framework/` or inline per suite), then replicate it across all 11 test area directories. Establish one "patterns" test file with sample specs demonstrating all SUITE-04 through SUITE-09 patterns.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| SUITE-01 | Create `suite_test.go` for each of 11 test areas | Pattern 1 (Suite Bootstrap) provides the exact template. Directories: ecosystem, triggers, operator, pipelines, pac, chains, results, mag, metrics, versions, olm. PAC already has `pac_test.go` but needs `suite_test.go` added. |
| SUITE-02 | `BeforeSuite` for cluster client initialization via `pkg/clients` | Pattern 1 shows `clients.NewClients(config.Flags.Kubeconfig, config.Flags.Cluster, config.TargetNamespace)`. Must store result in package-level `var sharedClients`. Pitfall 10 warns BeforeSuite runs even when all specs filtered -- keep it lightweight. |
| SUITE-03 | `AfterSuite` for global cleanup | Pattern 2 (AfterSuite) covers cleanup. Must handle context cancellation from `clients.Ctx`. Pitfall 12 documents the goroutine leak from commented-out context cancel. |
| SUITE-04 | Label system mapping Gauge tags (sanity, smoke, e2e, disconnected) | Pattern 3 (Labels) provides taxonomy mapping. Labels apply at `RunSpecs`, `Describe`, and `It` levels. Suite-level label in `RunSpecs` applies to all specs. |
| SUITE-05 | `DeferCleanup` pattern for resource teardown | Pattern 4 (DeferCleanup) with Pitfall 11 (context cancellation) -- cleanup functions must NOT capture the `It` context; use the callback-with-context form. |
| SUITE-06 | `Eventually`/`Consistently` async assertions | Pattern 5 (Eventually) replaces `time.Sleep` polling. Use `config.APITimeout` and `config.APIRetry` for timeout/polling. `Consistently` uses `config.ConsistentlyDuration`. |
| SUITE-07 | `Ordered` containers for multi-step workflows | Pattern 6 (Ordered) with `BeforeAll`/`AfterAll`. Variables shared via closure scope within the `Ordered` `Describe` block. Pitfall 3 warns against initializing in container scope. |
| SUITE-08 | `DescribeTable`/`Entry` for data-driven tests | Pattern 7 (DescribeTable) with Pitfall 4 -- Entry parameters are evaluated at tree construction time. Only use literals or constants in `Entry()` arguments. Dynamic data goes in the table body function. |
| SUITE-09 | `Skip` decorator for conditional tests | Pattern 8 (Skip) uses `config.Flags.IsDisconnected` and `config.Flags.ClusterArch` for conditional execution. Implement in `BeforeEach` or at the top of `It` blocks. |
</phase_requirements>

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/onsi/ginkgo/v2` | v2.28.1 | BDD test framework, suite runner, labels, decorators | Already in go.mod. Provides `RunSpecs`, `BeforeSuite`, `Label`, `Ordered`, `DescribeTable`, `DeferCleanup`, `Eventually` |
| `github.com/onsi/gomega` | v1.39.1 | Assertion library with async matchers | Already in go.mod. Provides `Expect`, `Eventually`, `Consistently`, `HaveField`, `ContainSubstring` |
| `github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients` | local | Kubernetes/Tekton/OpenShift client initialization | Existing package. `NewClients()` returns `*Clients` with all needed clientsets |
| `github.com/openshift-pipelines/release-tests-ginkgo/pkg/config` | local | Constants, flags, environment variable parsing | Existing package. Provides `APITimeout`, `APIRetry`, `CLITimeout`, `TargetNamespace`, `Flags` |
| `github.com/openshift-pipelines/release-tests-ginkgo/pkg/store` | local | Thread-safe scenario/suite data storage | Existing package. Needed during transition. Use closure-scoped vars for new tests |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc` | local | OpenShift CLI wrappers | Creating/deleting projects, resources, secrets |
| `github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd` | local | CLI command execution with timeouts | Running arbitrary CLI commands in tests |
| `github.com/openshift-pipelines/release-tests-ginkgo/pkg/opc` | local | OpenShift Pipelines CLI wrappers | Version checks, pipeline starts, hub operations |
| `gotest.tools/v3` | v3.5.2 | `icmd` package for command assertions | Used by `pkg/cmd` internally |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Package-level `var sharedClients` | `SynchronizedBeforeSuite` | SynchronizedBeforeSuite is needed for parallel execution (Phase 10), but adds complexity. Use simple `BeforeSuite` now, upgrade later |
| `pkg/store` for state | Closure-scoped variables | Closures are cleaner but require refactoring all existing helper functions that call `store.Namespace()` etc. Keep store during migration, migrate away incrementally |
| Per-suite `BeforeSuite` client init | Shared `pkg/framework.Setup()` | A shared setup function reduces duplication across 11 suites. Recommended as a helper, not a replacement for per-suite bootstrap |

## Architecture Patterns

### Recommended Project Structure
```
tests/
|-- ecosystem/
|   |-- suite_test.go          # TestEcosystem(t) + RunSpecs + BeforeSuite
|   +-- ecosystem_test.go      # Specs (created in later phases)
|-- triggers/
|   |-- suite_test.go
|   +-- triggers_test.go
|-- operator/
|   |-- suite_test.go
|   +-- operator_test.go
|-- pipelines/
|   |-- suite_test.go
|   +-- pipelines_test.go
|-- pac/
|   |-- suite_test.go          # NEW -- missing today
|   +-- pac_test.go            # EXISTS -- needs minor update
|-- chains/
|   |-- suite_test.go
|   +-- chains_test.go
|-- results/
|   |-- suite_test.go
|   +-- results_test.go
|-- mag/
|   |-- suite_test.go
|   +-- mag_test.go
|-- metrics/
|   |-- suite_test.go
|   +-- metrics_test.go
|-- versions/
|   |-- suite_test.go
|   +-- versions_test.go
+-- olm/
    |-- suite_test.go
    +-- olm_test.go
```

### Pattern 1: Suite Bootstrap (suite_test.go)

**What:** Each test area gets a `suite_test.go` with `TestXxx(t *testing.T)`, `RegisterFailHandler(Fail)`, `RunSpecs`, and `BeforeSuite`/`AfterSuite`.
**When:** Every test area directory.
**Why:** Without `suite_test.go`, Ginkgo silently skips the package or runs without the Ginkgo runner (losing labels, reporters, parallel support).

```go
package operator_test

import (
    "testing"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

// sharedClients holds the Kubernetes/Tekton clients initialized once per suite.
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

var _ = AfterSuite(func() {
    // Global cleanup -- delete any lingering test namespaces, temp files, etc.
    _ = config.RemoveTempDir()
})
```

**Confidence:** HIGH -- this is the canonical Ginkgo bootstrap pattern from official docs and Kubebuilder.

**Naming convention for `TestXxx` functions across suites:**

| Suite | Function | RunSpecs Description | Suite Label |
|-------|----------|---------------------|-------------|
| ecosystem | `TestEcosystem` | "Ecosystem Suite" | `Label("ecosystem")` |
| triggers | `TestTriggers` | "Triggers Suite" | `Label("triggers")` |
| operator | `TestOperator` | "Operator Suite" | `Label("operator")` |
| pipelines | `TestPipelines` | "Pipelines Suite" | `Label("pipelines")` |
| pac | `TestPAC` | "PAC Suite" | `Label("pac")` |
| chains | `TestChains` | "Chains Suite" | `Label("chains")` |
| results | `TestResults` | "Results Suite" | `Label("results")` |
| mag | `TestMAG` | "MAG Suite" | `Label("mag")` |
| metrics | `TestMetrics` | "Metrics Suite" | `Label("metrics")` |
| versions | `TestVersions` | "Versions Suite" | `Label("versions")` |
| olm | `TestOLM` | "OLM Suite" | `Label("olm")` |

### Pattern 2: AfterSuite with Context Cleanup

**What:** `AfterSuite` cleans up suite-level resources. Address the goroutine leak from `pkg/clients/clients.go` line 72-74.
**When:** Every suite.

```go
var _ = AfterSuite(func() {
    // Clean up temp directory
    _ = config.RemoveTempDir()
    // Future: cancel the clients context when context cancellation is uncommented
})
```

**Note:** The commented-out `ctx, cancel := context.WithCancel(ctx)` in `clients.go` should be addressed by storing a `CancelFunc` on the `Clients` struct and calling it in `AfterSuite`. However, this is a code change to `pkg/clients` which should be tracked as a separate task within this phase.

**Confidence:** HIGH

### Pattern 3: Label Taxonomy

**What:** Apply Ginkgo `Label` decorators to map Gauge tags.
**When:** All tests. Labels at `RunSpecs` (suite level), `Describe` (container level), and `It` (spec level).

```go
// Suite-level label -- all specs in this package inherit it
RunSpecs(t, "Operator Suite", Label("operator"))

// Container-level labels -- Gauge tag mapping
Describe("Auto-prune configuration", Label("e2e"), func() {
    It("enables auto-prune", Label("sanity"), func() { ... })
    It("handles disconnected cluster", Label("disconnected"), func() { ... })
})

// CLI filtering examples:
// ginkgo run --label-filter="sanity" ./tests/...
// ginkgo run --label-filter="e2e && !disconnected" ./tests/...
// ginkgo run --label-filter="operator && sanity" ./tests/...
```

**Label taxonomy (from Gauge):**

| Gauge Tag | Ginkgo Label | Scope |
|-----------|-------------|-------|
| `@sanity` | `Label("sanity")` | Quick validation |
| `@smoke` | `Label("smoke")` | Broader smoke set |
| `@e2e` | `Label("e2e")` | Full end-to-end |
| `@disconnected` | `Label("disconnected")` | Disconnected cluster only |
| `@admin` | `Label("admin")` | Cluster-admin required |
| `@tls` | `Label("tls")` | TLS-specific tests |

**Confidence:** HIGH

### Pattern 4: DeferCleanup for Resource Teardown

**What:** Co-locate cleanup with resource creation using `DeferCleanup`. Runs in LIFO order, even on failure.
**When:** Any test that creates Kubernetes resources (namespaces, pipelines, secrets).

```go
BeforeEach(func() {
    namespace = fmt.Sprintf("release-test-%s", uuid.New().String()[:8])
    oc.CreateNewProject(namespace)
    DeferCleanup(func() {
        oc.DeleteProjectIgnoreErors(namespace)
    })
})
```

**Critical:** Do NOT capture the `It` block's `SpecContext` in a `DeferCleanup` closure. The context is cancelled before cleanup runs. Use the callback-with-context form if you need a context:

```go
// WRONG -- ctx is cancelled before cleanup runs
It("test", func(ctx SpecContext) {
    DeferCleanup(func() {
        deleteWithContext(ctx, namespace) // ctx already cancelled!
    })
})

// RIGHT -- Ginkgo provides a fresh context to the cleanup callback
DeferCleanup(func(cleanupCtx context.Context) {
    deleteWithContext(cleanupCtx, namespace)
})
```

**Confidence:** HIGH

### Pattern 5: Eventually/Consistently Async Assertions

**What:** Replace `time.Sleep` + manual polling with `Eventually` for async Kubernetes operations.
**When:** Waiting for PipelineRun completion, pod readiness, deployment rollout, resource creation.

```go
// Wait for a PipelineRun to succeed
Eventually(func(g Gomega) {
    pr, err := sharedClients.PipelineRunClient.Get(ctx, prName, metav1.GetOptions{})
    g.Expect(err).NotTo(HaveOccurred())
    g.Expect(pr.Status.GetCondition(apis.ConditionSucceeded).IsTrue()).To(BeTrue())
}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Succeed())

// Assert something does NOT change (e.g., no extra pods created)
Consistently(func() int {
    pods, _ := sharedClients.KubeClient.Kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
    return len(pods.Items)
}).WithTimeout(config.ConsistentlyDuration).WithPolling(config.APIRetry).Should(Equal(1))
```

**Use `config.APITimeout` (10 minutes) and `config.APIRetry` (5 seconds) as defaults.** These are already defined in `pkg/config/config.go`.

**Confidence:** HIGH

### Pattern 6: Ordered Containers for Sequential Workflows

**What:** Use `Ordered` decorator for multi-step workflows where later steps depend on earlier results.
**When:** PAC integration tests, operator configuration sequences, any Gauge scenario with dependent steps.

```go
Describe("PAC GitLab Integration", Ordered, Label("pac", "e2e"), func() {
    var (
        gitlabClient *gitlab.Client
        namespace    string
    )

    BeforeAll(func() {
        namespace = fmt.Sprintf("release-test-%s", uuid.New().String()[:8])
        oc.CreateNewProject(namespace)
        DeferCleanup(func() {
            oc.DeleteProjectIgnoreErors(namespace)
        })
        gitlabClient = pac.InitGitLabClient()
    })

    It("creates GitLab project configuration: PIPELINES-30-TC01", func() {
        // Step 1: uses gitlabClient and namespace from BeforeAll
    })

    It("validates PipelineRun completes: PIPELINES-30-TC02", func() {
        // Step 2: depends on step 1 having created resources
    })
})
```

**Key rules:**
- Variables shared across `It` blocks must be declared in the `Describe` closure (not initialized there -- see Pitfall 3).
- Use `BeforeAll` (not `BeforeEach`) for one-time setup in ordered containers.
- If any `It` fails, subsequent `It` blocks in the `Ordered` container are skipped.

**Confidence:** HIGH

### Pattern 7: DescribeTable for Data-Driven Tests

**What:** `DescribeTable`/`Entry` generates one `It` per entry. Replaces Gauge data tables.
**When:** Ecosystem tasks (36 tests with same flow, different parameters), version checks.

```go
DescribeTable("Ecosystem Task Pipelines",
    func(taskName, yamlFixture, expectedStatus string) {
        namespace := fmt.Sprintf("release-test-%s", uuid.New().String()[:8])
        oc.CreateNewProject(namespace)
        DeferCleanup(func() { oc.DeleteProjectIgnoreErors(namespace) })

        oc.Create(yamlFixture, namespace)
        Eventually(func() string {
            // poll for pipeline completion
            return getPipelineRunStatus(sharedClients, taskName, namespace)
        }).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Equal(expectedStatus))
    },
    Label("ecosystem", "e2e"),
    Entry("buildah pipeline", "buildah", "ecosystem/buildah-pipeline.yaml", "Succeeded"),
    Entry("s2i-java pipeline", "s2i-java", "ecosystem/s2i-java-pipeline.yaml", "Succeeded"),
    Entry("git-clone task", "git-clone", "ecosystem/git-clone-pipeline.yaml", "Succeeded"),
)
```

**Critical pitfall (Pitfall 4):** Entry parameters are evaluated at *tree construction time*, before any `BeforeEach` runs. Only use literal values, constants, or pure functions in `Entry()` arguments. Dynamic data from `BeforeEach` must be accessed inside the table body function, not passed as Entry parameters.

**Confidence:** HIGH

### Pattern 8: Skip for Conditional Tests

**What:** Skip tests based on runtime cluster state (disconnected, architecture, missing prerequisites).
**When:** Disconnected cluster tests, arch-specific tests, tests requiring specific operator components.

```go
// Option A: Skip in BeforeEach (applies to all specs in container)
BeforeEach(func() {
    if !config.Flags.IsDisconnected {
        Skip("requires disconnected cluster")
    }
})

// Option B: Skip in individual It block
It("validates air-gapped registry", Label("disconnected"), func() {
    if !config.Flags.IsDisconnected {
        Skip("requires disconnected cluster")
    }
    // test logic
})

// Option C: Skip based on architecture
It("validates ARM-specific behavior", func() {
    if config.Flags.ClusterArch != "arm64" {
        Skip("requires ARM64 cluster")
    }
    // test logic
})
```

**Confidence:** HIGH

### Anti-Patterns to Avoid

- **Global mutable state for per-test data:** Do not use `store.PutScenarioData()` to pass data between Ginkgo `It` blocks. Use closure-scoped variables within `Describe`/`Context` blocks instead. The store is acceptable for suite-level data set in `BeforeSuite`.
- **Container-level variable initialization:** Declare variables in `Describe` scope, initialize them in `BeforeEach`. Container closures execute once during tree construction, not per-spec (Pitfall 3).
- **Nested BeforeSuite:** `BeforeSuite` must be at the top level of the suite file, never inside a `Describe` block. Use `BeforeAll` inside `Ordered` containers for group-specific setup.
- **Using `log.Printf` instead of `GinkgoWriter`:** Output from `log.Printf` is not captured in JUnit reports and interleaves in parallel output. Use `GinkgoWriter.Printf` for test-associated output. (Note: existing `pkg/oc` and `pkg/opc` use `log.Printf` -- this should be addressed incrementally, not in this phase.)
- **One giant suite for all tests:** Each test area must have its own `suite_test.go`. This enables running areas independently and parallelizing across areas.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Suite bootstrapping | Custom test runner | `ginkgo bootstrap` or manual `suite_test.go` with `RunSpecs` | Standard Ginkgo pattern. `ginkgo bootstrap` generates the file |
| Label-based test selection | Custom tag parser | `--label-filter` CLI flag with boolean operators | Built into Ginkgo CLI. Supports `&&`, `||`, `!`, `()` |
| Async resource polling | `time.Sleep` + retry loop | `Eventually(func() ...).WithTimeout().WithPolling().Should()` | Ginkgo/Gomega's async matchers provide timeout, polling, and clear failure messages |
| Cleanup ordering | Manual cleanup list | `DeferCleanup` with LIFO ordering | Ginkgo manages the stack. Runs even on failure/panic |
| Data-driven test generation | `for` loop with `It` inside | `DescribeTable` / `Entry` | Ginkgo generates proper test names and JUnit entries per `Entry` |
| Sequential test ordering | Numbered test functions | `Ordered` decorator on `Describe` | Ginkgo skips remaining specs on failure, provides `BeforeAll`/`AfterAll` |

**Key insight:** Every pattern needed for this phase is a first-class Ginkgo v2 feature. There is no need for custom infrastructure beyond the standard `suite_test.go` bootstrap.

## Common Pitfalls

### Pitfall 1: Missing suite_test.go Causes Silent Test Discovery Failure
**What goes wrong:** Without `suite_test.go`, `ginkgo run` silently skips the package. `go test` compiles and runs without the Ginkgo runner, losing labels, reporters, and parallel support.
**Why it happens:** Ginkgo requires explicit bootstrap. Gauge discovers specs automatically.
**How to avoid:** Create `suite_test.go` as the FIRST file in each test area directory. Verify with `ginkgo run ./tests/...`.
**Warning signs:** JUnit report is empty. Labels and `--label-filter` have no effect. `BeforeSuite` never executes.

### Pitfall 2: Container-Level Variable Initialization Causes Spec Pollution
**What goes wrong:** Variables initialized in `Describe`/`Context` closures are shared across all child specs. One spec's mutations leak to the next.
**Why it happens:** Container closures execute once during tree construction. The mental model of "container = scope" is wrong -- containers define tree structure, not execution scope.
**How to avoid:** Strict rule: "Declare in containers, initialize in `BeforeEach`."
**Warning signs:** Tests pass in default order but fail with `--randomize-all`.

### Pitfall 3: BeforeSuite Runs Even When All Specs Are Filtered Out
**What goes wrong:** `--label-filter` eliminates all specs in a package, but `BeforeSuite` still executes. If `BeforeSuite` is expensive (cluster connection, OPC download), this wastes time.
**Why it happens:** Ginkgo runs `BeforeSuite` before evaluating label filters at the spec level.
**How to avoid:** Keep `BeforeSuite` lightweight -- just client initialization (< 5 seconds). Move expensive setup to `BeforeEach` or `BeforeAll` in specific test containers.
**Warning signs:** `--label-filter` runs take longer than expected. Empty suites show client initialization logs.

### Pitfall 4: DeferCleanup Context Cancellation Causes Resource Leaks
**What goes wrong:** Capturing `SpecContext` from `It` and using it in `DeferCleanup` -- the context is cancelled before cleanup runs.
**Why it happens:** Ginkgo cancels the spec context when the spec completes, before running cleanup callbacks.
**How to avoid:** Use the callback-with-context form: `DeferCleanup(func(ctx context.Context) { ... })`.
**Warning signs:** Cleanup logs show "context canceled" errors. Orphaned test namespaces on the cluster.

### Pitfall 5: DescribeTable Entry Parameters Evaluated at Tree Construction Time
**What goes wrong:** Passing `BeforeEach`-initialized variables as `Entry` parameters -- they are zero-valued because `Entry` args are captured before any setup runs.
**Why it happens:** Ginkgo generates `It` nodes during tree construction. `Entry(...)` arguments are evaluated immediately.
**How to avoid:** Only use literals or constants in `Entry()` arguments. Access dynamic data inside the table body function.
**Warning signs:** Nil pointer panics during tree construction. All entries show zero values.

### Pitfall 6: store.Namespace() Returns Empty String
**What goes wrong:** `oc.DeleteResource()` (line 110 in `oc.go`) calls `store.Namespace()` which returns `""` if the scenario store was not populated. This causes `oc delete` to target the wrong namespace.
**Why it happens:** The store is a Gauge pattern. In Ginkgo, the scenario store is not automatically populated.
**How to avoid:** Pass namespace explicitly as a parameter instead of relying on `store.Namespace()`. For Phase 2, all new test patterns must pass namespace explicitly.
**Warning signs:** Resources created in one namespace, delete attempted in a different one or the default namespace.

### Pitfall 7: Ginkgo Default Suite Timeout of 1 Hour
**What goes wrong:** Full E2E suite with 110+ tests can exceed the default 1-hour timeout, causing Ginkgo to cancel all contexts including cleanup.
**Why it happens:** Ginkgo v2 changed default from 24h to 1h.
**How to avoid:** Set `--timeout=4h` in CI invocations. Not critical for Phase 2 (scaffolding only), but document it for Phase 10 (CI).
**Warning signs:** Suite-level "timed out" errors in CI.

## Code Examples

Verified patterns from official Ginkgo documentation and codebase analysis.

### Complete suite_test.go Template (for all 11 suites)

```go
package <area>_test

import (
    "testing"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

var sharedClients *clients.Clients

func Test<Area>(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "<Area> Suite", Label("<area>"))
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

var _ = AfterSuite(func() {
    _ = config.RemoveTempDir()
})
```

### Sample Spec with DeferCleanup and Eventually

```go
var _ = Describe("Pipeline Operations", Label("pipelines", "e2e"), func() {
    var namespace string

    BeforeEach(func() {
        namespace = fmt.Sprintf("release-test-%s", generateShortID())
        oc.CreateNewProject(namespace)
        DeferCleanup(func() {
            oc.DeleteProjectIgnoreErors(namespace)
        })
    })

    It("runs a basic pipeline: PIPELINES-XX-TC01", Label("sanity"), func() {
        oc.Create("pipelines/basic-pipeline.yaml", namespace)

        Eventually(func(g Gomega) {
            result := cmd.Run("oc", "get", "pipelinerun", "-n", namespace, "-o", "jsonpath={.items[0].status.conditions[0].status}")
            g.Expect(result.Stdout()).To(Equal("True"))
        }).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Succeed())
    })
})
```

### Sample Ordered Container

```go
var _ = Describe("PAC GitLab Integration", Ordered, Label("pac", "e2e"), func() {
    var (
        gitlabClient *gitlab.Client
        namespace    string
    )

    BeforeAll(func() {
        namespace = fmt.Sprintf("release-test-%s", generateShortID())
        oc.CreateNewProject(namespace)
        DeferCleanup(func() {
            oc.DeleteProjectIgnoreErors(namespace)
        })
        gitlabClient = pac.InitGitLabClient()
    })

    It("creates GitLab project: PIPELINES-30-TC01", func() {
        Expect(gitlabClient).NotTo(BeNil())
        // setup project
    })

    It("validates PipelineRun: PIPELINES-30-TC02", func() {
        // depends on previous step
    })
})
```

### Sample DescribeTable

```go
DescribeTable("Component version checks",
    func(component, envVar string) {
        expectedVersion := os.Getenv(envVar)
        if expectedVersion == "" {
            Skip(fmt.Sprintf("%s version not set in environment", envVar))
        }
        opc.AssertComponentVersion(expectedVersion, component)
    },
    Label("versions", "sanity"),
    Entry("pipeline version", "pipeline", "PIPELINE_VERSION"),
    Entry("triggers version", "triggers", "TRIGGERS_VERSION"),
    Entry("chains version", "chains", "CHAINS_VERSION"),
    Entry("operator version", "operator", "OPERATOR_VERSION"),
)
```

### Sample Skip Pattern

```go
It("validates disconnected registry", Label("disconnected"), func() {
    if !config.Flags.IsDisconnected {
        Skip("requires disconnected cluster (set IS_DISCONNECTED=true)")
    }
    // test logic for disconnected environment
})
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Gauge implicit step ordering | Ginkgo `Ordered` decorator | Ginkgo v2 (2022) | Explicit ordering. Specs skipped on prior failure. `BeforeAll`/`AfterAll` available |
| Gauge data tables | Ginkgo `DescribeTable`/`Entry` | Ginkgo v1 (existed), improved in v2 | Each Entry generates its own JUnit entry. Labels can be applied per-entry |
| Gauge tags (`@sanity`) | Ginkgo `Label("sanity")` | Ginkgo v2 (2022) | Boolean filter expressions. Labels inherited from parents. Suite-level labels |
| Custom Ginkgo reporter interface | `ReportAfterEach`/`ReportAfterSuite` | Ginkgo v2 (2022) | Custom reporters removed in v2. Use reporting nodes instead |
| `time.Sleep` polling | `Eventually`/`Consistently` | Available since Gomega v1 | Timeout, polling, clear failure messages, context-aware |
| `AfterEach` for cleanup | `DeferCleanup` | Ginkgo v2 (2022) | Co-located with creation, LIFO order, context-safe |

**Deprecated/outdated:**
- Custom Ginkgo reporter interfaces: Removed in Ginkgo v2. Use `ReportAfterEach`/`ReportAfterSuite`.
- `Measure` blocks: Removed in Ginkgo v2. Use `MustPassRepeatedly` or `b.N` benchmarks.

## Open Questions

1. **Should `pkg/store` scenario data be cleared between specs automatically?**
   - What we know: Gauge cleared scenario data automatically between scenarios. Ginkgo does not.
   - What's unclear: Whether existing helper functions in `pkg/oc`, `pkg/opc`, `pkg/pac` depend on scenario store being populated.
   - Recommendation: For Phase 2, do NOT add automatic clearing. Instead, establish the pattern that new tests pass data via closure variables. Address store cleanup in Phase 3+ when migrating actual tests that use the store.

2. **Should `pkg/clients/clients.go` context cancellation be uncommented?**
   - What we know: Lines 72-74 have `ctx, cancel := context.WithCancel(ctx)` commented out. Without cancellation, goroutines from informers/watches may leak.
   - What's unclear: Whether any existing code starts long-lived goroutines from the client context.
   - Recommendation: Fix in this phase. Store the `cancel` function on the `Clients` struct (or return it alongside). Call `cancel()` in `AfterSuite`. Low risk, prevents cumulative leaks across 110+ tests.

3. **Should `oc.DeleteResource()` be refactored to accept namespace parameter?**
   - What we know: `oc.DeleteResource()` calls `store.Namespace()` which returns `""` if store is not populated (line 110 of `oc.go`). This is a footgun for new tests.
   - What's unclear: How many existing helper functions depend on `store.Namespace()`.
   - Recommendation: Defer to Phase 3+. For Phase 2 sample specs, use `oc.DeleteResourceInNamespace()` instead, or `oc.DeleteProjectIgnoreErors()` for namespace-level cleanup.

4. **Should there be a `testdata/` directory created in this phase?**
   - What we know: `config.Dir()` returns a path to a `template/` directory that does not exist yet. `config.File()` and `config.Path()` resolve relative to this directory.
   - What's unclear: Whether test fixtures should go in `template/` (matching the existing config pattern) or `testdata/` (Go convention).
   - Recommendation: Defer to Phase 3+. Phase 2 is scaffolding only -- no actual test fixtures are needed yet. When needed, use `template/` to match the existing `config.Path()` resolution or update `config.Dir()` if `testdata/` is preferred.

5. **How should the PAC test file (`tests/pac/pac_test.go`) be updated?**
   - What we know: It exists with one active `It` block and several commented-out blocks. It has no `suite_test.go`.
   - What's unclear: Whether to leave the commented code or restructure it.
   - Recommendation: Add `suite_test.go` to `tests/pac/`. Leave `pac_test.go` as-is for now -- it will be properly migrated in Phase 8 (PAC migration). The `suite_test.go` addition should not break the existing test.

## Specific Implementation Notes

### Existing Code Interactions

1. **`pkg/clients.NewClients()` signature:** `func NewClients(configPath string, clusterName, namespace string) (*Clients, error)` -- takes kubeconfig path, cluster name, and namespace. The namespace is used to initialize namespace-scoped clients (PipelineClient, TaskClient, etc.) via `NewClientSet(namespace)`.

2. **`pkg/config.Flags`:** Already parsed via `flag` package. Provides `Kubeconfig`, `Cluster`, `IsDisconnected`, `ClusterArch`, `Channel`, `CSV`, etc. These are available globally without any additional initialization.

3. **`pkg/store` interaction with `pkg/oc`:** `oc.DeleteResource()` at line 110 calls `store.Namespace()`. New tests should avoid this function and use `oc.DeleteResourceInNamespace()` instead, or clean up via namespace deletion.

4. **`pkg/pac.InitGitLabClient()`:** Creates a secret in `store.Namespace()` if it does not exist. This function depends on the store being populated. For Phase 2 sample specs, this dependency must be noted but not fixed (Phase 8 PAC migration will address it).

5. **Flag parsing timing:** `pkg/config.initializeFlags()` runs during package init. However, `flag.Parse()` must be called before flags are usable. In Ginkgo, `flag.Parse()` is called by `testing.T` before `BeforeSuite`. This means flags are available in `BeforeSuite` but NOT during package init or `Describe` body evaluation (tree construction time).

### Directory Creation Checklist

These directories need to be created (they do not exist today):

| Directory | Exists? | Action |
|-----------|---------|--------|
| `tests/ecosystem/` | No | Create with `suite_test.go` |
| `tests/triggers/` | No | Create with `suite_test.go` |
| `tests/operator/` | No | Create with `suite_test.go` |
| `tests/pipelines/` | No | Create with `suite_test.go` |
| `tests/pac/` | Yes | Add `suite_test.go` only |
| `tests/chains/` | No | Create with `suite_test.go` |
| `tests/results/` | No | Create with `suite_test.go` |
| `tests/mag/` | No | Create with `suite_test.go` |
| `tests/metrics/` | No | Create with `suite_test.go` |
| `tests/versions/` | No | Create with `suite_test.go` |
| `tests/olm/` | No | Create with `suite_test.go` |

### Compilation Verification

After creating all `suite_test.go` files, verify with:
```bash
# Must compile without errors
go build ./tests/...

# Must discover all 11 suites
ginkgo list ./tests/...

# Must compile and report 0 specs (since no It blocks yet in new suites)
go vet ./tests/...
```

## Sources

### Primary (HIGH confidence)
- Ginkgo v2 Official Documentation: suite bootstrap, BeforeSuite/AfterSuite, Labels, Ordered, DescribeTable, DeferCleanup, Eventually -- https://onsi.github.io/ginkgo/
- Gomega Official Documentation: Eventually, Consistently, matchers -- https://onsi.github.io/gomega/
- Direct codebase analysis: `pkg/clients/clients.go`, `pkg/config/config.go`, `pkg/store/store.go`, `pkg/oc/oc.go`, `pkg/pac/pac.go`, `pkg/cmd/cmd.go`, `tests/pac/pac_test.go`, `go.mod`
- Phase 1 completion state: `go.mod` confirms module path `github.com/openshift-pipelines/release-tests-ginkgo`, Go 1.24, Ginkgo v2.28.1, Gomega v1.39.1

### Secondary (MEDIUM confidence)
- Kubernetes E2E Testing Best Practices, Reloaded -- https://www.kubernetes.dev/blog/2023/04/12/e2e-testing-best-practices-reloaded/
- Kubebuilder test scaffolding -- https://book.kubebuilder.io/cronjob-tutorial/writing-tests.html
- Architecture research (`.planning/research/ARCHITECTURE.md`) -- project-specific patterns
- Features research (`.planning/research/FEATURES.md`) -- feature landscape
- Pitfalls research (`.planning/research/PITFALLS.md`) -- known gotchas

### Tertiary (LOW confidence)
- None -- all findings verified against official docs or direct codebase inspection

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- all libraries already in go.mod, APIs verified against codebase
- Architecture: HIGH -- patterns verified against official Ginkgo docs and existing `tests/pac/pac_test.go`
- Pitfalls: HIGH -- pitfalls documented in `.planning/research/PITFALLS.md` and cross-verified with Ginkgo issue tracker

**Research date:** 2026-03-31
**Valid until:** 2026-04-30 (stable domain -- Ginkgo v2 API is settled)
