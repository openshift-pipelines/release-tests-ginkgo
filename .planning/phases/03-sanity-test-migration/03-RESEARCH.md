# Phase 3 Research: Sanity Test Migration

**Researched:** 2026-03-31
**Confidence:** HIGH (verified against Gauge spec files and existing Ginkgo scaffolding)

## Sanity Test Inventory

The Gauge reference repo (`reference-repo/release-tests/specs/`) has **55+ scenarios tagged `sanity`** spread across every test area. These are not all candidates for Phase 3. The roadmap specifies "~9 sanity tests" to prove the migration pattern end-to-end.

### Selected 9 Tests for Phase 3

These 9 tests were selected because they are the simplest, self-contained sanity tests with no complex dependencies (no multi-step Ordered workflows, no external integrations, no cluster-wide state modifications). They span two test areas: **versions** and **pipelines**.

#### Versions Tests (2 tests)

| Polarion ID | Scenario | Tags | Gauge Steps |
|-------------|----------|------|-------------|
| PIPELINES-22-TC01 | Check server side components versions | e2e, sanity | `opc.AssertComponentVersion()` for 9 components (pipeline, triggers, operator, chains, pac, hub, results, manual-approval-gate, OSP) |
| PIPELINES-22-TC02 | Check client versions | sanity | `opc.DownloadCLIFromCluster()`, `opc.AssertClientVersion()` for tkn, tkn-pac, opc client and server |

**Gauge source:** `specs/versions.spec`
**Ginkgo target:** `tests/versions/versions_test.go`
**Helper packages used:** `pkg/opc` (AssertComponentVersion, DownloadCLIFromCluster, AssertClientVersion, AssertServerVersion)

#### Pipelines Tests (7 tests)

| Polarion ID | Scenario | Tags | Gauge Steps |
|-------------|----------|------|-------------|
| PIPELINES-03-TC04 | Pipelinerun Timeout failure Test | e2e, pipelines, non-admin, sanity | Create YAML, Verify pipelinerun status=timeout |
| PIPELINES-03-TC05 | Configure execution results at Task level | e2e, integration, pipelines, non-admin, sanity | Create YAML, Verify pipelinerun status=successful |
| PIPELINES-03-TC06 | Cancel pipelinerun Test | e2e, integration, pipelines, non-admin, sanity | Create YAML (pvc + pipelinerun), Verify pipelinerun status=cancelled |
| PIPELINES-03-TC08 | Pipelinerun with large result | e2e, integration, pipelines, non-admin, results, sanity | Create YAML, Verify pipelinerun status=successful |
| PIPELINES-02-TC01 | Run Pipeline with non-existent ServiceAccount | e2e, pipeline, negative, non-admin, sanity | Verify SA does not exist, Create YAML, Verify pipelinerun status=Failure |
| PIPELINES-25-TC02 | Bundles resolver with parameter | e2e, sanity | Create YAML, Verify pipelinerun status=successful |
| PIPELINES-24-TC01 | Git resolver | e2e, sanity | Create YAML, Verify pipelinerun status=successful |

**Gauge source:** `specs/pipelines/run.spec`, `specs/pipelines/fail.spec`, `specs/pipelines/bundles-resolver.spec`, `specs/pipelines/git-resolvers.spec`
**Ginkgo target:** `tests/pipelines/pipelines_sanity_test.go`
**Helper packages used:** `pkg/oc` (Create, Apply, Delete), `pkg/pipelines` (ValidatePipelineRun -- needs porting from reference repo)

### Tests NOT Selected for Phase 3 (and Why)

| Area | Count | Reason Deferred |
|------|-------|-----------------|
| Triggers (7 sanity) | 7 | Complex EventListener + webhook setup, TLS config, multi-resource creation |
| Operator (12 sanity) | 12 | Cluster-wide state modifications (TektonConfig patches, addon toggles), requires Serial/Ordered |
| Ecosystem (10 sanity) | 10 | Heavy DescribeTable pattern, multi-arch conditionals, build-and-push workflows |
| PAC (5 sanity) | 5 | GitLab webhook integration, multi-step Ordered workflows |
| Chains (1 sanity) | 1 | Cosign key management, image signing, TektonConfig patching |
| Results (1 sanity) | 1 | Tekton Results API validation, route creation |
| MAG (2 sanity) | 2 | Manual approval gate multi-step pipeline workflow |
| Metrics (1 sanity) | 1 | Prometheus metrics validation, job health checks |
| OLM (1 sanity) | 1 | Full operator install (50+ steps), too complex for first migration |
| Hub (1 sanity) | 1 | Tagged `to-do`, likely incomplete |
| Cluster resolvers (1 sanity) | 1 | Requires precondition projects in separate namespaces |
| HTTP resolvers (1 sanity) | 1 | Could be included but 9 is sufficient for pattern validation |
| Hub resolvers (1 sanity) | 1 | Could be included but 9 is sufficient for pattern validation |

## Helper Package Gap Analysis

### Already Available in Ginkgo Repo

| Package | Functions Available |
|---------|-------------------|
| `pkg/opc` | `AssertComponentVersion`, `DownloadCLIFromCluster`, `AssertClientVersion`, `AssertServerVersion`, `GetOPCServerVersion` |
| `pkg/oc` | `Create`, `Apply`, `Delete`, `CreateNewProject`, `DeleteProject`, `DeleteProjectIgnoreErrors`, `CheckProjectExists` |
| `pkg/cmd` | `MustSucceed`, `MustSucceedIncreasedTimeout`, `Run`, `Assert` |
| `pkg/config` | `Flags`, `TargetNamespace`, `Path()`, `APITimeout`, `APIRetry`, `CLITimeout` |
| `pkg/clients` | `NewClients` |
| `pkg/store` | `Namespace`, `Clients`, `PutScenarioData`, `GetScenarioData` |

### Must Be Ported from Reference Repo

| Package | Functions Needed | Source File |
|---------|-----------------|-------------|
| `pkg/pipelines` | `ValidatePipelineRun(c, prname, status, namespace)` | `reference-repo/release-tests/pkg/pipelines/pipelines.go:179` |
| `pkg/pipelines` | `ValidateTaskRun(c, trname, status, namespace)` | `reference-repo/release-tests/pkg/pipelines/taskrun.go:22` |
| `pkg/operator` | `ValidateOperatorInstallStatus(c, crnames)` | `reference-repo/release-tests/pkg/operator/` (precondition) |

### Test Data (YAML Fixtures)

The following testdata files are needed for the 7 pipeline tests:

| Test | Testdata Path |
|------|---------------|
| PIPELINES-03-TC04 | `testdata/v1beta1/pipelinerun/pipelineruntimeout.yaml` |
| PIPELINES-03-TC05 | `testdata/v1beta1/pipelinerun/task_results_example.yaml` |
| PIPELINES-03-TC06 | `testdata/pvc/pvc.yaml`, `testdata/v1beta1/pipelinerun/pipelinerun.yaml` |
| PIPELINES-03-TC08 | `testdata/v1beta1/pipelinerun/pipelinerun-with-large-result.yaml` |
| PIPELINES-02-TC01 | `testdata/negative/v1beta1/pipelinerun.yaml` |
| PIPELINES-25-TC02 | `testdata/resolvers/pipelineruns/bundles-resolver-pipelinerun-param.yaml` |
| PIPELINES-24-TC01 | `testdata/resolvers/pipelineruns/git-resolver-pipelinerun.yaml` |

These YAML files must be copied from `reference-repo/release-tests/testdata/` to the local repo's `testdata/` directory.

## Migration Pattern

### Gauge Step -> Ginkgo Mapping

Each Gauge scenario follows this pattern:
1. **Precondition**: `Validate Operator should be installed` -> `BeforeSuite` (already handled by suite scaffolding)
2. **Create/Apply resources**: `oc.Create(path, namespace)` or `oc.Apply(path, namespace)` -> same function call, just pass namespace from test context
3. **Verify pipelinerun**: `pipelines.ValidatePipelineRun(clients, name, status, namespace)` -> same function call, needs porting
4. **Cleanup**: Gauge auto-creates/destroys namespaces per scenario -> Ginkgo uses `DeferCleanup` with `oc.DeleteProjectIgnoreErrors`

### Ginkgo Test Structure

```go
var _ = Describe("PIPELINES-03: Pipeline Runs", Label("sanity", "pipelines"), func() {
    var ns string

    BeforeEach(func() {
        ns = "sanity-" + uuid.New().String()[:8]
        oc.CreateNewProject(ns)
        DeferCleanup(func() {
            oc.DeleteProjectIgnoreErrors(ns)
        })
    })

    It("PIPELINES-03-TC04: Pipelinerun Timeout failure", Label("e2e"), func() {
        oc.Create("testdata/v1beta1/pipelinerun/pipelineruntimeout.yaml", ns)
        pipelines.ValidatePipelineRun(sharedClients, "pear", "timeout", ns)
    })
})
```

### Polarion Test Case ID Convention

Every `Describe` or `It` block must include the Polarion test case ID in its description string. The ID format is `PIPELINES-XX-TCNN`. This ensures:
1. JUnit XML test names contain the ID for Polarion mapping
2. `--dry-run -v` output is human-readable and traceable
3. grep/search for specific test cases is trivial

### JUnit XML Validation

Ginkgo's `--junit-report=report.xml` generates JUnit XML. For Polarion compatibility:
1. Test names in XML must contain `PIPELINES-XX-TCNN` IDs
2. A post-processing step may be needed to inject `<properties>` for Polarion project ID
3. Validate with: `ginkgo run --junit-report=sanity.xml --label-filter=sanity --dry-run ./tests/versions/ ./tests/pipelines/`
4. Inspect XML for correct test case names and structure

## Existing Ginkgo Scaffolding

### Suite Entry Points (from Phase 2)

- `tests/versions/suite_test.go` - TestVersions with Label("versions")
- `tests/pipelines/suite_test.go` - TestPipelines with Label("pipelines")
- Both have `BeforeSuite` with `clients.NewClients` and `AfterSuite` with cleanup

### Pattern Reference Files (from Phase 2)

- `tests/versions/versions_patterns_test.go` - Skip pattern reference (has a placeholder sanity test)
- `tests/operator/operator_patterns_test.go` - DeferCleanup, Eventually, Ordered patterns
- `tests/ecosystem/ecosystem_patterns_test.go` - DescribeTable/Entry pattern

The pattern reference files use `PDescribe` (pending) and should remain untouched. Real sanity tests go in new files.

## Risk Assessment

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| `pkg/pipelines` not yet ported | HIGH | Plan includes task to port ValidatePipelineRun from reference repo |
| Testdata YAML files missing | HIGH | Plan includes task to copy from reference repo |
| JUnit XML not Polarion-compatible | MEDIUM | Validate with dry-run first; defer full Polarion upload to CI phase |
| Namespace collision with pattern specs | LOW | Use unique namespace prefixes (e.g., `sanity-<uuid>`) |
| Version env vars not set | LOW | Tests already use Skip pattern for missing env vars |

## Sources

- `reference-repo/release-tests/specs/versions.spec` -- 2 sanity tests
- `reference-repo/release-tests/specs/pipelines/run.spec` -- 4 sanity tests
- `reference-repo/release-tests/specs/pipelines/fail.spec` -- 1 sanity test
- `reference-repo/release-tests/specs/pipelines/bundles-resolver.spec` -- 1 sanity test
- `reference-repo/release-tests/specs/pipelines/git-resolvers.spec` -- 1 sanity test
- `reference-repo/release-tests/steps/pipeline/pipeline.go` -- Gauge step implementations for Verify pipelinerun/taskrun
- `reference-repo/release-tests/steps/cli/oc.go` -- Gauge step implementations for Create/Apply
- `reference-repo/release-tests/steps/olm/operator.go` -- Gauge step implementations for version checks
- `reference-repo/release-tests/pkg/pipelines/pipelines.go` -- ValidatePipelineRun implementation
- `pkg/opc/opc.go` -- Already ported version/CLI assertion helpers
- `pkg/oc/oc.go` -- Already ported Create/Apply/Delete helpers

---
*Research completed: 2026-03-31*
*Ready for planning: yes*
