# Architecture

**Analysis Date:** 2026-03-31

## Pattern Overview

**Overall:** Test Library Architecture with Ginkgo BDD Framework

**Key Characteristics:**
- Helper-driven test organization with shared utilities
- Kubernetes client abstraction layer for OpenShift Pipelines resources
- Thread-safe state management for test scenarios
- CLI command execution wrappers with timeout and assertion handling

## Layers

**Test Layer:**
- Purpose: Contains Ginkgo BDD test specifications
- Location: `tests/`
- Contains: Test suites organized by component (e.g., PAC tests)
- Depends on: pkg/* utilities, Ginkgo/Gomega frameworks
- Used by: Ginkgo test runner

**Client Abstraction Layer:**
- Purpose: Wraps Kubernetes and Tekton clientsets for test interactions
- Location: `pkg/clients/`
- Contains: Client initialization, Kubernetes config loading, typed clients for Tekton resources
- Depends on: k8s.io/client-go, Tekton operator/pipeline clientsets, OpenShift client-go
- Used by: Test layer, helper packages (oc, opc, pac)

**Command Execution Layer:**
- Purpose: Execute and validate CLI commands with standardized error handling
- Location: `pkg/cmd/`
- Contains: Command runners with timeout support, assertion helpers
- Depends on: gotest.tools/v3/icmd, Gomega assertions, pkg/config
- Used by: oc, opc, pac helper packages

**Helper Utilities Layer:**
- Purpose: Domain-specific operations for OpenShift, Tekton, and PAC
- Location: `pkg/oc/`, `pkg/opc/`, `pkg/pac/`
- Contains: Resource management, CLI wrappers, validation helpers
- Depends on: pkg/cmd, pkg/store, pkg/config, pkg/clients
- Used by: Test layer

**State Management Layer:**
- Purpose: Thread-safe storage for test scenario and suite data
- Location: `pkg/store/`
- Contains: Concurrent-safe maps for test state, accessor functions
- Depends on: pkg/clients, pkg/opc, Tekton operator utils
- Used by: Test layer, helper utilities

**Configuration Layer:**
- Purpose: Global constants, environment flags, and path management
- Location: `pkg/config/`
- Contains: Timeouts, namespace constants, deployment names, installer set prefixes, environment flag parsing
- Depends on: Standard library only
- Used by: All other layers

## Data Flow

**Test Execution Flow:**

1. Ginkgo test suite (`tests/pac/pac_test.go`) imports test helpers
2. Test calls helper function (e.g., `pac.InitGitLabClient()`)
3. Helper retrieves configuration from `store` or environment via `config`
4. Helper executes commands via `cmd` package or Kubernetes operations via `clients`
5. Results stored in `store` for subsequent test steps
6. Assertions made using Gomega matchers

**State Management:**
- Test data flows into `store.PutScenarioData()` and `store.PutSuiteData()`
- Concurrent access protected by sync.RWMutex
- State retrieved via typed accessors (e.g., `store.Clients()`, `store.Namespace()`)

## Key Abstractions

**Clients:**
- Purpose: Unified access to all Kubernetes and Tekton API clients
- Examples: `pkg/clients/clients.go`
- Pattern: Builder pattern - `NewClients()` creates fully initialized client set with all typed interfaces

**Cmd:**
- Purpose: Standardized CLI command execution with timeout and assertion
- Examples: `pkg/cmd/cmd.go`, `pkg/opc/opc.go`
- Pattern: Wrapper pattern - `Run()`, `MustSucceed()`, `Assert()` methods wrap icmd with defaults

**Store:**
- Purpose: Type-safe, concurrent storage for test scenario state
- Examples: `pkg/store/store.go`
- Pattern: Repository pattern - Typed accessors hide internal map storage

**Helper Modules:**
- Purpose: Domain-specific operations for different CLIs (oc, opc, tkn, pac)
- Examples: `pkg/oc/oc.go` (OpenShift operations), `pkg/opc/opc.go` (Tekton CLI operations), `pkg/pac/pac.go` (Pipelines as Code operations)
- Pattern: Functional API with side effects (logging, assertions)

## Entry Points

**Ginkgo Test Suites:**
- Location: `tests/pac/pac_test.go`
- Triggers: `ginkgo` test runner or `go test`
- Responsibilities: Define test specifications using BDD syntax, orchestrate test setup/teardown, call helper utilities

**Client Initialization:**
- Location: `pkg/clients/clients.go` - `NewClients()`
- Triggers: Called by test setup or helper initialization
- Responsibilities: Load kubeconfig, create all Kubernetes/Tekton clientsets, configure QPS/Burst limits

## Error Handling

**Strategy:** Fail-fast with detailed error messages

**Patterns:**
- CLI failures: `cmd.MustSucceed()` uses Gomega `Expect()` to fail tests immediately with stdout/stderr
- Client creation errors: Return `fmt.Errorf()` with wrapped context
- Resource validation: Use Gomega matchers (e.g., `Expect(result).To(BeTrue())`) for test assertions
- Timeout handling: Commands use configurable timeouts (`config.CLITimeout`, custom timeouts for long operations)

## Cross-Cutting Concerns

**Logging:** Standard library `log.Printf()` for operation tracking, Ginkgo reporter for test output

**Validation:** Gomega assertions throughout (e.g., `Expect().To()`, `ContainSubstring()`, `BeTrue()`)

**Authentication:** Kubeconfig-based authentication for Kubernetes clients, environment variables for external services (GitLab tokens)

---

*Architecture analysis: 2026-03-31*
