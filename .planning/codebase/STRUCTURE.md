# Codebase Structure

**Analysis Date:** 2026-03-31

## Directory Layout

```
release-tests-ginkgo/
├── pkg/                 # Shared test utilities and helpers
│   ├── clients/         # Kubernetes and Tekton client wrappers
│   ├── cmd/             # CLI command execution utilities
│   ├── config/          # Configuration constants and environment flags
│   ├── oc/              # OpenShift CLI (oc) helper functions
│   ├── opc/             # OpenShift Pipelines CLI (opc) helper functions
│   ├── pac/             # Pipelines as Code helper functions
│   └── store/           # Thread-safe test state management
├── tests/               # Ginkgo test suites
│   └── pac/             # Pipelines as Code test scenarios
├── .planning/           # GSD planning documents
│   └── codebase/        # Codebase analysis documents
├── go.mod               # Go module definition
├── go.sum               # Go dependency checksums
├── LICENSE              # Apache 2.0 license
└── README.md            # Project overview
```

## Directory Purposes

**pkg/clients:**
- Purpose: Kubernetes and Tekton API client initialization and management
- Contains: Client structs, factory functions, typed client accessors
- Key files: `clients.go` - defines `Clients` and `KubeClient` structs, `NewClients()` factory

**pkg/cmd:**
- Purpose: Standardized CLI command execution with timeout and assertion support
- Contains: Command runners, assertion helpers
- Key files: `cmd.go` - `Run()`, `MustSucceed()`, `Assert()` functions

**pkg/config:**
- Purpose: Global configuration, constants, and environment variable parsing
- Contains: Timeout constants, namespace names, deployment names, flag parsing
- Key files: `config.go` - defines constants (e.g., `TargetNamespace`, `APITimeout`) and `EnvironmentFlags`

**pkg/oc:**
- Purpose: OpenShift CLI (oc) command wrappers for resource management
- Contains: Project creation, resource CRUD operations, secret management, namespace operations
- Key files: `oc.go` - functions like `CreateNewProject()`, `Create()`, `Delete()`

**pkg/opc:**
- Purpose: OpenShift Pipelines CLI (opc) command wrappers and validation
- Contains: Version checking, pipeline operations, resource listing, CLI download utilities
- Key files: `opc.go` - `Cmd` type, `AssertComponentVersion()`, `StartPipeline()`, `GetOpcPrList()`

**pkg/pac:**
- Purpose: Pipelines as Code specific helper functions
- Contains: GitLab client initialization, webhook configuration
- Key files: `pac.go` - `InitGitLabClient()`, `SetGitLabClient()`

**pkg/store:**
- Purpose: Thread-safe storage for test scenario and suite data
- Contains: Concurrent maps, typed accessor functions
- Key files: `store.go` - `Clients()`, `Namespace()`, `PutScenarioData()`, `GetScenarioData()`

**tests/pac:**
- Purpose: Ginkgo test specifications for Pipelines as Code functionality
- Contains: BDD-style test suites using Ginkgo Describe/It blocks
- Key files: `pac_test.go` - test suite for GitLab PAC integration

**.planning/codebase:**
- Purpose: GSD-generated codebase analysis documents
- Contains: Architecture, structure, conventions, testing, stack, integrations documents
- Generated: Yes
- Committed: Yes (used by GSD commands)

## Key File Locations

**Entry Points:**
- `tests/pac/pac_test.go`: Ginkgo test suite for Pipelines as Code

**Configuration:**
- `go.mod`: Go module dependencies and version
- `pkg/config/config.go`: Global constants and environment flags

**Core Logic:**
- `pkg/clients/clients.go`: Client initialization and management
- `pkg/cmd/cmd.go`: CLI command execution engine
- `pkg/store/store.go`: State management

**Testing:**
- `tests/pac/pac_test.go`: PAC integration tests

## Naming Conventions

**Files:**
- Package name matches directory name: `pkg/clients/clients.go`
- Test files: `*_test.go` suffix
- Single file per package for smaller utilities

**Directories:**
- Lowercase, singular or abbreviated names: `pkg`, `oc`, `opc`, `pac`
- Tests organized by component: `tests/pac/`

**Functions:**
- Exported functions: PascalCase - `NewClients()`, `MustSucceed()`, `InitGitLabClient()`
- Internal functions: camelCase - `newTektonOperatorAlphaClients()`, `resourceExists()`
- Constructor pattern: `New*()` - `NewClients()`, `NewKubeClient()`, `New()` (for opc.Cmd)

**Types:**
- Exported structs: PascalCase - `Clients`, `KubeClient`, `Cmd`, `EnvironmentFlags`
- Struct fields: PascalCase for exported - `KubeClient`, `Ctx`, `Dynamic`

## Where to Add New Code

**New Test Suite:**
- Primary code: `tests/<component>/<component>_test.go`
- Tests: Same file using Ginkgo Describe/It blocks
- Example: Add `tests/triggers/triggers_test.go` for Tekton Triggers tests

**New Helper Package:**
- Implementation: `pkg/<name>/<name>.go`
- Pattern: Follow existing helpers (oc, opc, pac) - export functions that use cmd package for execution
- Example: Add `pkg/hub/hub.go` for Tekton Hub operations

**New CLI Wrapper:**
- Implementation: `pkg/cmd/` or new package under `pkg/`
- Follow pattern: Create struct with `Path` field, add `MustSucceed()` and `Assert()` methods
- Example: Add `pkg/tkn/tkn.go` for tkn CLI wrapper

**New Client Type:**
- Implementation: Add to `pkg/clients/clients.go`
- Update: Add field to `Clients` struct, initialize in `NewClients()` or `NewClientSet()`
- Example: Add `ResultsClient` field for Tekton Results API

**Configuration Constants:**
- Implementation: `pkg/config/config.go`
- Add to appropriate section: timeouts, deployment names, namespace constants
- Example: Add `ResultsControllerName = "tekton-results-controller"`

**Test State Storage:**
- Implementation: `pkg/store/store.go`
- Pattern: Add typed accessor function (e.g., `GitLabClient()`) and corresponding `Put*()` function
- Example: Add `GitLabProject()` accessor for storing GitLab project references

## Special Directories

**.planning/codebase:**
- Purpose: GSD-generated codebase analysis documents for AI-assisted development
- Generated: Yes (by GSD mapper)
- Committed: Yes (consumed by GSD planner/executor)

**pkg:**
- Purpose: Reusable test utilities and helpers (not tests themselves)
- Generated: No
- Committed: Yes

**tests:**
- Purpose: Ginkgo test specifications organized by component
- Generated: No (manually written)
- Committed: Yes

---

*Structure analysis: 2026-03-31*
