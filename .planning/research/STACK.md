# Stack Research

**Domain:** Ginkgo-based integration testing for OpenShift Pipelines (Tekton) operator
**Researched:** 2026-03-31
**Confidence:** HIGH (core stack verified via official releases and docs; supplementary libraries verified via multiple sources)

## Current State Assessment

The repo already has a working Go module with Ginkgo v2.13.0 and Gomega v1.29.0 -- both are **significantly outdated** (13+ minor versions behind for Ginkgo, 10+ for Gomega). The Tekton client dependencies (pipeline v0.68.0, operator v0.75.0, triggers v0.31.0) are pinned to LTS versions that are approaching end-of-life (v0.68.0 EOL: 2026-01-30 -- already expired). The `k8s.io/client-go` is pinned to v0.29.6 via replace directives.

The `github.com/xanzy/go-gitlab` dependency is **deprecated** and must be migrated.

## Recommended Stack

### Core Test Framework

| Technology | Version | Purpose | Why Recommended | Confidence |
|------------|---------|---------|-----------------|------------|
| Ginkgo v2 | v2.22+ (stay on Go 1.23 compatible; v2.28.1 requires Go 1.24) | BDD test framework, suite runner, label filtering, JUnit reporting | Industry standard for K8s operator testing. Used by Kubernetes e2e framework, kubebuilder, operator-sdk, and all major OpenShift projects. Already in use, just needs version bump. | HIGH |
| Gomega | v1.36+ (stay on Go 1.23 compatible; v1.39.1 requires Go 1.24) | Matcher/assertion library | Ginkgo's companion. `Eventually()` and `Consistently()` are essential for async K8s resource assertions. `DescribeTable`/`Entry` needed for data-driven test migration from Gauge tables. | HIGH |
| Ginkgo CLI | Same version as library | Test runner binary | Required for `ginkgo run` with `--label-filter`, `--junit-report`, `--keep-going`, `-p` (parallel). `go test` alone lacks label filtering and unified JUnit output. | HIGH |

**IMPORTANT: Go version constraint.** The project uses Go 1.23.4 (toolchain 1.23.6). Ginkgo v2.28.0+ and Gomega v1.39.0+ require Go 1.24. The project must either:
1. **Stay on Ginkgo ~v2.27.5 / Gomega ~v1.38.3** (last Go 1.23 compatible releases), OR
2. **Upgrade to Go 1.24+** (recommended, since Go 1.23 lost support Jan 2026; Go 1.24.13 is current stable; Go 1.26 is latest)

**Recommendation:** Upgrade Go to 1.24 during Phase 1, then use latest Ginkgo/Gomega. Go 1.23 has been out of support since late 2025.

### Tekton / OpenShift Client Dependencies

| Technology | Current Version | Recommended Version | Purpose | Why | Confidence |
|------------|----------------|-------------------|---------|-----|------------|
| `github.com/tektoncd/pipeline` | v0.68.0 | v0.68.0 (keep) OR v1.6.1 (if upgrading) | Tekton Pipeline API types and clientsets | v0.68.0 is the LTS the current operator v0.75.0 ships. Keep pinned unless upgrading operator dep too. Tekton Pipeline v1.0+ (v1.6.1 latest LTS) is the new stable line. | MEDIUM |
| `github.com/tektoncd/operator` | v0.75.0 | v0.75.0 (keep) OR v0.78.x (latest LTS) | Tekton Operator API types (TektonConfig, TektonPipeline, etc.) | v0.75.0 LTS (EOL 2026-02-18 -- already expired). v0.78.x LTS ships Pipeline v1.6.x, EOL 2026-12-08. Upgrade when ready. | MEDIUM |
| `github.com/tektoncd/triggers` | v0.31.0 | v0.31.0 (keep) OR v0.34.x (LTS) | Tekton Triggers API types (EventListener, TriggerBinding) | v0.34.x is current LTS. Upgrade when operator dep is updated. | MEDIUM |
| `k8s.io/client-go` | v0.29.6 (via replace) | v0.29.6 (keep) OR v0.31.x (with operator upgrade) | Kubernetes API client | Must match Tekton dep versions. v0.29.6 matches tektoncd/pipeline v0.68.0. Bump only when Tekton deps bump. | HIGH |
| `k8s.io/apimachinery` | v0.29.6 (via replace) | Same as client-go | K8s API machinery types | Always keep in lockstep with client-go. | HIGH |
| `github.com/openshift/client-go` | v0.0.0-20240523 | Keep current | OpenShift API clients (Route, Config, Console) | Pinned by date hash. Only update if OpenShift API compatibility requires it. | MEDIUM |
| `github.com/openshift-pipelines/pipelines-as-code` | v0.27.2 | v0.27.2 (keep) OR v0.41.1 (latest) | PAC API types and clientsets | v0.41.1 is latest. Only upgrade if PAC API types changed and tests need new fields. | LOW |
| `github.com/openshift-pipelines/manual-approval-gate` | v0.2.2 | v0.2.2 (keep) | ApprovalTask API types | Still Technology Preview. v0.2.2 is latest known stable. | MEDIUM |
| `github.com/operator-framework/operator-lifecycle-manager` | v0.22.0 | v0.22.0 (keep) | OLM clientset for subscription/CSV tests | Stable API. No pressing reason to update for test usage. | MEDIUM |

**Dependency upgrade strategy:** Do NOT upgrade Tekton/K8s dependencies during the migration. The migration is about Gauge-to-Ginkgo test framework conversion, not API version bumps. Mixing both creates unnecessary risk. Upgrade Tekton deps in a separate post-migration effort.

### GitLab Client (ACTION REQUIRED)

| Technology | Current Version | Recommended Action | Why | Confidence |
|------------|----------------|-------------------|-----|------------|
| `github.com/xanzy/go-gitlab` | v0.109.0 | **Migrate to `gitlab.com/gitlab-org/api/client-go` v1.5.0** | `xanzy/go-gitlab` was deprecated Dec 2024. Final release was solely to redirect users. The new `gitlab.com/gitlab-org/api/client-go` v1.x has a backwards-compat guarantee. | HIGH |

**Migration note:** The API surface is largely the same (same codebase, new home). Update import paths and go.mod. The v1.x line includes a migration guide. However, this should be done as a **separate, focused change** -- not interleaved with test migration. The existing `xanzy/go-gitlab` v0.109.0 still works; it just won't receive updates.

### Command Execution

| Technology | Version | Purpose | Why Recommended | Confidence |
|------------|---------|---------|-----------------|------------|
| `gotest.tools/v3` | v3.5.2 (current, latest) | `icmd` subpackage for CLI command execution with timeout/assertions | Already in use throughout `pkg/cmd`. v3.5.2 is the latest release (Feb 2025). No action needed. | HIGH |

### Supporting Libraries (Already Present, No Changes Needed)

| Library | Version | Purpose | Status |
|---------|---------|---------|--------|
| `go.uber.org/zap` | v1.27.0 | Structured logging | Current, no update needed |
| `github.com/tektoncd/cli` | v0.38.1 (indirect) | Tekton CLI types | Transitive dep, no direct action |
| `knative.dev/pkg` | v0.0.0-20240416 | Knative utilities (transitive via Tekton) | Transitive, managed by Tekton deps |

## New Libraries to Add

### Required for Migration

| Library | Purpose | When to Add | Why |
|---------|---------|-------------|-----|
| None | -- | -- | The existing stack covers all needs. Ginkgo v2 has built-in JUnit reporting (`--junit-report`), label filtering (`Label()` decorator + `--label-filter`), table-driven tests (`DescribeTable`/`Entry`), and parallel execution support (`SynchronizedBeforeSuite`). No additional test libraries needed. |

**This is a key finding:** The migration does not require any new library dependencies. Ginkgo v2 + Gomega provide everything needed to replace Gauge's test organization, data tables, tagging, and reporting. The existing `pkg/` helpers (clients, cmd, oc, opc, pac, store, config) are framework-agnostic Go code that works with any test runner.

### Optional / Nice-to-Have

| Library | Purpose | Recommendation | Confidence |
|---------|---------|---------------|------------|
| `sigs.k8s.io/e2e-framework` | K8s e2e test framework from SIG Testing | **Do NOT add.** This is for projects starting from scratch. The project already has well-structured `pkg/clients` and `pkg/store`. Adding another framework layer creates confusion. | MEDIUM |
| `github.com/openshift/openshift-tests` framework | OpenShift's own e2e test framework | **Do NOT add.** Different scope (cluster conformance testing). The release-tests have their own patterns. | HIGH |

## Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `ginkgo` CLI | Run tests with labels, parallelism, JUnit output | Install: `go install github.com/onsi/ginkgo/v2/ginkgo@v2.27.5` (or `@latest` after Go 1.24 upgrade). Must match library version in go.mod. |
| `oc` CLI | OpenShift CLI for cluster operations | Already required. Tests invoke `oc` for resource creation/deletion. |
| `kubectl` | Kubernetes CLI | Already required. Fallback for non-OpenShift-specific operations. |

## Installation / Upgrade Commands

```bash
# Phase 1: Upgrade Go (recommended)
# Update go.mod to: go 1.24
# Update toolchain to: go1.24.13
# Then:

# Upgrade Ginkgo + Gomega to latest
go get github.com/onsi/ginkgo/v2@v2.28.1
go get github.com/onsi/gomega@v1.39.1

# Install Ginkgo CLI (must match library version)
go install github.com/onsi/ginkgo/v2/ginkgo@v2.28.1

# If staying on Go 1.23 (not recommended):
go get github.com/onsi/ginkgo/v2@v2.27.5
go get github.com/onsi/gomega@v1.38.3
go install github.com/onsi/ginkgo/v2/ginkgo@v2.27.5

# Tidy
go mod tidy
```

## Ginkgo CLI Commands for CI

```bash
# Run all tests with JUnit output
ginkgo run --junit-report=junit.xml --keep-going -v ./tests/...

# Run only sanity-labeled tests
ginkgo run --label-filter="sanity" --junit-report=junit.xml -v ./tests/...

# Run smoke tests excluding disconnected
ginkgo run --label-filter="smoke && !disconnected" --junit-report=junit.xml -v ./tests/...

# Run with parallelism (when tests support it)
ginkgo run -p --label-filter="e2e" --junit-report=junit.xml -v ./tests/...

# Keep separate per-suite reports (useful for Polarion)
ginkgo run --junit-report=junit.xml --keep-separate-reports --output-dir=./artifacts ./tests/...
```

## Alternatives Considered

| Category | Recommended | Alternative | Why Not |
|----------|-------------|-------------|---------|
| Test framework | Ginkgo v2 | Standard `go test` + testify | Ginkgo provides BDD structure matching Gauge's spec/scenario model, built-in label filtering (replacing Gauge tags), JUnit reporting, and parallel execution. `go test` would require reimplementing all of this. |
| Test framework | Ginkgo v2 | Gauge (keep current) | Gauge is being abandoned by the team. ThoughtWorks ended active development. Ginkgo is the OpenShift/K8s ecosystem standard. |
| Assertion library | Gomega | testify/assert | Gomega's `Eventually()`/`Consistently()` are purpose-built for K8s async assertions. testify lacks these. Gomega is Ginkgo's native companion -- better integration, better error messages. |
| GitLab client | `gitlab.com/gitlab-org/api/client-go` | Keep `xanzy/go-gitlab` | Deprecated since Dec 2024. Will not receive security patches. Migration is straightforward (same API surface, new import path). |
| K8s e2e framework | Custom `pkg/` helpers | `sigs.k8s.io/e2e-framework` | Existing helpers are well-tailored to OpenShift Pipelines. Adding a generic framework would require rewriting working code for no benefit. |
| Command execution | `gotest.tools/v3/icmd` | `os/exec` directly | `icmd` provides timeout handling, exit code assertions, and stdout/stderr capture in a test-friendly API. Already integrated throughout `pkg/cmd`. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `github.com/xanzy/go-gitlab` (for new code) | Deprecated Dec 2024, no further updates | `gitlab.com/gitlab-org/api/client-go` v1.x |
| `github.com/onsi/ginkgo` (v1) | Deprecated since 2022. V1 import path `github.com/onsi/ginkgo` (no `/v2`). | `github.com/onsi/ginkgo/v2` |
| `github.com/onsi/ginkgo/extensions/table` | Merged into core Ginkgo v2. This v1 extension package is dead. | `DescribeTable`/`Entry` from `github.com/onsi/ginkgo/v2` directly |
| `RunSpecsWithDefaultAndCustomReporters` | Deprecated in Ginkgo v2. | `RunSpecs()` + `--junit-report` CLI flag |
| `sigs.k8s.io/e2e-framework` | Adds complexity without benefit for this project. Different paradigm (resource lifecycle management) vs. the project's direct CLI/API testing approach. | Keep existing `pkg/clients`, `pkg/oc`, `pkg/cmd` helpers |
| `github.com/getgauge-contrib/gauge-go` | Still in indirect deps from `release-tests` import. Should not be used in new test code. | Ginkgo v2 constructs (`Describe`, `It`, `BeforeEach`, `Label`, `DescribeTable`) |
| `envtest` (`sigs.k8s.io/controller-runtime/pkg/envtest`) | Runs a lightweight local K8s API server. This project tests against **real** OpenShift clusters with full operator stack. envtest cannot run Tekton Operator, OLM, or OpenShift-specific controllers. | Direct cluster access via `pkg/clients` with real kubeconfig |

## Version Compatibility Matrix

| Component | Compatible With | Notes |
|-----------|-----------------|-------|
| Ginkgo v2.27.5 | Go 1.22-1.23 | Last version before Go 1.24 requirement |
| Ginkgo v2.28.0+ | Go 1.24+ | Requires Go 1.24 minimum |
| Gomega v1.38.3 | Go 1.22-1.23, Ginkgo v2.27.x | Last version before Go 1.24 requirement |
| Gomega v1.39.0+ | Go 1.24+ | Requires Go 1.24 minimum |
| `tektoncd/pipeline` v0.68.0 | `k8s.io/client-go` v0.29.x | Must keep replace directives in go.mod |
| `tektoncd/pipeline` v1.6.x | `k8s.io/client-go` v0.31.x | Would require updating all replace directives |
| `tektoncd/operator` v0.75.0 | `tektoncd/pipeline` v0.68.x | Current pairing in go.mod |
| `tektoncd/operator` v0.78.x | `tektoncd/pipeline` v1.6.x | Latest LTS pairing, EOL 2026-12-08 |

## Stack Patterns by Phase

**During migration (Phases 1-3):**
- Keep all Tekton/K8s deps at current versions (pipeline v0.68.0, operator v0.75.0)
- Upgrade only Ginkgo/Gomega and Go version
- Focus on test framework conversion, not API version bumps

**Post-migration (Phase 4+):**
- Upgrade Tekton deps to latest LTS (operator v0.78.x, pipeline v1.6.x)
- Migrate `xanzy/go-gitlab` to `gitlab.com/gitlab-org/api/client-go`
- Update k8s.io replace directives to match new Tekton deps
- Update module path from `github.com/srivickynesh/release-tests-ginkgo` to `github.com/openshift-pipelines/release-tests-ginkgo`

## Sources

- [Ginkgo releases (GitHub)](https://github.com/onsi/ginkgo/releases) -- v2.28.1 latest (Jan 30, 2026), verified via GitHub releases page (HIGH confidence)
- [Gomega releases (GitHub)](https://github.com/onsi/gomega/releases) -- v1.39.1 latest (Jan 30, 2026), verified via GitHub releases page (HIGH confidence)
- [Ginkgo documentation](https://onsi.github.io/ginkgo/) -- Label filtering, DescribeTable, JUnit reporting, parallel execution (HIGH confidence)
- [Tekton Pipeline releases](https://github.com/tektoncd/pipeline/blob/main/releases.md) -- v1.6.1 latest LTS, v0.68.0 EOL 2026-01-30 (HIGH confidence)
- [Tekton Operator releases](https://github.com/tektoncd/operator/releases) -- v0.78.x latest LTS (HIGH confidence)
- [Tekton Triggers releases](https://github.com/tektoncd/triggers/releases) -- v0.34.x LTS (MEDIUM confidence, version cross-referenced across CLI and Dashboard deps)
- [xanzy/go-gitlab deprecation](https://github.com/xanzy/go-gitlab) -- Deprecated Dec 2024, migrated to `gitlab.com/gitlab-org/api/client-go` (HIGH confidence)
- [gitlab.com/gitlab-org/api/client-go](https://gitlab.com/gitlab-org/api/client-go) -- v1.5.0 latest (MEDIUM confidence)
- [Go release history](https://go.dev/doc/devel/release) -- Go 1.24.13 current stable, Go 1.23 out of support (HIGH confidence)
- [gotest.tools releases](https://github.com/gotestyourself/gotest.tools/releases) -- v3.5.2 latest (Feb 2025) (HIGH confidence)
- [Kubebuilder testing docs](https://book.kubebuilder.io/cronjob-tutorial/writing-tests.html) -- Ginkgo + K8s testing patterns (HIGH confidence)
- [Kueue testing docs](https://kueue.sigs.k8s.io/docs/contribution_guidelines/testing/) -- Label-based test filtering patterns (MEDIUM confidence)
- [Kevin Fan blog: Speeding Up K8s Controller Tests](https://kev.fan/posts/04-k8s-ginkgo-parallel-tests/) -- SynchronizedBeforeSuite parallel patterns (MEDIUM confidence)
- [OpenShift Pipelines PAC releases](https://github.com/openshift-pipelines/pipelines-as-code/releases) -- v0.41.1 latest (MEDIUM confidence)

---
*Stack research for: Gauge-to-Ginkgo migration of OpenShift Pipelines release tests*
*Researched: 2026-03-31*
