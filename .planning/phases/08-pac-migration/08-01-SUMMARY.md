---
phase: 08-pac-migration
plan: 01
subsystem: testing
tags: [pac, gitlab, ginkgo, pipelines-as-code, webhook, smee]

requires:
  - phase: 01-foundation-repair
    provides: "Fixed store package, go.mod setup"
  - phase: 02-suite-scaffolding
    provides: "suite_test.go for PAC with sharedClients pattern"
provides:
  - "Complete pkg/pac/pac.go with 20+ helper functions for GitLab PAC workflows"
  - "GetLatestPipelinerun in pkg/pipelines for PipelineRun retrieval"
  - "ValidateDeployments and DeleteDeployment in pkg/k8s for deployment management"
  - "AssertNumberOfPipelineruns for count-based PipelineRun verification"
affects: [08-pac-migration-plan-02, tests/pac]

tech-stack:
  added: [pipelines-as-code/generate, pipelines-as-code/cli, pipelines-as-code/git, go-gitlab, gopkg.in/yaml.v2]
  patterns: [explicit-params-over-global-store, error-return-over-fail, ordered-container-safe-globals]

key-files:
  created: []
  modified:
    - pkg/pac/pac.go
    - pkg/pipelines/pipelines.go
    - pkg/k8s/k8s.go
    - go.mod
    - go.sum

key-decisions:
  - "Used explicit function parameters instead of store.GetScenarioData/PutScenarioData for all new PAC functions"
  - "Kept global var client *gitlab.Client since PAC tests run in Ordered containers (thread-safe)"
  - "Used package-level var projectURL instead of store for project URL tracking within PAC operations"
  - "InitGitLabClient accepts namespace parameter instead of reading from store.Namespace()"
  - "GetLatestPipelinerun sorts by CreationTimestamp without tektoncd/cli dependency"

patterns-established:
  - "PAC helper functions accept (*clients.Clients, namespace string) as first params"
  - "Functions that previously called testsuit.T.Fail now return error for caller handling"
  - "Functions that directly fail tests use Ginkgo Fail() with fmt.Sprintf"

requirements-completed: [PAC-01, PAC-02]

duration: 8min
completed: 2026-04-02
---

# Phase 08 Plan 01: Port PAC Helper Functions Summary

**Complete port of 20+ PAC helper functions from Gauge reference to Ginkgo with explicit params replacing global store, plus GetLatestPipelinerun and deployment management helpers**

## Performance

- **Duration:** 8 min
- **Started:** 2026-04-02T11:39:47Z
- **Completed:** 2026-04-02T11:48:30Z
- **Tasks:** 2
- **Files modified:** 4 (pkg/pac/pac.go, pkg/pipelines/pipelines.go, pkg/k8s/k8s.go, go.mod)

## Accomplishments
- Ported all PAC helper functions (SetupSmeeDeployment, SetupGitLabProject, ConfigurePreviewChanges, GeneratePipelineRunYaml, UpdateAnnotation, TriggerPushOnForkMain, AddComment, AddLabel, CleanupPAC, AssertPACInfoInstall, GetPipelineNameFromMR, GetPushPipelineNameFromMain, AssertNumberOfPipelineruns) from reference Gauge repo
- Added GetLatestPipelinerun to pkg/pipelines using creation timestamp sorting
- Added ValidateDeployments, DeleteDeployment, WaitForDeployment, WaitForDeploymentDeletion to pkg/k8s
- Eliminated all testsuit, store.GetScenarioData, store.PutScenarioData, store.Clients, store.Namespace references from ported code

## Task Commits

Each task was committed atomically:

1. **Task 1: Port pkg/pipelines and pkg/k8s helper functions** - `8f1ebaa` (feat)
2. **Task 2: Port full PAC helper implementation to pkg/pac/pac.go** - `8b9163e` (feat)

## Files Created/Modified
- `pkg/pac/pac.go` - Complete PAC helper implementation (840+ lines) for GitLab integration
- `pkg/pipelines/pipelines.go` - Added GetLatestPipelinerun and CheckLogVersion functions
- `pkg/k8s/k8s.go` - Added deployment validation/deletion/wait functions
- `go.mod` / `go.sum` - Updated for PAC generate/cli/git package imports

## Decisions Made
- Used explicit function parameters instead of store.GetScenarioData/PutScenarioData -- all PAC functions accept (*clients.Clients, namespace) and return values instead of writing to global store
- Kept global var client *gitlab.Client since PAC tests run in Ordered containers (safe for sequential access)
- InitGitLabClient now accepts namespace parameter instead of calling store.Namespace()
- GetLatestPipelinerun uses simple CreationTimestamp comparison instead of importing tektoncd/cli's prsort package
- ConfigurePreviewChanges returns (mrID int, err error) instead of storing mrID in scenario data

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- All PAC helper functions are ready to be called from Ginkgo test specs in Plan 08-02
- pkg/pac/pac.go, pkg/pipelines/pipelines.go, and pkg/k8s/k8s.go pass go vet

---
*Phase: 08-pac-migration*
*Completed: 2026-04-02*
