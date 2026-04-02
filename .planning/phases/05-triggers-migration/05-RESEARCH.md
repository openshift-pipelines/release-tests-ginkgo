# Phase 5 Research: Triggers Migration

**Researched:** 2026-03-31
**Confidence:** HIGH
**Discovery Level:** 0 (standard Ginkgo migration patterns, no new dependencies)

## Test Inventory

23 total tests across 4 Gauge spec files in `specs/triggers/`:

### eventlistener.spec (17 tests, prefix PIPELINES-05)

| TC | Name | Tags | Status |
|----|------|------|--------|
| TC01 | Create Eventlistener | triggers, to-do | Inactive (old concept format) |
| TC02 | Create Eventlistener with github interceptor | triggers, to-do | Inactive |
| TC03 | Create EventListener with custom interceptor | triggers, to-do | Inactive |
| TC04 | Create EventListener with CEL interceptor with filter | triggers, to-do | Inactive |
| TC05 | Create EventListener with CEL interceptor without filter | triggers, to-do | Inactive |
| TC06 | Create EventListener with multiple interceptors | triggers, to-do | Inactive |
| TC07 | Create Eventlistener with TLS enabled | tls, triggers, admin, e2e | Active |
| TC08 | Create Eventlistener embedded TriggersBindings specs | e2e, triggers, non-admin, sanity | Active |
| TC09 | Create embedded TriggersTemplate | e2e, triggers, non-admin, sanity | Active |
| TC10 | Create Eventlistener with gitlab interceptor | e2e, triggers, non-admin | Active |
| TC11 | Create Eventlistener with bitbucket interceptor | e2e, triggers, non-admin | Active |
| TC12 | Verify Github push event with Embedded TriggerTemplate using Github-CTB | e2e, triggers, non-admin, sanity | Active |
| TC13 | Verify Github pull_request event with Embedded TriggerTemplate using Github-CTB | e2e, triggers, non-admin, sanity | Active |
| TC14 | Verify Github pr_review event with Embedded TriggerTemplate using Github-CTB | e2e, triggers, non-admin | Active |
| TC15 | Create TriggersCRD resource with CEL interceptors (overlays) | e2e, triggers, non-admin, sanity | Active |
| TC16 | Create multiple Eventlistener with TLS enabled | e2e, tls, triggers, admin, sanity | Active |
| TC17 | Create Eventlistener with github interceptor And verify Kubernetes Events | e2e, events, triggers, admin, sanity | Active |

### triggerbinding.spec (3 tests, prefix PIPELINES-10)

| TC | Name | Tags | Status |
|----|------|------|--------|
| TC01 | Verify CEL marshaljson function Test | e2e, triggers, non-admin, sanity | Active |
| TC02 | Verify event message body parsing with old annotation Test | e2e, triggers, non-admin, sanity | Active |
| TC03 | Verify event message body marshalling error Test | bug-to-fix, non-admin | Known bug |

### cron.spec (1 test, prefix PIPELINES-04)

| TC | Name | Tags | Status |
|----|------|------|--------|
| TC01 | Create Triggers using k8s cronJob | e2e, triggers, non-admin, sanity | Active |

### tutorial.spec (2 tests, prefix PIPELINES-06)

| TC | Name | Tags | Status |
|----|------|------|--------|
| TC01 | Run pipelines tutorials | e2e, integration, non-admin, pipelines, tutorial, skip-4.14 | Active (not triggers-tagged) |
| TC02 | Run pipelines tutorial using triggers | e2e, integration, triggers, non-admin, tutorial, sanity, skip-4.14 | Active |

## Classification

- **Active e2e tests:** 16 (TC07-TC17, TC01-TC02 triggerbinding, cron TC01, tutorial TC02)
- **Inactive/to-do tests:** 6 (eventlistener TC01-TC06, old concept format)
- **Known bug test:** 1 (triggerbinding TC03, tagged `bug-to-fix`)
- **Pipeline tutorial (not trigger-specific):** 1 (tutorial TC01, tagged `pipelines` not `triggers`)

**Decision:** Migrate all 23 tests. The 6 `to-do` tests get `Pending("to-do: not yet implemented")` in Ginkgo. The `bug-to-fix` test gets `Pending("bug-to-fix: known marshalling issue")`. This preserves test count parity with Gauge while clearly marking non-running tests.

## Common Test Pattern

Most trigger tests follow a consistent flow:

1. **Setup:** Create YAML resources (TriggerTemplate, TriggerBinding, RBAC, EventListener)
2. **Expose:** Expose EventListener service as OpenShift Route (plain HTTP or TLS)
3. **Trigger:** Mock POST event with interceptor-specific headers (github/gitlab/bitbucket/CEL)
4. **Assert:** Verify EventListener response body matches expected namespace/name
5. **Verify:** Check PipelineRun/TaskRun completes with expected status
6. **Cleanup:** Delete EventListener and wait for generated Deployment/Service/Route deletion

### Two Sub-Patterns

**Pattern A: Concept-based (TC01-TC06)**
Uses Gauge concepts (`.cpt` file) to create individual YAML files one at a time, then calls specialized steps like "Add Event listener" and "Mock push event". These are the `to-do` tagged tests.

**Pattern B: Table-based (TC07-TC17, triggerbinding, cron, tutorial)**
Uses Gauge data tables to create multiple YAML files at once via `Create <table>`, then uses parameterized steps for event mocking and verification. These are the active e2e tests.

## Existing Infrastructure

### Already in Ginkgo repo (`pkg/`)

- `pkg/oc/oc.go`: `Create()`, `CreateRemote()`, `Apply()`, `Delete()`, `EnableTLSConfigForEventlisteners()`, `VerifyKubernetesEventsForEventListener()`, `CreateSecretWithSecretToken()`, `LinkSecretToSA()`
- `pkg/cmd/cmd.go`: `MustSucceed()`, `Run()`, command execution
- `pkg/config/config.go`: `Path()`, `TriggersSecretToken` constant
- `pkg/clients/clients.go`: Kubernetes/Tekton/OpenShift clientsets including `TriggersClient`
- `pkg/store/store.go`: State management (scenario/suite stores)
- `pkg/opc/opc.go`: `VerifyResourceListMatchesName()` for resource verification
- `tests/triggers/suite_test.go`: Suite entry point with BeforeSuite/AfterSuite already created

### Must be created or ported from reference repo (`pkg/triggers/`)

The reference repo has `pkg/triggers/triggers.go` and `pkg/triggers/helper.go` with:

- `ExposeEventListner()` -- expose EL service as Route, return route URL
- `ExposeEventListenerForTLS()` -- TLS route with cert generation
- `MockPostEvent()` -- send HTTP POST with interceptor-specific headers
- `MockPostEventWithEmptyPayload()` -- empty POST for edge cases
- `AssertElResponse()` -- verify EventListener response body
- `CleanupTriggers()` -- delete EventListener and wait for generated resources to be removed
- `GetRoute()` / `GetRouteURL()` -- route URL retrieval
- `CreateHTTPClient()` / `CreateHTTPSClient()` -- HTTP clients for event mocking
- `GetSignature()` -- HMAC SHA256 for webhook validation
- `buildHeaders()` -- interceptor-specific header construction

These functions must be ported to `pkg/triggers/` in the Ginkgo repo, replacing Gauge's `testsuit.T` with Gomega assertions and removing `gauge.GetScenarioStore()` usage in favor of local variables.

### Must also port from reference repo (`pkg/pipelines/`)

The trigger tests use shared pipeline verification steps:
- `ValidatePipelineRun()` -- verify PipelineRun status
- `ValidateTaskRun()` -- verify TaskRun status
- `WatchForPipelineRun()` -- watch for new PipelineRun creation
- `AssertForNoNewPipelineRunCreation()` -- assert no new PipelineRuns
- `AssertNumberOfPipelineruns()` -- count PipelineRuns within timeout
- `GetLatestPipelinerun()` -- get most recent PipelineRun name

These may already be partially ported or will be ported in Phase 3 (sanity) or Phase 7 (pipelines core). The triggers phase should create them if they do not yet exist, or import them if they do.

### Testdata

Testdata YAML fixtures exist in the reference repo under `testdata/triggers/` with subdirectories:
- `bitbucket/`, `certs/`, `cron/`, `eventlisteners/`, `github-ctb/`, `gitlab/`, `triggerbindings/`, `triggersCRD/`, `triggertemplate/`

These must be copied to the Ginkgo repo's `testdata/triggers/` directory. The old concept-based testdata under `testdata/triggers/eventlisteners/triggertemplate/`, `triggerbinding/`, `role-resources/`, `custom-interceptor/` is needed only if the `to-do` tests are migrated as non-pending.

## Gauge-to-Ginkgo Mapping

### State Management

Gauge uses `store.PutScenarioData("route", routeurl)` to pass state between steps. In Ginkgo, each `It` block is a self-contained closure -- local variables replace the scenario store:

```go
// Gauge pattern:
store.PutScenarioData("route", routeurl)
// ... later step ...
triggers.MockPostEvent(store.GetScenarioData("route"), ...)

// Ginkgo pattern:
routeURL := triggers.ExposeEventListener(clients, elName, ns)
resp := triggers.MockPostEvent(routeURL, interceptor, eventType, payload, isTLS)
triggers.AssertElResponse(clients, resp, elName, ns)
```

### Data Table Mapping

Gauge data tables become sequential `oc.Create()` calls:

```go
// Gauge: Create <table> with rows of resource_dir
// Ginkgo:
oc.Create("testdata/triggers/sample-pipeline.yaml", ns)
oc.Create("testdata/triggers/triggerbindings/triggerbinding.yaml", ns)
oc.Create("testdata/triggers/triggertemplate/triggertemplate.yaml", ns)
oc.Create("testdata/triggers/eventlisteners/eventlistener-embeded-binding.yaml", ns)
```

### Label Mapping

| Gauge Tag | Ginkgo Label |
|-----------|-------------|
| e2e | Label("e2e") |
| triggers | Label("triggers") |
| sanity | Label("sanity") |
| tls | Label("tls") |
| admin | Label("admin") |
| non-admin | Label("non-admin") |
| to-do | Pending("to-do") |
| bug-to-fix | Pending("bug-to-fix") |
| skip-4.14 | Label("skip-4.14") |
| tutorial | Label("tutorial") |
| integration | Label("integration") |

### Cleanup Mapping

Gauge's explicit `Cleanup Triggers` step becomes `DeferCleanup` registered immediately after resource creation:

```go
It("PIPELINES-05-TC08: Create Eventlistener embedded TriggersBindings specs", Label("e2e", "triggers", "non-admin", "sanity"), func() {
    ns := ... // create namespace
    DeferCleanup(func() {
        triggers.CleanupTriggers(sharedClients, "listener-embed-binding", ns)
    })
    // ... test body ...
})
```

## Test Grouping for Ginkgo

Natural grouping by Describe/Context:

```
Describe("Triggers")
  Context("EventListeners")
    Context("Basic (to-do)")       -- TC01-TC06 (Pending)
    Context("TLS")                 -- TC07, TC16
    Context("Embedded Bindings")   -- TC08, TC09
    Context("Interceptors")        -- TC10 (gitlab), TC11 (bitbucket)
    Context("ClusterTriggerBinding") -- TC12, TC13, TC14
    Context("TriggersCRD")         -- TC15
    Context("Kubernetes Events")   -- TC17
  Context("TriggerBindings")       -- PIPELINES-10-TC01, TC02, TC03
  Context("CronJob Triggers")     -- PIPELINES-04-TC01
  Context("Tutorial")              -- PIPELINES-06-TC01, TC02
```

## Risks and Mitigations

1. **TLS cert generation requires filesystem access**: The `ExposeEventListenerForTLS()` function creates certs in `testdata/triggers/certs/`. In Ginkgo, ensure temp cert directory is created per-test and cleaned up. Use `GinkgoT().TempDir()` or `DeferCleanup` for cert file cleanup.

2. **HTTP client for TLS needs cert files on disk**: `CreateHTTPSClient()` reads cert files from disk. The certs must exist before the HTTP client is created. This is sequential within a test -- no issue.

3. **Time.Sleep in route exposure**: Both `ExposeEventListner()` and `GetRouteURL()` use `time.Sleep(5 * time.Second)`. These should ideally be replaced with `Eventually()` polling for route readiness, but for initial migration, keeping the sleep is acceptable.

4. **Tutorial tests use remote URLs**: `PIPELINES-06-TC01/TC02` use `oc.CreateRemote()` to fetch YAML from GitHub. These require network access and a valid `OSP_TUTORIAL_BRANCH` environment variable.

5. **Shared pipeline verification functions**: If `pkg/pipelines/` helpers are not yet ported when Phase 5 executes, they must be created. Since Phase 5 depends on Phase 3 (which migrates sanity tests), basic pipeline verification likely exists by then.

## Sources

- `reference-repo/release-tests/specs/triggers/*.spec` -- All 4 trigger spec files
- `reference-repo/release-tests/specs/concepts/triggers.cpt` -- Gauge concept definitions
- `reference-repo/release-tests/steps/triggers/triggers.go` -- Gauge step implementations
- `reference-repo/release-tests/pkg/triggers/triggers.go` -- Core trigger helper functions
- `reference-repo/release-tests/pkg/triggers/helper.go` -- HTTP client and header helpers
- `reference-repo/release-tests/steps/pipeline/pipeline.go` -- Pipeline/TaskRun verification steps
- `reference-repo/release-tests/steps/cli/oc.go` -- OC CLI steps (Create, Enable TLS, etc.)
- `tests/triggers/suite_test.go` -- Existing Ginkgo suite entry point
- `pkg/oc/oc.go` -- Already-ported OC helpers
