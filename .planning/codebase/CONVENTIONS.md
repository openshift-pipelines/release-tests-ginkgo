# Coding Conventions

**Analysis Date:** 2026-03-31

## Naming Patterns

**Files:**
- Package files use lowercase, single-word names: `pac.go`, `oc.go`, `cmd.go`, `store.go`, `config.go`
- Test files use `_test.go` suffix: `pac_test.go`
- No file name separators (no hyphens or underscores in package files)

**Functions:**
- Exported (public) functions use PascalCase: `MustSucceed`, `InitGitLabClient`, `NewClients`, `GetScenarioData`
- Unexported (private) functions use camelCase: `newTektonOperatorAlphaClients`, `resourceExists`, `initializeFlags`
- Test-related functions often start with verbs: `Assert`, `Validate`, `Verify`
- Command wrappers start with action verbs: `Run`, `Create`, `Delete`, `Apply`
- Assertion functions start with `Assert` or `Must`: `AssertComponentVersion`, `MustSucceed`

**Variables:**
- Package-level variables use camelCase: `scenarioStore`, `suiteStore`, `client`
- Constants use PascalCase: `APIRetry`, `CLITimeout`, `TargetNamespace`, `PipelineControllerName`
- Single-letter variables for short scopes: `c` for client, `w` for writer, `f` for flags
- Descriptive names for longer scopes: `commandResult`, `expectedVersion`, `pipelineRunName`

**Types:**
- Structs use PascalCase: `Clients`, `KubeClient`, `EnvironmentFlags`, `PipelineRunList`
- Field names in structs use PascalCase: `KubeClient`, `Dynamic`, `Operator`, `InstallVersion`

## Code Style

**Formatting:**
- Standard Go formatting (likely using `go fmt` or `gofmt`)
- No explicit linter configuration files detected (.golangci.yml, .editorconfig not present)
- Tabs for indentation (Go standard)
- Opening braces on same line as declaration

**Linting:**
- No explicit linter configuration detected
- Code follows standard Go conventions
- Clean code with no TODO/FIXME/HACK/XXX comments found

## Import Organization

**Order:**
1. Standard library imports (grouped together)
2. Third-party imports (grouped together)
3. Local package imports (grouped together)

**Example from `pkg/pac/pac.go`:**
```go
import (
	"fmt"
	"log"
	"os"

	"github.com/srivickynesh/release-tests-ginkgo/pkg/oc"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/store"
	"github.com/xanzy/go-gitlab"

	. "github.com/onsi/ginkgo/v2"
)
```

**Path Aliases:**
- Dot imports used for Ginkgo/Gomega test matchers: `. "github.com/onsi/ginkgo/v2"`, `. "github.com/onsi/gomega"`
- No custom path aliases in `go.mod`
- Full package paths used in non-test code

## Error Handling

**Patterns:**
- Standard Go error handling with `if err != nil` checks
- Errors wrapped with `fmt.Errorf` and `%w` verb for error chains
- Test code uses `Fail()` from Ginkgo for fatal errors
- Command execution uses custom assertion wrappers: `MustSucceed`, `Assert`

**Examples:**
```go
// Standard error return pattern
func NewClients(configPath string, clusterName, namespace string) (*Clients, error) {
	clients.KubeClient, clients.KubeConfig, err = NewKubeClient(configPath, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubeclient from config file at %s: %s", configPath, err)
	}
	// ...
}

// Test assertion pattern with Fail()
if err != nil {
	Fail(fmt.Sprintf("failed to initialize GitLab client: %v", err))
}

// Ignoring errors explicitly (when intentional)
func DeleteProjectIgnoreErors(ns string) {
	log.Printf("output: %s\n", cmd.Run("oc", "delete", "project", ns).Stdout())
}
```

## Logging

**Framework:** Standard library `log` package

**Patterns:**
- Verbose logging with `log.Printf()` for command output
- Structured format: `log.Printf("output: %s\n", result)`
- Informational messages: `log.Printf("Pipelinerun %s started", pipelineRunName)`
- Status messages: `log.Printf("Secret \"%s\" already exists", webhookConfigName)`
- No debug/trace levels (simple logging only)

**Examples from `pkg/oc/oc.go`:**
```go
func Create(path_dir, namespace string) {
	log.Printf("output: %s\n", cmd.MustSucceed("oc", "create", "-f", config.Path(path_dir), "-n", namespace).Stdout())
}
```

## Comments

**When to Comment:**
- Package-level documentation for public functions
- Inline comments for non-obvious logic
- Section separators in test files using `//` comments
- Commented-out test cases (migration in progress)

**JSDoc/TSDoc:**
- Not applicable (Go codebase)
- Go doc comments used for exported functions
- Comments appear directly above function declarations

**Examples:**
```go
// Clients holds instances of interfaces for making requests to Tekton Pipelines.
type Clients struct {
	KubeClient *KubeClient
	// ...
}

// Initialize Gitlab Client
func InitGitLabClient() *gitlab.Client {
	// ...
}
```

## Function Design

**Size:** 
- Helper functions are concise (10-30 lines): `MustSucceed`, `Create`, `Apply`
- Complex functions can be longer (50-100 lines): `NewClients`, `InitializeFlags`
- Utility functions favor clarity over brevity

**Parameters:**
- Positional parameters for required values
- Variadic parameters (`...string`) for flexible command arguments
- Named return values occasionally used for clarity

**Return Values:**
- Single return value for simple operations: `string`, `bool`
- Tuple returns for operations that can fail: `(*Clients, error)`, `(string, error)`
- Pointers returned for struct types to avoid copies
- Error as last return value (Go convention)

**Examples:**
```go
// Simple return
func Namespace() string {
	// ...
}

// Error tuple return
func NewClients(configPath string, clusterName, namespace string) (*Clients, error) {
	// ...
}

// Variadic parameters
func Run(cmd ...string) *icmd.Result {
	return icmd.RunCmd(icmd.Cmd{Command: cmd, Timeout: config.CLITimeout})
}
```

## Module Design

**Exports:**
- Only capitalized identifiers are exported (Go standard)
- Package-level variables exported when needed by other packages
- Helper types exported to enable usage: `Clients`, `KubeClient`, `Cmd`

**Barrel Files:**
- Not applicable (Go doesn't use barrel file pattern)
- Each package has its own named file: `pkg/cmd/cmd.go`, `pkg/oc/oc.go`
- Single file per package for most packages

## Package Organization

**Pattern:**
- Flat package structure under `pkg/`
- Each package represents a domain: `cmd`, `oc`, `opc`, `clients`, `config`, `store`, `pac`
- Test code in separate `tests/` directory
- No subpackages or nested package hierarchies

**Package Responsibilities:**
- `pkg/cmd`: Command execution wrappers
- `pkg/oc`: OpenShift CLI operations
- `pkg/opc`: OpenShift Pipelines CLI operations
- `pkg/clients`: Kubernetes client initialization
- `pkg/config`: Configuration and constants
- `pkg/store`: Test state management
- `pkg/pac`: Pipelines-as-Code specific helpers

## Synchronization

**Patterns:**
- Mutex usage for thread-safe access: `sync.RWMutex` in `pkg/store/store.go`
- Read locks (`RLock`) for read operations
- Write locks (`Lock`) for write operations
- Consistent lock/unlock with defer: `defer mu.RUnlock()`

**Example from `pkg/store/store.go`:**
```go
func Namespace() string {
	mu.RLock()
	defer mu.RUnlock()
	if v, ok := scenarioStore["namespace"].(string); ok {
		return v
	}
	return ""
}
```

---

*Convention analysis: 2026-03-31*
