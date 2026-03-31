# Testing Patterns

**Analysis Date:** 2026-03-31

## Test Framework

**Runner:**
- Ginkgo v2.13.0
- Config: No explicit config file detected (uses defaults)

**Assertion Library:**
- Gomega v1.29.0
- Additional assertions: `gotest.tools/v3` (for command testing)

**Run Commands:**
```bash
go test ./...                    # Run all tests
go test -v ./tests/...          # Run tests with verbose output
ginkgo -r                        # Run with Ginkgo CLI (recursive)
ginkgo watch -r                  # Watch mode
ginkgo -cover -r                 # With coverage
```

## Test File Organization

**Location:**
- Tests in separate `tests/` directory (not co-located with source)
- Test structure: `tests/{feature}/{feature}_test.go`
- Example: `tests/pac/pac_test.go` for Pipelines-as-Code tests

**Naming:**
- Test files use `_test.go` suffix
- Package names use `{package}_test` suffix: `package pac_test`
- External test packages (black-box testing)

**Structure:**
```
tests/
├── pac/
│   └── pac_test.go
└── (additional test directories for other features)
```

## Test Structure

**Suite Organization:**
```go
package pac_test

import (
	. "github.com/onsi/ginkgo/v2"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/pac"
)

var _ = Describe("Pipelines As Code tests", func() {

	Describe("Configure PAC in GitLab Project: PIPELINES-30-TC01", func() {
		It("Setup Gitlab Client", func() {
			c := pac.InitGitLabClient()
			pac.SetGitLabClient(c)
		})
		
		It("Validate PipelineRun for success", func() {
			// Test implementation
		})
	})
})
```

**Patterns:**
- Top-level `Describe` blocks group related test scenarios
- Nested `Describe` blocks for test case organization (includes test case ID)
- `It` blocks contain individual test steps
- Test case IDs referenced in Describe labels: `PIPELINES-30-TC01`
- Dot imports for Ginkgo DSL: `. "github.com/onsi/ginkgo/v2"`

## Mocking

**Framework:** No explicit mocking framework detected

**Patterns:**
- Integration testing approach (testing against real cluster)
- Test helpers in `pkg/` packages provide test utilities
- State management via `pkg/store` for test data sharing
- Command execution wrappers (`pkg/cmd`) enable controlled command testing

**What to Mock:**
- Not applicable - tests run against real OpenShift/Kubernetes clusters
- Tests use actual `oc`, `opc`, and other CLI tools

**What NOT to Mock:**
- Kubernetes API interactions (tested via real clients)
- CLI command execution (real commands executed)
- External services like GitLab (configured with real credentials)

## Fixtures and Factories

**Test Data:**
- Centralized store pattern via `pkg/store/store.go`
- Scenario-level data: `PutScenarioData(key, value)`, `GetScenarioData(key)`
- Suite-level data: `PutSuiteData(key, value)`, `GetSuiteData(key)`
- Thread-safe access with mutex locks

**Example from `pkg/store/store.go`:**
```go
// Store scenario-specific data
func PutScenarioData(key, value string) {
	mu.Lock()
	defer mu.Unlock()
	scenarioStore[key] = value
}

// Retrieve scenario data
func GetScenarioData(key string) string {
	mu.RLock()
	defer mu.RUnlock()
	if v, ok := scenarioStore[key].(string); ok {
		return v
	}
	return ""
}
```

**Location:**
- Test state: `pkg/store/store.go`
- Configuration: `pkg/config/config.go` (constants and flags)
- No separate fixtures directory

## Coverage

**Requirements:** None enforced

**View Coverage:**
```bash
ginkgo -cover -r
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Test Types

**Unit Tests:**
- Not prominent in current implementation
- Focus on integration testing

**Integration Tests:**
- Primary testing approach
- Tests interact with real OpenShift/Kubernetes clusters
- Test Tekton Pipelines, Triggers, Chains, and PAC components
- Command-line tool integration (oc, opc, tkn)
- External service integration (GitLab via PAC)

**E2E Tests:**
- Current test suite is E2E in nature
- Tests entire workflows: setup → configure → validate → cleanup
- Multi-step test scenarios with shared state
- Example: PAC GitLab integration tests full pipeline lifecycle

## Common Patterns

**Async Testing:**
- Not explicitly visible in current code
- Ginkgo supports async with `Eventually()` and `Consistently()`
- Timeouts configured via `config.APITimeout`, `config.CLITimeout`

**Command Execution:**
```go
// Using cmd package wrappers
result := cmd.MustSucceed("oc", "get", "pods")

// With timeout
result := cmd.MustSucceedIncreasedTimeout(time.Minute*5, "oc", "delete", "project", "test")

// With assertion
cmd.Assert(icmd.Success, "oc", "version")
```

**Error Testing:**
```go
// Using Gomega matchers
result := cmd.Run("oc", "patch", "tektonconfig", "config", "-p", invalidData, "--type=merge")
Expect(result.ExitCode).To(Equal(1))
Expect(result.Stderr()).To(ContainSubstring(errorMessage))
```

**Failure Handling:**
```go
// Using Ginkgo Fail() for immediate failure
if err != nil {
	Fail(fmt.Sprintf("failed to initialize GitLab client: %v", err))
}
```

## Test Helpers

**Command Wrappers (`pkg/cmd/cmd.go`):**
- `Run(cmd ...string)`: Execute command with default timeout
- `MustSucceed(args ...string)`: Assert command succeeds (exit 0)
- `Assert(exp icmd.Expected, args ...string)`: Assert specific exit code
- `MustSucceedIncreasedTimeout(timeout, args)`: Long-running commands

**Kubernetes Helpers (`pkg/clients/clients.go`):**
- `NewClients(configPath, clusterName, namespace)`: Initialize all K8s clients
- `NewKubeClient(configPath, clusterName)`: Initialize base kube client
- Provides typed clients for Tekton, Triggers, PAC, OLM, Routes, etc.

**OpenShift Helpers (`pkg/oc/oc.go`):**
- `Create(path_dir, namespace)`: Create resources from YAML
- `Delete(path_dir, namespace)`: Delete resources
- `CreateNewProject(ns)`: Create namespace/project
- `SecretExists(secretName, namespace)`: Check secret existence
- `EnableConsolePlugin()`: Enable Pipelines console plugin

**State Management (`pkg/store/store.go`):**
- `Clients()`: Get test Kubernetes clients
- `Namespace()`: Get test namespace
- `PutScenarioData(key, value)`: Store test data
- `GetScenarioData(key)`: Retrieve test data

## Test Configuration

**Environment Variables:**
- `KUBECONFIG`: Path to kubeconfig file
- `CHANNEL`: Operator subscription channel
- `CATALOG_SOURCE`: Operator catalog source
- `CSV_VERSION`: Operator version
- `GITLAB_TOKEN`: GitLab authentication token
- `GITLAB_WEBHOOK_TOKEN`: GitLab webhook secret
- Component versions: `PIPELINE_VERSION`, `TRIGGERS_VERSION`, `CHAINS_VERSION`, `PAC_VERSION`, `TKN_CLIENT_VERSION`

**Configuration via `pkg/config/config.go`:**
```go
const (
	APIRetry         = time.Second * 5
	APITimeout       = time.Minute * 10
	CLITimeout       = time.Second * 90
	ResourceTimeout  = 60 * time.Second
	TargetNamespace  = "openshift-pipelines"
)
```

**Flags (parsed but not mandatory):**
- `--cluster`: Kubernetes cluster name
- `--kubeconfig`: Path to kubeconfig
- `--channel`: Operator channel
- `--csv`: CSV name
- `--clusterarch`: Cluster architecture

## Migration Status

**Note:** This repository is migrating from Gauge to Ginkgo framework.

**Current State:**
- Ginkgo v2 test framework in place
- Many test cases commented out (migration in progress)
- Single active test in `tests/pac/pac_test.go`
- Helper packages fully implemented
- Infrastructure for expanded test coverage ready

**Pattern:**
```go
// Active test
It("Setup Gitlab Client", func() {
	c := pac.InitGitLabClient()
	pac.SetGitLabClient(c)
})

// Commented tests (to be migrated)
// It("Validate PAC Info Install", func() {
// 	pac.AssertPACInfoInstall()
// })
```

## Best Practices Observed

**1. Separation of Concerns:**
- Test code in `tests/`
- Helper utilities in `pkg/`
- Clear package boundaries

**2. Descriptive Test Names:**
- Include test case IDs: `PIPELINES-30-TC01`
- Action-oriented: "Setup Gitlab Client", "Validate PipelineRun"

**3. Shared State Management:**
- Centralized via `pkg/store`
- Thread-safe with mutexes
- Type-safe getters/setters

**4. Command Execution Safety:**
- Wrappers validate exit codes
- Timeout protection on long operations
- Explicit error handling

**5. Logging:**
- Verbose command output logging
- Helpful for debugging test failures
- Consistent format across helpers

---

*Testing analysis: 2026-03-31*
