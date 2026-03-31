# Pitfalls Research

**Domain:** Gauge-to-Ginkgo test framework migration for OpenShift Pipelines operator E2E tests
**Researched:** 2026-03-31
**Confidence:** HIGH (verified against Ginkgo official docs, Kubernetes E2E best practices, and this codebase)

## Critical Pitfalls

### Pitfall 1: Dual Store Package Creates Silent State Corruption

**What goes wrong:**
The codebase has TWO store packages: `pkg/store` (local, Ginkgo-aware, with mutex) and `github.com/openshift-pipelines/release-tests/pkg/store` (imported from old Gauge repo). The `pkg/oc/oc.go` file imports the Gauge repo's store (`release-tests/pkg/store`) while test code uses the local store (`srivickynesh/release-tests-ginkgo/pkg/store`). Functions like `oc.DeleteResource` call `store.Namespace()` on the *wrong* store -- the Gauge one, which is never populated in a Ginkgo context. This causes namespace resolution to return empty strings, leading to resource operations targeting the wrong namespace or failing silently.

**Why it happens:**
During initial migration, `pkg/oc/oc.go` was copied from the Gauge repo with its imports intact. The local `pkg/store` was created as the Ginkgo replacement, but `oc.go` was never updated to use it.

**How to avoid:**
- Audit every import of `github.com/openshift-pipelines/release-tests/pkg/*` across the codebase and replace with local equivalents before writing any tests that depend on those packages.
- Remove the `release-tests` dependency from `go.mod` entirely once all code is locally owned.
- Run `go vet` and write a simple grep-based CI check that flags any remaining imports from the Gauge repo.

**Warning signs:**
- Tests pass locally but resources are created/deleted in unexpected namespaces.
- `oc delete` commands hit empty namespace arguments.
- `store.Namespace()` returns `""` despite being set in test setup.

**Phase to address:**
Phase 1 (Foundation) -- must be fixed before any test migration begins. Every test depends on correct store resolution.

---

### Pitfall 2: Global Mutable State in Package Variables Breaks Parallel Execution and Test Isolation

**What goes wrong:**
The `pkg/store` package uses package-level `var scenarioStore` and `var suiteStore` maps. The `pkg/pac` package uses a package-level `var client *gitlab.Client`. When Ginkgo runs specs sequentially within a single process, mutations from one spec's `scenarioStore` entries leak into the next spec because nothing clears the map between `It` blocks. When running in parallel (separate processes), the store is empty in each process, causing `store.Clients()` to return `nil` and `store.Opc()` to panic.

**Why it happens:**
Gauge had a built-in scenario data store that was automatically cleared between scenarios. The current `pkg/store` mimics that API but without automatic cleanup. Ginkgo has no concept of "scenario lifecycle" -- you must use `BeforeEach`/`AfterEach` or `DeferCleanup` to manage per-spec state.

**How to avoid:**
- Add a `ClearScenarioStore()` function to `pkg/store` and call it in an `AfterEach` block in every test suite, or register it with `DeferCleanup` in a `BeforeEach`.
- Refactor away from global state entirely: pass `*clients.Clients` as a parameter or store it in a `BeforeEach`-initialized closure variable, not in a package-level map.
- For `pkg/pac`, remove the package-level `var client` and pass the `*gitlab.Client` through function parameters or return it for the caller to manage.
- Use `--race` flag when running tests to catch data races early.

**Warning signs:**
- Tests pass individually (`ginkgo --focus="specific test"`) but fail when run as a full suite.
- `store.Opc()` panics with "store: opc Cmd not set or wrong type."
- Intermittent failures that change based on spec execution order.
- Race detector warnings on `scenarioStore` map access.

**Phase to address:**
Phase 1 (Foundation) -- the store pattern must be redesigned before migrating tests. Every migrated test depends on this.

---

### Pitfall 3: Container-Level Variable Initialization Causes Spec Pollution

**What goes wrong:**
Developers migrating from Gauge initialize variables inside `Describe`/`Context` blocks (container nodes) instead of `BeforeEach` (setup nodes). Ginkgo evaluates container closures once during tree construction, so the variable is shared across all `It` specs in that container. If one spec mutates the variable, subsequent specs see the mutated value. This is the single most common Ginkgo bug and it is especially insidious during migration because Gauge scenarios were naturally isolated.

**Why it happens:**
Gauge scenarios each got fresh state. Developers accustomed to that model naturally write `var namespace = "test-ns"` at the `Describe` level, not realizing Ginkgo shares that instance across all child specs. The mental model of "container = scope" is wrong in Ginkgo -- containers define *tree structure*, not *execution scope*.

**How to avoid:**
- Establish a strict rule: "Declare in containers, initialize in `BeforeEach`."
- Use this pattern consistently:
  ```go
  var _ = Describe("Feature", func() {
      var ns string           // DECLARE here
      BeforeEach(func() {
          ns = "test-" + uuid  // INITIALIZE here
      })
      It("does something", func() {
          // USE ns here
      })
  })
  ```
- Add a linting note or code review checklist item: "Are any variables assigned in Describe/Context closures?"
- Enable `--randomize-all` from day one to surface pollution immediately.

**Warning signs:**
- Tests pass when run in default order but fail with `--randomize-all`.
- A test's assertions fail with values that belong to a different test.
- `namespace` or `client` variables have unexpected values.

**Phase to address:**
Phase 1 (Foundation) -- establish the pattern in the first migrated tests (sanity suite). Enforce via code review for all subsequent phases.

---

### Pitfall 4: DescribeTable Entry Parameters Evaluated at Tree Construction Time, Not Run Time

**What goes wrong:**
When migrating Gauge data tables to Ginkgo `DescribeTable`/`Entry`, developers pass variables set in `BeforeEach` as `Entry` parameters. Since `Entry` parameters are evaluated during tree construction (before any `BeforeEach` runs), these variables are zero-valued or nil, causing nil pointer panics or incorrect test data.

**Why it happens:**
Gauge data tables are evaluated at runtime with full context. Ginkgo's `DescribeTable` generates `It` nodes during tree construction, and `Entry(...)` arguments are captured immediately. This is a fundamental semantic difference that is not obvious from the API surface.

**How to avoid:**
- Entry parameters must be literal values, constants, or values computable without test setup:
  ```go
  DescribeTable("pipeline tasks",
      func(taskName string, expectedOutput string) {
          // BeforeEach-initialized state is available HERE inside the closure
          // but NOT in the Entry(...) argument list
      },
      Entry("buildah task", "buildah", "expected output"),  // literals only
      Entry("git-clone task", "git-clone", "expected output"),
  )
  ```
- If you need dynamic data from `BeforeEach`, access it inside the `DescribeTable` body function, not in `Entry` arguments.
- Document this rule prominently in the migration guide: "Entry parameters are compile-time constants."

**Warning signs:**
- Nil pointer panics during test tree construction (before any test runs).
- All entries in a table show the same (zero) values.
- Test failures at import time rather than during spec execution.

**Phase to address:**
Phase 2 (Ecosystem task migration) -- this is where data tables are first heavily used (36 tests with parameterized task names, images, expected outputs).

---

### Pitfall 5: Ginkgo v2 Default Timeout of 1 Hour Silently Kills E2E Suites in CI

**What goes wrong:**
Ginkgo v2 changed the default suite timeout from 24 hours to 1 hour, and this timeout applies to the *entire* suite run (not per-spec). OpenShift Pipelines E2E tests against a live cluster involve resource creation with Tekton Results finalizers (150+ second waits), operator reconciliation loops, and image pulls. A suite of 110+ tests with 5-minute deletion timeouts can easily exceed 1 hour. When the timeout fires, Ginkgo cancels *all* contexts including cleanup callbacks, leaving orphaned namespaces and resources on the cluster.

**Why it happens:**
The timeout change was made in Ginkgo v2 (documented in the migration guide) but developers who start with Ginkgo v2 directly (not migrating from v1) never encounter the migration docs that warn about this. The 1-hour default feels generous until you have 100+ E2E tests against a real cluster.

**How to avoid:**
- Set `--timeout=4h` (or appropriate value) explicitly in all CI pipeline invocations and in the test suite Makefile/scripts.
- Add per-spec timeouts using `SpecTimeout(10 * time.Minute)` or `NodeTimeout(5 * time.Minute)` to prevent one stuck test from consuming the entire budget.
- Monitor CI run durations and set alerts when total time exceeds 75% of the configured timeout.
- Never rely on the default -- always set timeout explicitly.

**Warning signs:**
- CI runs that were passing suddenly fail with "Ginkgo timed out" or "context deadline exceeded."
- Cleanup callbacks report "context canceled" errors.
- Orphaned `test-*` namespaces accumulate on the CI cluster.
- JUnit report shows tests as "interrupted" rather than passed or failed.

**Phase to address:**
Phase 1 (Foundation) -- set the timeout in the suite bootstrap and CI configuration before any tests run.

---

### Pitfall 6: JUnit XML Output Incompatible with Polarion Uploader

**What goes wrong:**
Ginkgo's built-in JUnit reporter (`--junit-report`) generates XML that does not match Polarion's JUMP tool expectations. Specifically: (1) missing `<properties>` section at the test suite level (Polarion requires project ID here), (2) `<system-out>` elements absent from passed test cases, and (3) test case names formatted as nested `[Describe] [Context] [It]` paths instead of flat test IDs that Polarion maps to test case IDs. The result: Polarion import succeeds but shows zero completed tests, or rejects the file entirely with "Project id not specified or invalid."

**Why it happens:**
Ginkgo's JUnit format follows the JUnit XML schema but Polarion's JUMP importer has stricter/different expectations. The Gauge reporter likely produced output specifically tuned for the Polarion uploader. This is a known issue documented by the Submariner project.

**How to avoid:**
- Build a post-processing step (Go script or XSLT transform) that runs after `ginkgo --junit-report` and before Polarion upload. This step should:
  1. Inject `<properties>` with the Polarion project ID.
  2. Map Ginkgo's hierarchical test names to Polarion test case IDs (e.g., `PIPELINES-30-TC01`).
  3. Ensure `<system-out>` elements exist for all test cases.
- Embed Polarion test case IDs in Ginkgo test descriptions or labels so the mapping is deterministic.
- Test the full pipeline (Ginkgo -> JUnit XML -> transform -> Polarion upload) early with a small subset of tests, not after all 110 tests are migrated.
- Consider using `ReportAfterSuite` to generate a custom JUnit report directly instead of relying on CLI flags.

**Warning signs:**
- Polarion shows "0 tests executed" after importing JUnit XML.
- Import errors mentioning "Project id not specified."
- Test case IDs in Polarion don't match Ginkgo test names.

**Phase to address:**
Phase 1 (Foundation) -- validate JUnit-to-Polarion pipeline with the first 9 sanity tests. Do not defer this to the end.

---

### Pitfall 7: Tekton Results Finalizers Cause 300-Second Cleanup Timeouts That Cascade

**What goes wrong:**
Tekton Results adds finalizers to PipelineRuns and TaskRuns that block deletion for 150+ seconds (configured via `store_deadline` and `forward_buffer`). The existing `oc.Delete` and `oc.DeleteResource` functions already use a 300-second timeout per resource. In a suite of 110+ tests, if each test creates and deletes resources, the cumulative cleanup time alone can be 30+ minutes, pushing the suite past CI timeouts. Worse, if a timeout fires mid-cleanup, Ginkgo cancels the context and orphans the resource.

**Why it happens:**
This is inherent to the OpenShift Pipelines environment -- Tekton Results needs time to forward data. The Gauge test suite had the same issue but each scenario ran independently. In Ginkgo, the cumulative effect across the full suite is more visible and damaging.

**How to avoid:**
- Use `DeferCleanup` with explicit `NodeTimeout` for cleanup operations: `DeferCleanup(func(ctx context.Context) { ... }, NodeTimeout(5*time.Minute))`.
- Implement async deletion with polling: issue the delete command and then use `Eventually` to confirm deletion, rather than blocking on synchronous `oc delete`.
- Configure the test cluster's Tekton Results to use shorter `store_deadline` and `forward_buffer` values for the test environment.
- Create namespaces with unique prefixes (`test-<uuid>`) and use a global `AfterSuite` cleanup that deletes all `test-*` namespaces, rather than relying on per-test cleanup.
- Account for cleanup time in the suite timeout budget (add 1 minute per test as buffer).

**Warning signs:**
- Individual tests pass but the suite times out.
- `oc delete` commands hang in CI logs for 5 minutes each.
- Cluster has dozens of `test-*` namespaces from aborted runs.

**Phase to address:**
Phase 2 (Ecosystem tasks) -- this is where the problem first manifests at scale (36 tests with resource creation/deletion). Phase 1 (sanity tests) should prototype the cleanup pattern.

---

### Pitfall 8: Missing Suite Bootstrap Files Cause Silent Test Discovery Failures

**What goes wrong:**
Ginkgo requires a `suite_test.go` file with `TestSuiteName(t *testing.T) { RegisterFailHandler(Fail); RunSpecs(t, "Suite Name") }` in each test package. The current repo has `tests/pac/pac_test.go` but NO `suite_test.go` file anywhere. Without it, `ginkgo run` silently skips the package or `go test` runs it without the Ginkgo runner (losing labels, reporters, parallel support). Tests may appear to pass in `go test` but produce no JUnit output.

**Why it happens:**
In Gauge, the runner discovers `.spec` files automatically. In Ginkgo, the bootstrap is explicit. Developers migrating tests may create `_test.go` files without the bootstrap, and `go test` will still compile and run them (without Ginkgo features) -- masking the problem.

**How to avoid:**
- Create `suite_test.go` in every test package directory as the FIRST step of migration.
- Use `ginkgo bootstrap` to generate the file automatically.
- Add a CI check that verifies every directory containing `*_test.go` also contains a `suite_test.go`.
- Always run tests via `ginkgo run` (not `go test`) to ensure the Ginkgo runner is active.

**Warning signs:**
- `ginkgo run ./...` reports "no test suites found" for directories with test files.
- JUnit report is empty despite tests appearing to pass.
- Labels and `--label-filter` have no effect.
- `BeforeSuite`/`AfterSuite` never execute.

**Phase to address:**
Phase 1 (Foundation) -- create all `suite_test.go` files as part of directory structure setup, before migrating any tests.

---

## Moderate Pitfalls

### Pitfall 9: Label Filtering Silently Ignores FIt/FDescribe Focus Markers

**What goes wrong:**
When using `--label-filter` (e.g., `--label-filter="sanity"` for CI runs), Ginkgo treats `FIt` and `FDescribe` focus markers as regular `It`/`Describe` nodes. A developer debugging locally may add `FIt` to focus on one test, push the code, and CI (which uses `--label-filter`) runs ALL tests in the label set, not just the focused one. The `FIt` is silently ignored. Conversely, when another developer runs locally without `--label-filter`, only the focused test runs and all others are skipped -- but CI reports success.

**Prevention:**
- Use `ginkgo --fail-on-focused` in CI to fail the suite if any `FIt`/`FDescribe`/`FContext` is present.
- Add a pre-commit hook or linting rule that rejects `FIt`, `FDescribe`, `FContext`, `FEntry`.
- Document this behavior in the contributing guide.

**Phase to address:** Phase 1 (Foundation) -- add `--fail-on-focused` to CI configuration.

---

### Pitfall 10: BeforeSuite Runs Even When All Specs Are Filtered Out by Labels

**What goes wrong:**
When `--label-filter` eliminates all specs in a package, `BeforeSuite` still executes. If `BeforeSuite` does expensive setup (cluster connection, OPC binary download, client initialization), this wasted work adds minutes per empty package. With tests organized across multiple packages (sanity, ecosystem, triggers, operator, pipelines, PAC, chains, results), running `--label-filter="sanity"` still triggers `BeforeSuite` in all 8+ packages.

**Prevention:**
- Use `SynchronizedBeforeSuite` and keep setup lightweight (just client init).
- Move expensive setup (OPC download, deployment validation) into `BeforeEach` blocks of the specs that need them, protected by label checks.
- Alternatively, consolidate tests into fewer packages so fewer `BeforeSuite` blocks execute.

**Phase to address:** Phase 1 (Foundation) -- design `BeforeSuite` to be fast and safe to run even when no specs match.

---

### Pitfall 11: Context Cancellation in DeferCleanup Causes Resource Leaks

**What goes wrong:**
Developers capture the `ctx` from `It(func(ctx context.Context) {...})` and pass it to `DeferCleanup`. When cleanup runs, this context is already canceled, so all API calls (`oc delete`, `k8s client.Delete()`) return immediately with "context canceled" errors. Resources are not deleted. This is the most common resource leak pattern in Kubernetes E2E tests with Ginkgo.

**Prevention:**
- Always use the callback-with-context pattern:
  ```go
  DeferCleanup(func(cleanupCtx context.Context) {
      // Use cleanupCtx, NOT the It's ctx
      deleteProject(cleanupCtx, namespace)
  })
  ```
- Never close over the `It` context in deferred functions.
- Establish this as a code review standard from test #1.

**Phase to address:** Phase 1 (Foundation) -- establish cleanup patterns in the sanity test suite.

---

### Pitfall 12: Commented-Out Context Cancellation in Client Initialization Leaks Goroutines

**What goes wrong:**
In `pkg/clients/clients.go:72-74`, the context cancellation is commented out:
```go
ctx := context.Background()
// ctx, cancel := context.WithCancel(ctx)
// defer cancel()
```
This means the context is never canceled, and any goroutines started by Kubernetes informers or watches using this context will leak. Over the course of a 110-test suite, accumulated goroutine leaks can exhaust file descriptors or memory.

**Prevention:**
- Uncomment the context cancellation but move `cancel()` to `AfterSuite`/`DeferCleanup` rather than using `defer cancel()` (which would cancel immediately after `NewClients` returns).
- Store the cancel function and call it during suite teardown.
- Use `goleak` to detect goroutine leaks in CI.

**Phase to address:** Phase 1 (Foundation) -- fix as part of client initialization refactoring.

---

### Pitfall 13: Panic in store.Opc() Crashes Entire Suite Instead of Failing One Spec

**What goes wrong:**
`store.Opc()` calls `panic("store: opc Cmd not set or wrong type")` when the OPC command is not initialized. In Ginkgo, an unrecovered panic in a spec marks that spec as panicked and Ginkgo continues. But if the panic happens during `BeforeSuite` or in a setup node shared across specs, it can crash the entire suite or leave the suite in an indeterminate state with confusing error output.

**Prevention:**
- Replace `panic()` with `Fail()` (Ginkgo's failure mechanism) or return an error.
- Ensure `PutSuiteData("opc", ...)` is called in `BeforeSuite`/`SynchronizedBeforeSuite` before any spec accesses it.
- Add a `BeforeEach` guard that checks `store.Opc()` availability with a meaningful error message.

**Phase to address:** Phase 1 (Foundation) -- fix `store.Opc()` before migrating tests that use OPC commands.

---

### Pitfall 14: Module Path Mismatch Blocks go get and CI Caching

**What goes wrong:**
`go.mod` declares `module github.com/srivickynesh/release-tests-ginkgo` but the repo is at `github.com/openshift-pipelines/release-tests-ginkgo`. Import paths in test files reference the `srivickynesh` path. This breaks `go get`, confuses dependency resolution, and prevents proper Go module caching in CI.

**Prevention:**
- Update `go.mod` module path to `github.com/openshift-pipelines/release-tests-ginkgo`.
- Find-and-replace all imports across the codebase.
- Do this before adding new test files to avoid compounding the problem.

**Phase to address:** Phase 1 (Foundation) -- fix before writing any new code.

---

## Minor Pitfalls

### Pitfall 15: Randomization Disabled by Default Hides Order-Dependent Tests

**What goes wrong:**
Tests that pass in the default alphabetical order fail when `--randomize-all` is enabled, revealing hidden order dependencies (e.g., one test creates a namespace that another test assumes exists).

**Prevention:**
- Enable `--randomize-all` in CI from day one.
- Fix order dependencies as they surface.
- Use `Ordered` decorator intentionally for genuinely ordered workflows (e.g., create-then-validate sequences) rather than leaving implicit dependencies.

**Phase to address:** Phase 1 (Foundation).

---

### Pitfall 16: FEntry/FDescribeTable Left in Code Silently Skips Other Table Entries

**What goes wrong:**
Debugging a single `Entry` by changing it to `FEntry` focuses just that entry. If committed, all other entries in the table are silently skipped. Unlike `FIt`, which is more visible, `FEntry` inside a large `DescribeTable` is easy to miss during code review.

**Prevention:**
- `--fail-on-focused` catches this in CI.
- Use `PEntry` (pending) for entries you want to skip, with a comment explaining why.

**Phase to address:** Phase 2 (Ecosystem tasks) -- where `DescribeTable` is most used.

---

### Pitfall 17: Typo in MustSuccedIncreasedTimeout Function Name

**What goes wrong:**
The function `MustSuccedIncreasedTimeout` (missing 'e' in Succeed) creates a confusing API. Developers may try to call `MustSucceedIncreasedTimeout` (correct spelling) and get a compile error, then waste time finding the typo.

**Prevention:**
- Rename to `MustSucceedIncreasedTimeout` before migration.
- Add a type alias or wrapper for backward compatibility if needed temporarily.

**Phase to address:** Phase 1 (Foundation).

---

## Technical Debt Patterns

Shortcuts that seem reasonable but create long-term problems.

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Keeping `release-tests` as a dependency | Saves time rewriting `pkg/cmd`, `pkg/config` | Two store packages, import confusion, version pinning on pseudo-version | Never -- fork needed code into local packages |
| Using package-level `var` for shared state | Mimics Gauge's data store API | Breaks parallelism, causes race conditions, prevents test isolation | Never in Ginkgo -- use `BeforeEach` closures |
| Using `log.Printf` instead of `GinkgoWriter` | Familiar API, works immediately | Output not captured in JUnit reports, interleaved in parallel output, not associated with specific tests | Only for debug during migration; replace with `GinkgoWriter.Printf` for production |
| Skipping `suite_test.go` creation | One less file to create | Tests run via `go test` without Ginkgo runner, losing labels/reporters/parallelism | Never |
| Hardcoding timeout values | Quick to write | Different clusters have different latencies; CI under load is slower than local | Only if configurable via env var fallback |

## Integration Gotchas

Common mistakes when connecting to external services.

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| OpenShift cluster API | Creating one `*clients.Clients` in `BeforeSuite` with namespace-scoped clients, then using it across specs that need different namespaces | Initialize `*clients.Clients` per-test in `BeforeEach` with the test-specific namespace, or call `NewClientSet(namespace)` per spec |
| Polarion JUnit upload | Assuming Ginkgo's `--junit-report` output is directly compatible | Build a post-processing transform that injects Polarion properties and maps test IDs |
| GitLab API (PAC tests) | Storing `*gitlab.Client` in a package-level variable | Pass client through function parameters; initialize in `BeforeEach` for the PAC test suite |
| Tekton Results | Not accounting for finalizer delays in cleanup timeouts | Use 300s+ timeouts for resource deletion, or disable Results finalizers in test environments |
| OPC binary download | Downloading in every parallel node's `BeforeSuite` | Use `SynchronizedBeforeSuite`: download on node 1, share path to all nodes |

## Performance Traps

Patterns that work at small scale but fail as the suite grows.

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Synchronous resource deletion per test | Suite runtime grows linearly with test count | Async delete + Eventually polling, or namespace-level bulk deletion | >30 tests with resource cleanup |
| Re-downloading OPC binary per suite run | 10-minute timeout per download, multiplied by parallel nodes | Cache binary on disk, check version before downloading | Parallel execution (N downloads) |
| Sequential `oc` commands with 90s timeouts | Individual tests take 5+ minutes even when passing | Use shorter timeouts for known-fast operations, parallelize independent commands | >50 tests in the suite |
| Creating unique namespaces without cleanup | Cluster accumulates hundreds of test namespaces over CI runs | Add `AfterSuite` that cleans up all `test-*` namespaces; add TTL-based cleanup script | >10 CI runs without manual cleanup |

## Security Mistakes

Domain-specific security issues in this test framework context.

| Mistake | Risk | Prevention |
|---------|------|------------|
| Logging GITLAB_TOKEN or GITLAB_WEBHOOK_TOKEN values | Token exposure in CI logs, JUnit reports | Validate env vars are set without logging values; use `GinkgoWriter` which can be filtered |
| `bash -c` with unsanitized input in `oc.CopySecret` | Command injection if secret JSON contains shell metacharacters | Use Go's `exec.Command` with argument arrays instead of shell interpolation |
| Secrets data returned as plain string from `GetSecretsData` | Secret values in test output and JUnit reports | Add secret redaction to any logging of return values |
| Hardcoded secret names (`gitlab-webhook-config`) | Collisions between parallel test runs | Prefix secret names with test-specific identifiers |

## "Looks Done But Isn't" Checklist

Things that appear complete but are missing critical pieces.

- [ ] **Test migration:** Tests compile and pass -- verify JUnit output is actually consumed by Polarion (not just generated)
- [ ] **Label system:** Labels are applied to tests -- verify `--label-filter` produces the expected subset (use `--dry-run -v`)
- [ ] **Parallel execution:** Tests pass with `-p` -- verify no shared namespace collisions, no store race conditions (run with `--race`)
- [ ] **Cleanup:** Tests pass -- verify no orphaned namespaces/resources on cluster after full suite run (`oc get projects | grep test-`)
- [ ] **CI pipeline:** Tests run in CI -- verify timeout is set explicitly, `--fail-on-focused` is enabled, JUnit is uploaded
- [ ] **Parity:** Ginkgo tests pass -- verify the same tests that pass in Gauge also pass in Ginkgo (cross-reference test IDs)
- [ ] **Store cleanup:** `scenarioStore` is cleared -- verify by running suite twice in a row without cluster reset
- [ ] **suite_test.go:** Each test directory has it -- verify by running `ginkgo run ./...` (not `go test`)

## Recovery Strategies

When pitfalls occur despite prevention, how to recover.

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Dual store corruption | LOW | Replace imports in `pkg/oc/oc.go`, retest all affected functions |
| Global state race conditions | MEDIUM | Refactor store to closure-based pattern, update all callers |
| Container-level initialization pollution | LOW | Move initialization to `BeforeEach`, one file at a time |
| DescribeTable entry nil panics | LOW | Move dynamic values into the table body function |
| CI timeout exceeded | LOW | Add `--timeout=4h` to CI command, add per-spec timeouts |
| JUnit/Polarion incompatibility | MEDIUM | Build XML transform script, validate with Polarion test import |
| Finalizer cleanup timeouts | MEDIUM | Implement async deletion pattern, update all cleanup calls |
| Missing suite_test.go | LOW | Run `ginkgo bootstrap` in each test directory |
| Label filter surprises | LOW | Add `--dry-run -v` validation step to CI before real run |
| Resource leaks on cluster | MEDIUM | Script to delete all `test-*` namespaces; add `AfterSuite` global cleanup |
| Module path mismatch | LOW | Find-replace all imports, update go.mod |

## Pitfall-to-Phase Mapping

How roadmap phases should address these pitfalls.

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Dual store package | Phase 1: Foundation | `grep -r "release-tests/pkg" .` returns zero results |
| Global mutable state | Phase 1: Foundation | Tests pass with `--race` and `--randomize-all` |
| Container-level init | Phase 1: Foundation, enforced all phases | Code review checklist item verified per PR |
| DescribeTable entry timing | Phase 2: Ecosystem tasks | No nil panics during tree construction |
| Suite timeout | Phase 1: Foundation | CI config has explicit `--timeout` flag |
| JUnit/Polarion compat | Phase 1: Foundation (validate), Phase 2+ (maintain) | Polarion import shows correct test count |
| Finalizer cleanup | Phase 1: Foundation (pattern), Phase 2+ (apply) | `oc get projects --no-headers | grep test- | wc -l` is 0 after suite |
| Missing suite_test.go | Phase 1: Foundation | `ginkgo run ./...` discovers all test packages |
| Label filter + FIt | Phase 1: Foundation | CI uses `--fail-on-focused`, `FIt` in code fails CI |
| BeforeSuite with no specs | Phase 1: Foundation | `--label-filter` runs complete in <30 seconds for non-matching packages |
| Context in DeferCleanup | Phase 1: Foundation (pattern), all phases (enforce) | Cleanup succeeds even when spec fails |
| Goroutine leak from context | Phase 1: Foundation | `goleak` check passes in CI |
| store.Opc() panic | Phase 1: Foundation | Replace panic with Ginkgo Fail |
| Module path | Phase 1: Foundation | `go build ./...` succeeds with correct module path |
| Randomization | Phase 1: Foundation | CI runs with `--randomize-all` |
| FEntry left in code | Phase 2+ | `--fail-on-focused` catches in CI |
| Function name typo | Phase 1: Foundation | Rename and verify compilation |

## Sources

- [Ginkgo Official Documentation -- Spec tree construction, variable scoping, parallel execution](https://onsi.github.io/ginkgo/) -- HIGH confidence
- [Ginkgo V2 Migration Guide -- timeout changes, CLI flag changes, custom reporter removal](https://onsi.github.io/ginkgo/MIGRATING_TO_V2) -- HIGH confidence
- [Kubernetes E2E Testing Best Practices, Reloaded -- DeferCleanup, context handling, namespace isolation](https://www.kubernetes.dev/blog/2023/04/12/e2e-testing-best-practices-reloaded/) -- HIGH confidence
- [Submariner Shipyard Issue #48 -- Polarion JUnit XML parsing failure with Ginkgo output](https://github.com/submariner-io/shipyard/issues/48) -- HIGH confidence (direct report of same Polarion issue)
- [Ginkgo Issue #440 -- shared variables in parallel execution](https://github.com/onsi/ginkgo/issues/440) -- HIGH confidence
- [Ginkgo Issue #530 -- intermittent test pollution with global Describe variables](https://github.com/onsi/ginkgo/issues/530) -- HIGH confidence
- [Ginkgo Issue #1221 -- focused specs ignored with label filtering](https://github.com/onsi/ginkgo/issues/1221) -- HIGH confidence
- [Ginkgo Issue #378 -- BeforeEach variables in DescribeTable Entry](https://github.com/onsi/ginkgo/issues/378) -- HIGH confidence
- [Ginkgo Issue #1119 -- labels not supported on BeforeSuite/BeforeEach](https://github.com/onsi/ginkgo/issues/1119) -- MEDIUM confidence
- [Speeding Up Kubernetes Controller Integration Tests with Ginkgo Parallelism](https://kev.fan/posts/04-k8s-ginkgo-parallel-tests/) -- MEDIUM confidence
- [OpenShift cluster-node-tuning-operator PR #517 -- Ginkgo v2 timeout migration](https://github.com/openshift/cluster-node-tuning-operator/pull/517) -- MEDIUM confidence
- [Ginkgo Issue #969 -- timeouts and cleanup interaction](https://github.com/onsi/ginkgo/issues/969) -- MEDIUM confidence
- [Effective Ginkgo/Gomega -- helper function patterns](https://medium.com/swlh/effective-ginkgo-gomega-b6c28d476a09) -- MEDIUM confidence
- Codebase analysis: `pkg/store/store.go`, `pkg/oc/oc.go`, `pkg/pac/pac.go`, `pkg/clients/clients.go`, `tests/pac/pac_test.go` -- HIGH confidence (direct observation)

---
*Pitfalls research for: Gauge-to-Ginkgo migration of OpenShift Pipelines release tests*
*Researched: 2026-03-31*
