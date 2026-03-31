# Phase 1: Foundation Repair - Research

**Researched:** 2026-03-31
**Domain:** Go module hygiene, import resolution, toolchain upgrades for Gauge-to-Ginkgo migration
**Confidence:** HIGH

## Summary

Phase 1 fixes the foundational issues that block all subsequent migration work. The codebase currently compiles but is semantically broken: `pkg/oc/oc.go` imports `cmd`, `config`, and `store` from the original Gauge repo (`github.com/openshift-pipelines/release-tests`), meaning functions like `oc.DeleteResource` resolve namespaces via Gauge's `gauge.GetScenarioStore()` -- a call that will never work in a Ginkgo context. The module path is declared as `github.com/srivickynesh/release-tests-ginkgo` but the repo lives at `github.com/openshift-pipelines/release-tests-ginkgo`. Go 1.23 is declared in go.mod but is out of support, and Ginkgo v2.13.0 / Gomega v1.29.0 are 13+ minor versions behind current releases.

The fix is surgical: switch 3 imports in one file (`pkg/oc/oc.go`), handle 2 API differences between the Gauge and local packages, rename the module path everywhere, bump Go/Ginkgo/Gomega versions, and run `go mod tidy` to drop Gauge transitive dependencies.

**Primary recommendation:** Fix `pkg/oc/oc.go` imports first (it is the only Go source file importing from the Gauge repo), then rename the module path, then upgrade toolchain versions, then tidy.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| FOUND-01 | Fix dual store import -- `pkg/oc` must reference local `pkg/store`, not the original Gauge repo's store | `pkg/oc/oc.go` imports 3 packages from `github.com/openshift-pipelines/release-tests/pkg/{cmd,config,store}`. Switching to local equivalents requires handling 2 API differences: `config.Path()` returns `(string, error)` locally vs `string` in Gauge, and `cmd.MustSuccedIncreasedTimeout` (typo) in Gauge vs `cmd.MustSucceedIncreasedTimeout` (corrected) locally. Only `oc.DeleteResource()` (line 110) calls `store.Namespace()`. |
| FOUND-02 | Rename module path from `github.com/srivickynesh/release-tests-ginkgo` to `github.com/openshift-pipelines/release-tests-ginkgo` | 6 Go source files + go.mod contain the old path. Full list: `go.mod`, `pkg/cmd/cmd.go`, `pkg/store/store.go`, `pkg/opc/opc.go`, `pkg/pac/pac.go`, `tests/pac/pac_test.go`. Simple find-and-replace. |
| FOUND-03 | Upgrade Go version from 1.23 to 1.24+ | System has Go 1.25.0 installed. go.mod currently declares `go 1.23.4` with `toolchain go1.23.6`. Update to `go 1.24` minimum (or `go 1.25`). |
| FOUND-04 | Upgrade Ginkgo from v2.13.0 to v2.27+ and Gomega from v1.29.0 to v1.38+ | With Go 1.25.0 available, can use latest Ginkgo (v2.28.1+) and Gomega (v1.39.1+). Use `go get` to bump. |
| FOUND-05 | Clone original `openshift-pipelines/release-tests` repo as local reference source | Simple `git clone` operation. Repo URL: `https://github.com/openshift-pipelines/release-tests.git`. Needed for side-by-side comparison during test migration in later phases. |
| FOUND-06 | Remove or replace any transitive Gauge dependencies from go.mod | After switching `pkg/oc/oc.go` imports to local packages, the `github.com/openshift-pipelines/release-tests` direct dependency can be removed. `go mod tidy` will then drop transitive Gauge deps: `github.com/getgauge-contrib/gauge-go v0.4.0`, `github.com/getgauge/common`, and `github.com/dmotylev/goproperties`. |
</phase_requirements>

## Standard Stack

### Core (No new libraries needed)

| Library | Current Version | Target Version | Purpose | Action |
|---------|----------------|----------------|---------|--------|
| Go | 1.23.4 (go.mod) / 1.25.0 (system) | 1.24+ in go.mod | Language runtime | Update go.mod `go` directive |
| Ginkgo v2 | v2.13.0 | v2.28.1+ | BDD test framework | `go get github.com/onsi/ginkgo/v2@latest` |
| Gomega | v1.29.0 | v1.39.1+ | Matcher/assertion library | `go get github.com/onsi/gomega@latest` |
| gotest.tools/v3 | v3.5.2 | v3.5.2 (keep) | CLI command execution via `icmd` | No change needed |

### Already Present (No changes in this phase)

| Library | Version | Purpose | Status |
|---------|---------|---------|--------|
| `github.com/tektoncd/pipeline` | v0.68.0 | Tekton Pipeline API types | Keep -- upgrade deferred to v2 |
| `github.com/tektoncd/operator` | v0.75.0 | Tekton Operator API types | Keep -- upgrade deferred to v2 |
| `github.com/xanzy/go-gitlab` | v0.109.0 | GitLab API client | Keep -- migration deferred to v2 |
| `k8s.io/client-go` | v0.29.6 (via replace) | Kubernetes client | Keep -- pinned to match Tekton deps |

### Alternatives Considered

None -- this phase has no library selection decisions. It is purely about fixing imports, renaming paths, and upgrading versions of already-chosen tools.

## Architecture Patterns

### Pattern 1: Import Fix for `pkg/oc/oc.go`

**What:** Replace the 3 Gauge repo imports with local package equivalents.
**Confidence:** HIGH (verified by reading both source files)

The current imports in `pkg/oc/oc.go`:
```go
import (
    "github.com/openshift-pipelines/release-tests/pkg/cmd"     // Gauge repo
    "github.com/openshift-pipelines/release-tests/pkg/config"   // Gauge repo
    "github.com/openshift-pipelines/release-tests/pkg/store"    // Gauge repo
)
```

Must become (after module path rename):
```go
import (
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)
```

**Two API differences must be resolved:**

1. **`config.Path()` return type difference:**
   - Gauge repo: `func Path(elem ...string) string` (returns just string)
   - Local repo: `func Path(elem ...string) (string, error)` (returns string + error)
   - `pkg/oc/oc.go` calls `config.Path(path_dir)` at lines 21, 30, 38 treating it as single-return
   - **Fix options:**
     - (A) Change local `config.Path()` to return just `string` (matching Gauge behavior, panic/Fail on error) -- simplest, keeps `oc.go` callers clean
     - (B) Update `oc.go` callers to handle the error -- more Go-idiomatic but adds boilerplate to 3 call sites
   - **Recommendation:** Option A -- the local `config.Path()` already constructs a path from `Dir()` which uses `runtime.Caller`. The error check is for "directory not found" which is a programming error, not a runtime condition. Use `Expect().NotTo(HaveOccurred())` or a `MustPath()` wrapper.

2. **`cmd.MustSuccedIncreasedTimeout` typo:**
   - Gauge repo: `MustSuccedIncreasedTimeout` (typo -- missing 'e')
   - Local repo: `MustSucceedIncreasedTimeout` (corrected spelling)
   - `pkg/oc/oc.go` calls the typo version at lines 38 and 110
   - **Fix:** Update call sites to use `cmd.MustSucceedIncreasedTimeout` (corrected name already exists in local `pkg/cmd/cmd.go`)

### Pattern 2: Module Path Rename

**What:** Global find-and-replace of module path across all Go source files and go.mod.
**Confidence:** HIGH

Files requiring the rename (exhaustive list from grep):

| File | Lines with old path |
|------|-------------------|
| `go.mod` | Line 1: module declaration |
| `pkg/cmd/cmd.go` | Line 7: imports `pkg/config` |
| `pkg/store/store.go` | Lines 7-8: imports `pkg/clients`, `pkg/opc` |
| `pkg/opc/opc.go` | Line 14: imports `pkg/cmd` |
| `pkg/pac/pac.go` | Lines 8-9: imports `pkg/oc`, `pkg/store` |
| `tests/pac/pac_test.go` | Line 5: imports `pkg/pac` |

Replace: `github.com/srivickynesh/release-tests-ginkgo` with `github.com/openshift-pipelines/release-tests-ginkgo`

**Order matters:** Rename the module path AFTER fixing `pkg/oc/oc.go` imports, because `oc.go` currently imports from the Gauge repo (not using the old module path at all). If you rename first, `oc.go` still points to the Gauge repo. If you fix `oc.go` first, you temporarily write the old module path which then gets caught in the rename pass.

**Recommended sequence:** Fix `oc.go` imports using the NEW module path directly, then rename the remaining files. This avoids a two-pass edit.

### Pattern 3: Go Toolchain Upgrade in go.mod

**What:** Update the `go` and `toolchain` directives in go.mod.
**Confidence:** HIGH

```
# Current
go 1.23.4
toolchain go1.23.6

# Target
go 1.24
toolchain go1.25.0
```

The system has Go 1.25.0 installed. Setting `go 1.24` as the minimum allows the project to be built with Go 1.24 or later. Setting `toolchain go1.25.0` records the actual toolchain used for builds.

**Note:** After changing the go directive, running `go mod tidy` may update indirect dependency versions as Go 1.24+ resolves modules differently. This is expected and safe.

### Anti-Patterns to Avoid

- **Do NOT upgrade Tekton/K8s dependencies in this phase.** Mixing framework migration with API version bumps creates unnecessary risk. The `replace` directives for `k8s.io/*` must stay pinned to v0.29.6 to match `tektoncd/pipeline v0.68.0`.
- **Do NOT migrate `xanzy/go-gitlab` in this phase.** It works fine at v0.109.0, just deprecated. Separate concern.
- **Do NOT remove `pkg/store` or refactor it to closure-based patterns yet.** Phase 1 only fixes the dual-store import issue. Store refactoring is Phase 2 (Suite Structure) work.
- **Do NOT create `suite_test.go` files in this phase.** Suite scaffolding is Phase 2.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Module path rename | Manual per-file editing | `sed` or `gofmt -r` across all `.go` files | Human error in string replacement; grep first to find all instances |
| Dependency cleanup | Manual go.mod editing | `go mod tidy` after removing the `release-tests` import | `go mod tidy` correctly removes unused direct AND transitive deps |
| Ginkgo/Gomega version selection | Guessing compatible versions | `go get github.com/onsi/ginkgo/v2@latest` | Go modules resolve compatible versions automatically |

**Key insight:** Nearly all Phase 1 work is mechanical (find-replace, version bumps, tidy). The only design decision is how to handle the `config.Path()` return type difference.

## Common Pitfalls

### Pitfall 1: Editing `oc.go` Without Handling API Differences
**What goes wrong:** Developer changes the import paths in `oc.go` from Gauge repo to local packages, but doesn't realize `config.Path()` now returns `(string, error)` instead of `string`. Build breaks with "multiple-value config.Path() used in single-value context" at 3 call sites.
**Why it happens:** The local `config` package was rewritten with different signatures than the Gauge original.
**How to avoid:** Either change local `config.Path()` to return just `string` (recommended), or update all 3 call sites in `oc.go` to handle the error.
**Warning signs:** Compilation errors after import path change.

### Pitfall 2: Running `go mod tidy` Before Fixing All Imports
**What goes wrong:** `go mod tidy` is run while `pkg/oc/oc.go` still imports from `github.com/openshift-pipelines/release-tests`. Tidy keeps the Gauge dependency and all its transitive deps (gauge-go, gauge-common, goproperties).
**Why it happens:** `go mod tidy` analyzes imports in source files. It will keep any dependency that is actually imported.
**How to avoid:** Fix ALL imports in `pkg/oc/oc.go` first, then run `go mod tidy`. Verify with `grep -r "release-tests/pkg" *.go` that zero results remain.
**Warning signs:** `go.sum` still contains gauge-related entries after tidy.

### Pitfall 3: Forgetting to Remove the Direct Dependency Line
**What goes wrong:** After fixing imports, the `require github.com/openshift-pipelines/release-tests v0.0.0-...` line remains in go.mod as a direct dependency even though no code imports it.
**Why it happens:** `go mod tidy` should handle this, but only if ALL imports are truly removed.
**How to avoid:** After fixing `pkg/oc/oc.go`, verify no Go file imports from `release-tests`, then run `go mod tidy`. Manually remove the `require` line if tidy doesn't.
**Warning signs:** `go list -m all | grep release-tests` returns a result after tidy.

### Pitfall 4: Module Path Rename Missing a File
**What goes wrong:** One file still uses `github.com/srivickynesh/release-tests-ginkgo` after rename, causing "cannot find module providing package" errors.
**Why it happens:** grep missed a file, or a new file was added between the grep and the rename.
**How to avoid:** Use `grep -rn "srivickynesh" --include="*.go" .` after rename to verify zero results. Also check `go.mod` line 1.
**Warning signs:** `go build ./...` fails with "cannot find module" errors.

### Pitfall 5: Go Version Bump Breaking Replace Directives
**What goes wrong:** Updating Go from 1.23 to 1.24+ causes `go mod tidy` to flag or modify the `replace` directives for `k8s.io/*` packages.
**Why it happens:** Go 1.24 introduced stricter module graph pruning. Replace directives that override versions declared by direct dependencies may be handled differently.
**How to avoid:** Run `go mod tidy` after the version bump and inspect the diff carefully. If replace directives change, verify they still point to v0.29.6 for all `k8s.io/*` packages (required for `tektoncd/pipeline v0.68.0` compatibility).
**Warning signs:** `go.sum` changes drastically; `k8s.io` versions in replace block change.

## Code Examples

### Example 1: Fixed `pkg/oc/oc.go` Import Block

```go
// BEFORE (current -- imports from Gauge repo)
import (
    "github.com/openshift-pipelines/release-tests/pkg/cmd"
    "github.com/openshift-pipelines/release-tests/pkg/config"
    "github.com/openshift-pipelines/release-tests/pkg/store"
)

// AFTER (fixed -- imports from local packages with new module path)
import (
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
    "github.com/openshift-pipelines/release-tests-ginkgo/pkg/store"
)
```

### Example 2: `config.Path()` Signature Alignment

```go
// Option A (recommended): Change local config.Path to match Gauge's single-return signature
// In pkg/config/config.go:
func Path(elem ...string) string {
    td := filepath.Join(Dir(), "..")
    if _, err := os.Stat(td); os.IsNotExist(err) {
        panic(fmt.Sprintf("test data path not found: %s", td))
    }
    return filepath.Join(append([]string{td}, elem...)...)
}

// This keeps oc.go callers clean:
//   config.Path(path_dir)   // works as single-value
```

### Example 3: Fix `MustSuccedIncreasedTimeout` Typo in `oc.go`

```go
// BEFORE (oc.go line 38)
cmd.MustSuccedIncreasedTimeout(time.Second*300, "oc", "delete", ...)

// AFTER (using corrected name from local cmd package)
cmd.MustSucceedIncreasedTimeout(time.Second*300, "oc", "delete", ...)
```

### Example 4: Module Path Rename Command

```bash
# Rename module path in go.mod
sed -i '' 's|github.com/srivickynesh/release-tests-ginkgo|github.com/openshift-pipelines/release-tests-ginkgo|g' go.mod

# Rename imports in all Go files
find . -name "*.go" -not -path "./.planning/*" -exec sed -i '' \
  's|github.com/srivickynesh/release-tests-ginkgo|github.com/openshift-pipelines/release-tests-ginkgo|g' {} +

# Verify no old path remains
grep -rn "srivickynesh" --include="*.go" .
grep "srivickynesh" go.mod
```

### Example 5: Go/Ginkgo/Gomega Upgrade Commands

```bash
# Update go.mod go directive (manual edit or use go mod edit)
go mod edit -go=1.24

# Upgrade Ginkgo and Gomega
go get github.com/onsi/ginkgo/v2@latest
go get github.com/onsi/gomega@latest

# Remove the Gauge repo dependency (after fixing oc.go imports)
go mod edit -droprequire github.com/openshift-pipelines/release-tests

# Tidy (removes transitive Gauge deps)
go mod tidy

# Verify Gauge deps are gone
go list -m all | grep -i gauge   # should return nothing
go list -m all | grep release-tests  # should return nothing (only release-tests-ginkgo)

# Verify build
go build ./...

# Install matching Ginkgo CLI
go install github.com/onsi/ginkgo/v2/ginkgo@latest
```

## State of the Art

| Old State | Target State | Impact |
|-----------|-------------|--------|
| go.mod: `go 1.23.4` | `go 1.24` | Go 1.23 lost support late 2025; 1.24+ required for latest Ginkgo/Gomega |
| Ginkgo v2.13.0 (Nov 2023) | v2.28.1+ (Jan 2026) | 15+ releases of bug fixes, performance improvements, new features (SpecTimeout, improved JUnit, better parallel support) |
| Gomega v1.29.0 (Nov 2023) | v1.39.1+ (Jan 2026) | 10+ releases of matcher improvements |
| Module: `srivickynesh/release-tests-ginkgo` | `openshift-pipelines/release-tests-ginkgo` | Correct org path, enables `go get`, proper CI caching |
| `pkg/oc/oc.go` imports Gauge repo | Imports local packages | Eliminates dual-store corruption, removes Gauge transitive deps |
| `go.sum` contains gauge-go, gauge-common | No Gauge entries | Clean dependency tree |

## Detailed Import Dependency Graph

Understanding exactly what imports what is critical for getting the fix order right.

### Current State (BROKEN for Ginkgo usage)

```
pkg/oc/oc.go
  -> github.com/openshift-pipelines/release-tests/pkg/cmd     [GAUGE REPO - uses gauge testsuit.T]
  -> github.com/openshift-pipelines/release-tests/pkg/config   [GAUGE REPO - Path() returns string]
  -> github.com/openshift-pipelines/release-tests/pkg/store    [GAUGE REPO - uses gauge.GetScenarioStore()]
  -> github.com/onsi/ginkgo/v2                                 [LOCAL]
  -> github.com/onsi/gomega                                    [LOCAL]

pkg/cmd/cmd.go
  -> github.com/srivickynesh/release-tests-ginkgo/pkg/config   [LOCAL - correct, needs rename]
  -> github.com/onsi/gomega                                    [LOCAL]

pkg/store/store.go
  -> github.com/srivickynesh/release-tests-ginkgo/pkg/clients  [LOCAL - correct, needs rename]
  -> github.com/srivickynesh/release-tests-ginkgo/pkg/opc      [LOCAL - correct, needs rename]

pkg/opc/opc.go
  -> github.com/srivickynesh/release-tests-ginkgo/pkg/cmd      [LOCAL - correct, needs rename]

pkg/pac/pac.go
  -> github.com/srivickynesh/release-tests-ginkgo/pkg/oc       [LOCAL - correct, needs rename]
  -> github.com/srivickynesh/release-tests-ginkgo/pkg/store     [LOCAL - correct, needs rename]

tests/pac/pac_test.go
  -> github.com/srivickynesh/release-tests-ginkgo/pkg/pac      [LOCAL - correct, needs rename]
```

### Target State (FIXED)

All `github.com/srivickynesh/release-tests-ginkgo` -> `github.com/openshift-pipelines/release-tests-ginkgo`
All `github.com/openshift-pipelines/release-tests/pkg/*` -> `github.com/openshift-pipelines/release-tests-ginkgo/pkg/*`

### Why It's Only One File

`pkg/oc/oc.go` is the ONLY file that imports from the old Gauge repo (`github.com/openshift-pipelines/release-tests`). All other files already use local imports (just with the wrong module path prefix). This makes FOUND-01 a targeted 1-file fix.

## Gauge Dependencies That Will Be Removed

After fixing `pkg/oc/oc.go` and running `go mod tidy`, these dependencies will be removed:

| Dependency | Type | Why It Exists | Removed By |
|-----------|------|---------------|------------|
| `github.com/openshift-pipelines/release-tests` | direct | `pkg/oc/oc.go` imports its `cmd`, `config`, `store` | Fixing oc.go imports |
| `github.com/getgauge-contrib/gauge-go v0.4.0` | indirect | Transitive via `release-tests/pkg/store` (uses `gauge.GetScenarioStore()`) | `go mod tidy` |
| `github.com/getgauge/common` | indirect | Transitive via gauge-go | `go mod tidy` |
| `github.com/dmotylev/goproperties` | indirect | Transitive via gauge-go | `go mod tidy` |

## Recommended Execution Order

The order of operations within this phase matters to avoid build breakage:

1. **Clone reference repo** (FOUND-05) -- independent, do first or in parallel
2. **Fix `pkg/oc/oc.go` imports** (FOUND-01) -- switch 3 imports to local packages, fix `config.Path()` signature, fix `MustSuccedIncreasedTimeout` -> `MustSucceedIncreasedTimeout`
3. **Rename module path** (FOUND-02) -- global find-replace across go.mod + 5 Go files
4. **Upgrade Go version** (FOUND-03) -- update go.mod `go` and `toolchain` directives
5. **Upgrade Ginkgo/Gomega** (FOUND-04) -- `go get` to latest versions
6. **Remove Gauge deps and tidy** (FOUND-06) -- `go mod edit -droprequire`, then `go mod tidy`
7. **Verify** -- `go build ./...` succeeds, `grep` confirms no old paths remain

Steps 2-3 can be combined into a single editing pass. Steps 4-6 can be combined into a single `go get` + `go mod tidy` pass.

## Open Questions

1. **Should `config.Path()` return `string` or `(string, error)`?**
   - What we know: The Gauge version returns `string`. The local version returns `(string, error)`. The error case is "test data directory not found" which is a setup error, not a runtime condition.
   - Recommendation: Change to return `string` and panic on error (Option A). This matches Go testing convention where test setup failures should halt the test, not be propagated as errors. However, the project owner may prefer the error-returning signature.
   - **Impact if deferred:** LOW -- either approach works. The planner should pick one.

2. **Should Go version be set to 1.24 or 1.25 in go.mod?**
   - What we know: System has Go 1.25.0. Go 1.24 is the minimum for latest Ginkgo/Gomega.
   - Recommendation: Set `go 1.24` to allow building with Go 1.24+ (broader compatibility). The `toolchain` directive will record the actual version used.
   - **Impact if deferred:** NONE -- can be decided during planning.

## Sources

### Primary (HIGH confidence)
- Direct codebase inspection of `pkg/oc/oc.go`, `pkg/cmd/cmd.go`, `pkg/config/config.go`, `pkg/store/store.go`, `pkg/pac/pac.go`, `pkg/opc/opc.go`, `tests/pac/pac_test.go`, `pkg/clients/clients.go`, `go.mod`
- Direct inspection of Gauge repo's cached modules at `$(go env GOMODCACHE)/github.com/openshift-pipelines/release-tests@v0.0.0-20250509122825-94ba39df192b/pkg/{cmd,config,store}/`
- `go build ./...` -- confirmed current build succeeds (Gauge deps compile but are semantically wrong for Ginkgo)
- `go version` -- confirmed Go 1.25.0 installed on system
- Ginkgo v2.28.1 release notes (verified via GitHub releases in earlier research)
- Gomega v1.39.1 release notes (verified via GitHub releases in earlier research)

### Secondary (MEDIUM confidence)
- Go release history at go.dev/doc/devel/release -- Go 1.23 support timeline
- Earlier project research documents: `.planning/research/STACK.md`, `.planning/research/PITFALLS.md`, `.planning/research/ARCHITECTURE.md`

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- no new libraries, only version bumps of existing deps
- Architecture: HIGH -- the import fix is verified by reading both source repos
- Pitfalls: HIGH -- all pitfalls identified by direct code inspection and build testing

**Research date:** 2026-03-31
**Valid until:** 2026-04-30 (stable -- no fast-moving dependencies in this phase)
