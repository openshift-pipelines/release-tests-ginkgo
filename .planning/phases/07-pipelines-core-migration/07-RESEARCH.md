# Phase 7 Research: Pipelines Core Migration

**Researched:** 2026-03-31
**Confidence:** HIGH

## Test Inventory

16 tests across 7 Gauge spec files in `specs/pipelines/`:

### run.spec (PIPELINES-03) -- 6 tests

| Test Case | Name | Tags | Status to Validate | Resources |
|-----------|------|------|--------------------|-----------|
| PIPELINES-03-TC01 | Run sample pipeline | e2e, pipelines, non-admin | successful | testdata/pvc/pvc.yaml, testdata/v1beta1/pipelinerun/pipelinerun.yaml |
| PIPELINES-03-TC04 | Pipelinerun Timeout failure | e2e, pipelines, non-admin, sanity | timeout | testdata/v1beta1/pipelinerun/pipelineruntimeout.yaml |
| PIPELINES-03-TC05 | Configure execution results at Task level | e2e, integration, pipelines, non-admin, sanity | successful | testdata/v1beta1/pipelinerun/task_results_example.yaml |
| PIPELINES-03-TC06 | Cancel pipelinerun | e2e, integration, pipelines, non-admin, sanity | cancelled | testdata/v1beta1/pipelinerun/pipelinerun.yaml, testdata/pvc/pvc.yaml |
| PIPELINES-03-TC07 | Pipelinerun with pipelinespec and taskspec | e2e, integration, pipelines, non-admin | successful | testdata/v1beta1/pipelinerun/pipelinerun-with-pipelinespec-and-taskspec.yaml |
| PIPELINES-03-TC08 | Pipelinerun with large result | e2e, integration, pipelines, non-admin, results, sanity | successful | testdata/v1beta1/pipelinerun/pipelinerun-with-large-result.yaml |

### fail.spec (PIPELINES-02) -- 2 tests

| Test Case | Name | Tags | Status to Validate | Resources |
|-----------|------|------|--------------------|-----------|
| PIPELINES-02-TC01 | Run Pipeline with non-existent ServiceAccount | e2e, pipeline, negative, non-admin, sanity | Failure | testdata/negative/v1beta1/pipelinerun.yaml |
| PIPELINES-02-TC02 | Run Task with non-existent ServiceAccount | e2e, tasks, negative, non-admin | Failure | testdata/negative/v1beta1/pull-request.yaml |

### bundles-resolver.spec (PIPELINES-25) -- 2 tests

| Test Case | Name | Tags | Status to Validate | Resources |
|-----------|------|------|--------------------|-----------|
| PIPELINES-25-TC01 | Bundles resolver functionality | e2e | successful | testdata/resolvers/pipelineruns/bundles-resolver-pipelinerun.yaml |
| PIPELINES-25-TC02 | Bundles resolver with parameter | e2e, sanity | successful | testdata/resolvers/pipelineruns/bundles-resolver-pipelinerun-param.yaml |

### cluster-resolvers.spec (PIPELINES-23) -- 2 tests

| Test Case | Name | Tags | Status to Validate | Resources |
|-----------|------|------|--------------------|-----------|
| PIPELINES-23-TC01 | Cluster resolvers #1 | e2e, sanity | successful | testdata/resolvers/pipelineruns/resolver-pipelinerun.yaml |
| PIPELINES-23-TC02 | Cluster resolvers #2 | e2e | successful | testdata/resolvers/pipelines/resolver-pipeline-same-ns.yaml, testdata/resolvers/pipelineruns/resolver-pipelinerun-same-ns.yaml |

**Special:** Cluster resolvers has Precondition (create projects "releasetest-tasks" and "releasetest-pipelines", apply tasks/pipelines) and Teardown (delete both projects). Maps to Ordered container with BeforeAll/AfterAll.

### git-resolvers.spec (PIPELINES-24) -- 2 tests

| Test Case | Name | Tags | Status to Validate | Resources |
|-----------|------|------|--------------------|-----------|
| PIPELINES-24-TC01 | Git resolver functionality | e2e, sanity | successful | testdata/resolvers/pipelineruns/git-resolver-pipelinerun.yaml |
| PIPELINES-24-TC02 | Git resolver with auth and token | e2e | successful | testdata/resolvers/pipelineruns/git-resolver-pipelinerun-private.yaml, git-resolver-pipelinerun-private-token-auth.yaml, git-resolver-pipelinerun-private-url.yaml |

**Special:** TC02 creates a secret "private-repo-auth-secret" with GitHub token, and verifies 3 pipelineruns.

### http-resolvers.spec (PIPELINES-31) -- 1 test

| Test Case | Name | Tags | Status to Validate | Resources |
|-----------|------|------|--------------------|-----------|
| PIPELINES-31-TC01 | HTTP resolver functionality | e2e, sanity | successful | testdata/resolvers/pipelineruns/http-resolver-pipelinerun.yaml |

### hub-resolvers.spec -- 1 test

| Test Case | Name | Tags | Status to Validate | Resources |
|-----------|------|------|--------------------|-----------|
| (no Polarion ID) | Hub resolver functionality | e2e, sanity | successful | testdata/resolvers/pipelines/git-cli-hub.yaml, testdata/pvc/pvc.yaml, testdata/resolvers/pipelineruns/git-cli-hub.yaml |

**Note:** Hub resolver spec lacks a Polarion test case ID. One must be assigned during migration (convention: use spec file naming pattern).

## Helper Code Analysis

### pkg/pipelines/pipelines.go

Core validation function `ValidatePipelineRun(c, prname, status, namespace)` dispatches to:
- `validatePipelineRunForSuccessStatus` -- uses `wait.WaitForPipelineRunState(c, prname, wait.PipelineRunSucceed(prname), ...)`
- `validatePipelineRunForFailedStatus` -- uses `wait.WaitForPipelineRunState(c, prname, wait.PipelineRunFailed(prname), ...)`
- `validatePipelineRunTimeoutFailure` -- complex: waits for running, then lists taskruns, waits for timeout, waits for taskrun cancellation
- `validatePipelineRunCancel` -- complex: waits for running, lists taskruns, issues `opc pipelinerun cancel`, waits for cancelled state

Error handling: All validation functions collect PipelineRun logs (via Tekton CLI) and k8s warning events on failure for diagnostic output.

**Gauge dependencies to remove:**
- `gauge.GetScenarioStore()` in `WatchForPipelineRun` and `AssertForNoNewPipelineRunCreation`
- `testsuit.T.Errorf()` throughout -- replace with Gomega assertions
- `gauge.WriteMessage()` -- replace with GinkgoWriter or AddReportEntry

### pkg/pipelines/taskrun.go

`ValidateTaskRun(c, trname, status, namespace)` -- similar dispatch pattern:
- `validateTaskRunForSuccessStatus` -- `wait.WaitForTaskRunState` + `wait.TaskRunSucceed`
- `validateTaskRunForFailedStatus` -- `wait.WaitForTaskRunState` + `wait.TaskRunFailed`
- `validateTaskRunTimeOutFailure` -- `wait.WaitForTaskRunState` + `wait.FailedWithReason("TaskRunTimeout", ...)`

Also includes `ValidateTaskRunLabelPropogation` (tests label propagation from TaskRun to Pod) and `getTaskRunNameMatches` (regex match on TaskRun name from list).

### pkg/pipelines/helper.go

Utility functions: `GetPodForTaskRun`, `AssertLabelsMatch`, `AssertAnnotationsMatch`, `Cast2pipelinerun`, `createKeyValuePairs`. All use `testsuit.T.Errorf` which must become Gomega `Expect`.

### pkg/pipelines/ecosystem.go

`AssertTaskPresent`, `AssertTaskNotPresent`, `AssertStepActionPresent`, `AssertStepActionNotPresent` -- used by ecosystem tests more than pipeline core, but lives in this package.

### pkg/wait/wait.go

Provides `WaitForPipelineRunState` and `WaitForTaskRunState` with `PollUntilContextTimeout`. These already implement polling; in Ginkgo they should be wrapped with `Eventually` for better diagnostics, but can be used directly initially since they already poll.

## Migration Patterns

### Pattern 1: Simple PipelineRun Create-and-Verify

Most tests follow: Create YAML -> Verify PipelineRun status. Maps directly to:

```go
It("PIPELINES-03-TC01: Run sample pipeline", Label("e2e", "pipelines"), func() {
    oc.CreateResource(ns, "testdata/pvc/pvc.yaml")
    oc.CreateResource(ns, "testdata/v1beta1/pipelinerun/pipelinerun.yaml")
    DeferCleanup(oc.DeleteResource, ns, "testdata/pvc/pvc.yaml")

    pipelines.ValidatePipelineRun(c, "output-pipeline-run-v1b1", "successful", ns)
})
```

### Pattern 2: Negative Tests (Failure Verification)

fail.spec tests verify ServiceAccount doesn't exist, then create run expecting failure:

```go
It("PIPELINES-02-TC01: Run Pipeline with non-existent ServiceAccount", Label("e2e", "sanity"), func() {
    // Verify SA doesn't exist
    _, err := c.KubeClient.Kube.CoreV1().ServiceAccounts(ns).Get(ctx, "foobar", metav1.GetOptions{})
    Expect(errors.IsNotFound(err)).To(BeTrue())

    oc.CreateResource(ns, "testdata/negative/v1beta1/pipelinerun.yaml")
    pipelines.ValidatePipelineRun(c, "output-pipeline-run-vb", "Failure", ns)
})
```

### Pattern 3: Resolver Tests with Preconditions (Ordered)

Cluster resolvers needs cross-namespace setup. Maps to `Ordered` container:

```go
Describe("PIPELINES-23: Cluster Resolvers", Ordered, Label("e2e"), func() {
    BeforeAll(func() {
        oc.CreateProject("releasetest-tasks")
        oc.Apply("releasetest-tasks", "testdata/resolvers/tasks/resolver-task.yaml")
        // ...
    })
    AfterAll(func() {
        oc.DeleteProject("releasetest-tasks")
        oc.DeleteProject("releasetest-pipelines")
    })
    // ... It blocks
})
```

### Pattern 4: Complex Validation (Timeout/Cancel)

Timeout and cancel tests have multi-phase validation (wait for running -> trigger cancel/timeout -> verify all TaskRuns cancelled). The existing `validatePipelineRunTimeoutFailure` and `validatePipelineRunCancel` encapsulate this. In Ginkgo, these should use `Eventually` to poll PipelineRun status rather than the existing `wait.PollUntilContextTimeout` to get better Ginkgo-native diagnostics.

## Testdata Files Required

All testdata YAML files are already present in the reference repo and need to be copied:
- `testdata/pvc/pvc.yaml` (shared with other test areas)
- `testdata/v1beta1/pipelinerun/*.yaml` (9 files)
- `testdata/negative/v1beta1/*.yaml` (2 files)
- `testdata/resolvers/pipelineruns/*.yaml` (10 files)
- `testdata/resolvers/pipelines/*.yaml` (6 files)
- `testdata/resolvers/tasks/*.yaml` (4 files)

## Ginkgo File Structure

Proposed test file layout:
```
tests/pipelines/
  suite_test.go          (already exists)
  pipelinerun_test.go    (PIPELINES-03: run.spec tests)
  failure_test.go        (PIPELINES-02: fail.spec tests)
  resolvers_test.go      (PIPELINES-23/24/25/31 + hub: all resolver tests)
```

Three test files keeps each focused. Resolvers are grouped because they share the same pattern and are logically related.

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| Timeout test takes 5+ minutes | Medium | Set SpecTimeout(10 * time.Minute) on timeout/cancel specs |
| Git resolver TC02 requires GitHub token env var | Medium | Skip if GITHUB_TOKEN not set |
| Cluster resolver cross-namespace setup | Low | Ordered container with BeforeAll/AfterAll handles this cleanly |
| Hub resolver lacks Polarion ID | Low | Assign PIPELINES-XX-TC01 during migration (document in plan) |
| Existing wait.go already polls -- wrapping in Eventually is redundant | Low | Use existing wait functions directly, wrap outer assertion in Eventually only where needed |

## Discovery Level

**Level 0 -- Skip.** All work follows established Ginkgo patterns already proven in Phase 2 (suite scaffolding) and Phase 3 (sanity migration). No new libraries, no architectural decisions. Direct Gauge-to-Ginkgo mapping using documented patterns.

---
*Research completed: 2026-03-31*
