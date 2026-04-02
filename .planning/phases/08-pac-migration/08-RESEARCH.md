# Phase 8 Research: PAC Migration

**Researched:** 2026-03-31
**Confidence:** HIGH

## Test Inventory

PAC tests span 2 Gauge spec files with 7 total scenarios:

### pac-gitlab.spec (PIPELINES-30) -- 3 GitLab-based tests

| Test ID | Name | Tags | Steps | Complexity |
|---------|------|------|-------|------------|
| PIPELINES-30-TC01 | Configure PAC in GitLab Project | pac, sanity, e2e | 10 | HIGH -- push + pull_request events, full lifecycle |
| PIPELINES-30-TC02 | Configure PAC in GitLab Project (on-label) | pac, e2e | 9 | HIGH -- label annotation, MR label management |
| PIPELINES-30-TC03 | Configure PAC in GitLab Project (on-comment) | pac, e2e | 10 | HIGH -- comment annotation, MR comment trigger |

### pac.spec (PIPELINES-20) -- 4 TektonConfig/GitHub-based tests

| Test ID | Name | Tags | Steps | Complexity |
|---------|------|------|-------|------------|
| PIPELINES-20-TC01 | Enable/Disable PAC | pac, sanity, to-do | 8 | MEDIUM -- TektonConfig CR modification |
| PIPELINES-20-TC02 | Enable/Disable PAC (app name change) | pac, sanity, to-do | 9 | HIGH -- GitHub app, pipelinerun verification |
| PIPELINES-20-TC03 | Enable/Disable auto-configure-new-github-repo | pac, sanity, to-do | 7 | MEDIUM -- TektonConfig setting, repo CR verification |
| PIPELINES-20-TC04 | Enable/Disable error-log-snippet | pac, sanity, to-do | 11 | HIGH -- GitHub app, error log verification |

**Key observation:** PIPELINES-20-TC02 through TC04 are tagged `to-do` -- these are defined but not yet fully implemented in the Gauge suite. PIPELINES-20-TC01 is also `to-do` but has a simpler TektonConfig-based workflow that can be implemented.

## Source Analysis

### Existing Ginkgo Code (current repo)

**`tests/pac/suite_test.go`** -- Complete and correct. Has BeforeSuite with client initialization, AfterSuite with cleanup, Label("pac").

**`tests/pac/pac_test.go`** -- Minimal scaffolding. Only one test (TC01) partially started. All steps except `InitGitLabClient`/`SetGitLabClient` are commented out. Uses `store.Namespace()` and `store.Clients()` in comments but the Describe is not marked `Ordered`.

**`pkg/pac/pac.go`** (Ginkgo repo) -- Only has `InitGitLabClient()` and `SetGitLabClient()`. Uses Ginkgo `Fail()` instead of Gauge `testsuit.T.Fail()`. Still uses global `var client *gitlab.Client` and `pkg/store` for namespace.

### Reference Gauge Code (reference-repo)

**`reference-repo/release-tests/pkg/pac/pac.go`** -- Full implementation with 822 lines covering:
- GitLab client initialization (`InitGitLabClient`)
- Smee deployment creation (`SetupSmeeDeployment`, `createSmeeDeployment`)
- GitLab project forking (`forkProject`, `SetupGitLabProject`)
- Webhook management (`addWebhook`)
- PAC Repository CR creation (`createNewRepository`)
- PipelineRun YAML generation (`GeneratePipelineRunYaml`, `generatePipelineRun`)
- Annotation updates (`UpdateAnnotation`)
- Branch/commit/MR management (`createBranch`, `createCommit`, `ConfigurePreviewChanges`)
- Push event triggering (`TriggerPushOnForkMain`)
- Pipeline status checking (`checkPipelineStatus`, `GetPipelineName`, `GetPipelineNameFromMR`)
- Label/comment management (`AddLabel`, `AddComment`)
- Cleanup (`CleanupPAC`, `deleteGitlabProject`)
- PAC info validation (`AssertPACInfoInstall`)

**`reference-repo/release-tests/steps/pac/pac.go`** -- Gauge step bindings that map spec steps to pkg/pac functions.

### Critical Dependencies

| Dependency | Current State | Migration Impact |
|-----------|--------------|-----------------|
| `pkg/store` (global state) | Used for Namespace(), Clients(), ScenarioData | Must refactor to closure-scoped or pass explicitly |
| `pkg/oc` (secret management) | SecretExists, CreateSecretForWebhook exist in Ginkgo repo | Ready to use |
| `pkg/pipelines` | ValidatePipelineRun, GetLatestPipelinerun in reference only | Must port to Ginkgo repo |
| `pkg/k8s` | ValidateDeployments, DeleteDeployment in reference only | Must port to Ginkgo repo |
| `pkg/opc` | GetOpcPacInfoInstall in reference only | Must port to Ginkgo repo |
| `github.com/xanzy/go-gitlab` | Deprecated but functional | Use as-is per roadmap (DEP-02 deferred) |
| PAC API types | `pacv1alpha1` from pipelines-as-code | Already in go.mod |
| PAC generate | `pacgenerate` from pipelines-as-code | Already in go.mod |

## Architecture Decisions

### 1. Ordered Containers are Mandatory

Each PAC GitLab test (TC01-TC03) follows a strict sequential workflow:
1. Init GitLab client
2. Create Smee deployment
3. Setup GitLab project (fork + webhook + Repository CR)
4. Generate PipelineRun YAML
5. Configure preview changes (branch + commit + MR)
6. Validate PipelineRun
7. Cleanup

Steps depend on prior step state (project ID, MR ID, smee URL). Ginkgo `Ordered` container is the correct pattern.

### 2. State Passing Within Ordered Containers

The Gauge implementation uses `store.PutScenarioData`/`store.GetScenarioData` for inter-step state (projectID, mrID, SMEE_URL, etc.). In Ginkgo Ordered containers, this maps to:
- Declare variables at the `Describe` (container) level
- Initialize in sequential `It` blocks
- Variables are shared across all `It` blocks in the Ordered container

This is explicitly allowed by Ginkgo for Ordered containers (unlike regular containers where it causes spec pollution).

### 3. DeferCleanup for GitLab Resources

GitLab webhook and forked project must be cleaned up even on test failure. Register DeferCleanup in the setup step that creates the resource:
- After fork: DeferCleanup(deleteGitlabProject, projectID)
- After smee deployment: DeferCleanup(deleteSmeeDeployment, ...)

### 4. pkg/pac Refactoring Strategy

The current `pkg/pac/pac.go` in the Ginkgo repo is minimal. The full implementation from the reference repo must be ported. Key changes:
- Replace `testsuit.T.Fail(fmt.Errorf(...))` with `Fail(fmt.Sprintf(...))` or return errors
- Replace `store.PutScenarioData`/`store.GetScenarioData` with return values and function parameters
- Replace `store.Clients()` and `store.Namespace()` with explicit parameters
- Keep `var client *gitlab.Client` as package-level for now (within Ordered container it's safe since PAC tests run serially)

### 5. Helper Functions Needed

Functions from reference repo that must be ported to the Ginkgo `pkg/` packages:

**Must port to `pkg/pac/pac.go`:**
- `SetupSmeeDeployment` / `createSmeeDeployment`
- `forkProject` / `SetupGitLabProject`
- `addWebhook`
- `createNewRepository`
- `GeneratePipelineRunYaml` / `generatePipelineRun` / `createPacGenerateOpts`
- `UpdateAnnotation`
- `ConfigurePreviewChanges` / `createBranch` / `createCommit` / `createMergeRequest`
- `TriggerPushOnForkMain`
- `checkPipelineStatus` / `GetPipelineName` / `GetPipelineNameFromMR` / `GetPushPipelineNameFromMain`
- `AddLabel` / `AddComment` / `addLabelToProject`
- `CleanupPAC` / `deleteGitlabProject`
- `AssertPACInfoInstall`
- `validateYAML` / `repoFileExists` / `extractMergeRequestID` / `isTerminalStatus`

**Must port to `pkg/pipelines/`:**
- `ValidatePipelineRun`
- `GetLatestPipelinerun`

**Must port to `pkg/k8s/`:**
- `ValidateDeployments`
- `DeleteDeployment`

### 6. PIPELINES-20 Tests (to-do tagged)

PIPELINES-20-TC01 (Enable/Disable PAC) modifies TektonConfig and is an operator-level test. It should use `Serial` + `Ordered` decorators. TC02-TC04 require GitHub App integration which is not implemented. These should be migrated as `PIt` (pending) specs with the test structure defined but implementation deferred.

## Test Flow Mapping

### PIPELINES-30-TC01: Push + Pull Request Events
```
Ordered, Label("pac", "sanity", "e2e")
  BeforeAll: InitGitLabClient, SetupSmeeDeployment, SetupGitLabProject
  It: Generate pull_request YAML
  It: Generate push YAML
  It: ConfigurePreviewChanges (creates branch, commits both files, creates MR)
  It: Validate pull_request PipelineRun success
  It: TriggerPushOnForkMain
  It: Validate push PipelineRun success
  AfterAll / DeferCleanup: CleanupPAC
```

### PIPELINES-30-TC02: On-Label Annotation
```
Ordered, Label("pac", "e2e")
  BeforeAll: InitGitLabClient, SetupSmeeDeployment, SetupGitLabProject
  It: Generate pull_request YAML
  It: UpdateAnnotation on-label [bug]
  It: ConfigurePreviewChanges
  It: Assert 0 pipelineruns within 10s
  It: AddLabel "bug" to MR
  It: Validate pull_request PipelineRun success
  AfterAll / DeferCleanup: CleanupPAC
```

### PIPELINES-30-TC03: On-Comment Annotation
```
Ordered, Label("pac", "e2e")
  BeforeAll: InitGitLabClient, SetupSmeeDeployment, SetupGitLabProject
  It: Generate pull_request YAML
  It: UpdateAnnotation on-comment ^/hello-world
  It: ConfigurePreviewChanges
  It: Validate first pull_request PipelineRun success
  It: AddComment /hello-world to MR
  It: Assert 2 pipelineruns within 10s
  It: Validate second pull_request PipelineRun success
  AfterAll / DeferCleanup: CleanupPAC
```

### PIPELINES-20-TC01: Enable/Disable PAC
```
Ordered, Serial, Label("pac", "sanity")
  It: Set pipelinesAsCode.enable = false
  It: Verify PAC installersets not present
  It: Verify PAC pods not present
  It: Verify pipelines-as-code CR removed
  It: Set pipelinesAsCode.enable = true
  It: Verify PAC installersets present
  It: Verify PAC pods present
  It: Verify pipelines-as-code CR removed (still removed even when enabled)
```

### PIPELINES-20-TC02/TC03/TC04: GitHub App Tests (Pending)
These require GitHub App configuration not available in the current test infrastructure. Migrate as `PIt` (Pending) specs.

## Risk Assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| Global `var client *gitlab.Client` in pkg/pac | Thread-safety if run in parallel | PAC tests are Ordered+Serial; safe for now. Mark with comment for future refactor |
| Smee.io availability | External dependency for webhook forwarding | Tests already tagged with pac label; can be filtered out if smee unavailable |
| GitLab API rate limits | Fork/webhook operations may hit limits | Existing exponential backoff in forkProject handles this |
| Missing pkg/pipelines and pkg/k8s helpers | Tests depend on ValidatePipelineRun, GetLatestPipelinerun, etc. | Must port these functions before PAC tests can run |
| PIPELINES-20 GitHub tests not implemented | 3 of 7 tests can't be verified | Migrate as Pending specs; count them but mark clearly |

## Plan Structure

**Plan 08-01: Port PAC helper functions and refactor pkg/pac**
- Port all functions from reference `pkg/pac/pac.go` to Ginkgo `pkg/pac/pac.go`
- Port required helper functions to `pkg/pipelines/` and `pkg/k8s/`
- Replace Gauge patterns (testsuit.T.Fail, store.*) with Ginkgo patterns (Fail, parameters)
- Wave 1 (no dependencies within this phase)

**Plan 08-02: Migrate all 7 PAC test specs**
- Migrate PIPELINES-30-TC01, TC02, TC03 as Ordered containers with full implementation
- Migrate PIPELINES-20-TC01 as Ordered+Serial container
- Migrate PIPELINES-20-TC02, TC03, TC04 as Pending specs
- Wire to pkg/pac functions from Plan 01
- Wave 2 (depends on 08-01)

---
*Research completed: 2026-03-31*
