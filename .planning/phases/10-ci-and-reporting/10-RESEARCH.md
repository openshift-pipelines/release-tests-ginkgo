# Phase 10 Research: CI and Reporting

**Phase:** 10-ci-and-reporting
**Researched:** 2026-03-31
**Confidence:** HIGH

## Scope

Phase 10 delivers the CI infrastructure for running the migrated Ginkgo test suite: a purpose-built Docker image (replacing Gauge), label-based filtering in CI pipelines, diagnostic collection on test failure, parallel execution with SynchronizedBeforeSuite, and proper timeout/safety configuration.

**Requirements:** CI-01, CI-02, CI-03, CI-04, CI-05, CI-06, CI-07

## Current State Analysis

### Existing Docker Images (reference-repo)

Two Dockerfiles exist in the Gauge reference repo:

1. **Dockerfile** -- Minimal: extends `quay.io/openshift-pipeline/ci` (has Gauge pre-installed), copies tests, configures Gauge runner timeouts. Not useful for Ginkgo.

2. **Dockerfile.CI** -- Full CI image built on Fedora 44. Installs:
   - System tools: azure-cli, git, go, jq, make, openssl, python3, skopeo, vim, wget, yq
   - OpenShift tools: oc (4.19), oc-mirror, opm, rosa, tkn/tkn-pac/opc (1.20.0)
   - Signing tools: cosign (3.0.3), rekor-cli (1.4.3)
   - Dev tools: golangci-lint (2.7.2), mc (MinIO client)
   - Gauge: gauge (1.6.25) + plugins (go, html-report, xml-report, reportportal)
   - Python: pyyaml, reportportal-client
   - Red Hat certs for internal registry access

   This is the primary reference for the Ginkgo CI image. Most tools carry over; Gauge is replaced by Ginkgo CLI.

### Existing Suite Structure

All 11 test areas have `suite_test.go` files (created in Phase 2) with:
- `BeforeSuite` initializing `sharedClients` via `clients.NewClients()`
- `AfterSuite` calling `config.RemoveTempDir()`
- Suite-level `Label()` matching directory name
- Ginkgo v2.28.1 / Gomega v1.39.1 / Go 1.24

### Key Decisions from Prior Phases

- Suite-level Labels match directory names (Label("ecosystem"), Label("pac"), etc.)
- All pattern specs use PDescribe (pending) to avoid requiring live cluster
- `config.Path()` panics on missing test data path (setup error, not runtime)
- Module path is `github.com/openshift-pipelines/release-tests-ginkgo`

## Technical Analysis

### CI-01: Docker Image with Ginkgo CLI

The Ginkgo CLI binary is required for `--label-filter`, `--junit-report`, parallel execution (`-p`), and `--fail-on-focused`. Running via `go test` alone loses these capabilities.

**Approach:** Create a new `Dockerfile` that:
1. Bases on Fedora 44 (same as Dockerfile.CI)
2. Keeps all OpenShift/Tekton/signing tools (oc, tkn, opc, cosign, rekor-cli, etc.)
3. Removes Gauge and all Gauge plugins
4. Installs Ginkgo CLI via `go install github.com/onsi/ginkgo/v2/ginkgo@v2.28.1`
5. Pre-downloads Go module cache (`go mod download`)
6. Sets proper permissions for OpenShift (group 0 writable)

**Ginkgo CLI install:** `go install github.com/onsi/ginkgo/v2/ginkgo@v2.28.1` produces a single binary at `$GOPATH/bin/ginkgo`. This is the standard installation method from official docs.

### CI-02: Label Filtering in CI

Already established in Phase 2 suite scaffolding. CI pipelines use:
- `ginkgo run --label-filter="sanity" ./tests/...` -- sanity only
- `ginkgo run --label-filter="e2e && !disconnected" ./tests/...` -- e2e minus disconnected
- `ginkgo run --label-filter="operator && sanity" ./tests/...` -- area + tag combo

The wrapper script standardizes these invocations with presets.

### CI-03: JUnit XML Post-Processing for Polarion

Known gap from research: Ginkgo's `--junit-report` output lacks `<properties>` section Polarion requires. Three options:

1. **ReportAfterSuite with JunitReportConfig** -- Use `reporters.GenerateJUnitReportWithConfig()` to control format. Can set `OmitLeafNodeType`, `OmitSpecLabels`, etc. Does NOT add Polarion `<properties>`.
2. **Post-processing script** -- Go program that reads Ginkgo JUnit XML, injects Polarion `<properties>`, maps test names to test case IDs.
3. **Both** -- Use JunitReportConfig for clean output, then post-process for Polarion properties.

**Recommendation:** Option 3. The JunitReportConfig cleans up Ginkgo-specific noise, and a simple Go post-processor adds Polarion-required `<properties>` with project ID. The post-processor is a standalone `cmd/junit-polarion/main.go` that:
- Reads JUnit XML
- Injects `<properties><property name="polarion-project-id" value="PIPELINES"/></properties>`
- Extracts test case IDs from spec names (regex: `PIPELINES-\d+-TC\d+`)
- Writes transformed XML

### CI-04: ReportAfterEach for Diagnostic Collection

When a test fails in CI, the report should include:
- Pod logs from the test namespace
- Kubernetes events from the test namespace
- Resource state (PipelineRuns, TaskRuns status)

**Implementation:** A shared `ReportAfterEach` in each `suite_test.go` that checks `CurrentSpecReport().Failed()` and collects diagnostics via `oc` commands, attaching them via `AddReportEntry()`.

**Key consideration:** Diagnostics must use the test's namespace. Since namespace is a closure variable in `BeforeEach`, the `ReportAfterEach` needs access to it. Two approaches:
1. Package-level variable `currentNamespace` set in `BeforeEach` -- simple but global state
2. Use `CurrentSpecReport().FullText()` to derive namespace -- fragile

**Recommendation:** Use a package-level `lastNamespace` variable set in `BeforeEach`. This is acceptable because `ReportAfterEach` runs synchronously after each spec, before the next spec's `BeforeEach`.

### CI-05: Parallel Execution with SynchronizedBeforeSuite

Current `BeforeSuite` creates `sharedClients` on each process. For parallel mode:

1. **SynchronizedBeforeSuite** -- First function (node 1 only) creates clients, validates cluster connectivity, downloads OPC binary if needed. Serializes config as JSON. Second function (all nodes) deserializes config and creates node-local clients.

2. **Namespace isolation** -- Each parallel process generates namespaces with `GinkgoParallelProcess()` suffix to prevent collisions.

3. **Serial tests** -- Operator tests modifying TektonConfig must be `Serial`. Already planned in Phase 6.

**SynchronizedBeforeSuite pattern:**
```go
var _ = SynchronizedBeforeSuite(
    func() []byte {
        // Node 1 only: validate cluster, download OPC
        clients, err := clients.NewClients(...)
        Expect(err).NotTo(HaveOccurred())
        config := SerializeClientConfig(clients)
        return config
    },
    func(configBytes []byte) {
        // All nodes: deserialize
        sharedClients = DeserializeClientConfig(configBytes)
    },
)
```

**Critical:** This changes the `suite_test.go` template. Must be done carefully to not break sequential mode (SynchronizedBeforeSuite works in both sequential and parallel modes).

### CI-06: Suite Timeout

Research identified Ginkgo v2 default timeout of 1 hour. With 110+ tests, Tekton Results finalizers (150+ second waits), and operator reconciliation, total runtime can exceed 1 hour.

**Approach:**
- Set `--timeout=4h` as default in wrapper script
- Allow override via environment variable `GINKGO_TIMEOUT`
- Add per-spec timeouts via `SpecTimeout()` for known long-running tests (operator install: 15min, pipeline runs: 10min)

### CI-07: --fail-on-focused

Prevents `FDescribe`/`FIt`/`FContext`/`FEntry` from being committed. Must be set in CI, not in local development.

**Approach:** Always set `--fail-on-focused` in the wrapper script's CI mode. Local mode omits it to allow developer focus during debugging.

## Task Decomposition

### Wave 1 (independent, can run in parallel)

**Plan 10-01: Docker Image and Wrapper Script** (CI-01, CI-02, CI-06, CI-07)
- Create Dockerfile replacing Gauge with Ginkgo CLI
- Create `scripts/run-tests.sh` wrapper with label presets, timeout config, fail-on-focused

**Plan 10-02: Diagnostic Collection and JUnit Post-Processing** (CI-03, CI-04)
- Create ReportAfterEach diagnostic collector in a shared package
- Create JUnit-to-Polarion post-processing tool

### Wave 2 (depends on Wave 1)

**Plan 10-03: Parallel Execution with SynchronizedBeforeSuite** (CI-05)
- Upgrade BeforeSuite to SynchronizedBeforeSuite across all 11 suites
- Add namespace isolation with GinkgoParallelProcess()
- Validate parallel mode works correctly

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Polarion XML format mismatch | HIGH | MEDIUM | Test with sample XML before full suite; iterate transform |
| SynchronizedBeforeSuite serialization complexity | MEDIUM | HIGH | Keep serialization simple (kubeconfig path, not full client state) |
| Parallel test interference | MEDIUM | HIGH | Namespace isolation with process ID suffix; Serial decorator on cluster-wide tests |
| Docker image size bloat | LOW | LOW | Multi-stage build, clean package cache |
| Diagnostic collection slowing test suite | LOW | MEDIUM | Only collect on failure; cap log line count |

## Sources

- Ginkgo v2 Official Documentation -- SynchronizedBeforeSuite, ReportAfterEach, --fail-on-focused
- Ginkgo v2 reporters package -- JunitReportConfig
- Kubernetes E2E Testing Best Practices -- namespace isolation, diagnostic collection
- Submariner Shipyard Issue #48 -- Polarion JUnit XML incompatibility
- Existing Dockerfile.CI in reference-repo -- tool inventory for new Docker image
- Phase 2 suite_test.go template -- current BeforeSuite pattern

---
*Research completed: 2026-03-31*
