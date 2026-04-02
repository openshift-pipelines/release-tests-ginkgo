# Phase 6 Research: Operator Migration

**Researched:** 2026-03-31
**Confidence:** HIGH
**Discovery Level:** 0 (all patterns established in prior phases)

## Source Analysis

### Gauge Spec Files (7 files, 33 scenarios)

| Spec File | Polarion ID | Scenarios | Tags |
|-----------|-------------|-----------|------|
| addon.spec | PIPELINES-15 | 6 (TC05, TC06, TC07, TC08, TC09, TC010) | e2e, integration, addon, admin |
| auto-prune.spec | PIPELINES-12 | 15 (TC01-TC15) | e2e, integration, auto-prune, admin |
| hpa.spec | PIPELINES-13 | 1 (TC01) | hpa, admin, to-do |
| post-upgrade.spec | PIPELINES-19 | 4 (TC01, TC03, TC04, TC05) | post-upgrade, admin |
| pre-upgrade.spec | PIPELINES-18 | 4 (TC01, TC03, TC04, TC05) | pre-upgrade, admin |
| rbac.spec | PIPELINES-11 | 2 (TC01, TC02) | e2e, rbac-disable, admin |
| roles.spec | PIPELINES-34 | 1 (TC02 -- note: uses PIPELINES-11-TC02 ID) | e2e, admin |

### Test Categories and Ginkgo Structure

**1. Addon Tests (6 tests) -- `addon_test.go`**
- All modify TektonConfig/TektonAddon CR (cluster-wide) -> Serial + Ordered
- Test resolverTasks enable/disable, pipeline templates toggle, versioned tasks/stepactions
- Each scenario is multi-step: toggle config, verify tasks appear/disappear, toggle back
- Negative test: TC05 expects validation error when enabling pipelineTemplates with resolverTasks disabled
- Helper functions: `operator.VerifyVersionedTasks()`, `operator.VerifyVersionedStepActions()`

**2. Auto-Prune Tests (15 tests) -- `autoprune_test.go`**
- Heaviest test area (15 of 33 tests); all modify TektonConfig pruner config (cluster-wide) -> Serial
- Each test follows pattern: remove config -> create resources -> set pruner config -> wait for pruning -> verify counts -> cleanup
- Tests use `time.Sleep` for timing-dependent pruning (MUST convert to Eventually polling)
- Key resource: cronjob with prefix "tekton-resource-pruner"
- Per-namespace annotations: `operator.tekton.dev/prune.skip`, `prune.resources`, `prune.keep`, `prune.keep-since`, `prune.schedule`, `prune.strategy`
- TC12: validation tests (expects specific error messages from invalid config)
- TC13: cronjob stability test (verify cronjob NOT re-created on random annotation)
- TC14: container count verification
- TC15: operator stability after namespace deletion with pruner annotation

**3. HPA Test (1 test) -- `hpa_test.go`**
- Tagged `to-do` in Gauge (may be incomplete/flaky)
- Scales tekton-pipelines-webhook deployment replicas
- Uses `time.Sleep(30s)` -- convert to Eventually polling for pod readiness

**4. Pre/Post Upgrade Tests (8 tests) -- `upgrade_test.go`**
- Pre-upgrade: creates resources (triggers, TLS eventlisteners, pipelines, secrets) in dedicated namespaces
- Post-upgrade: verifies those resources still work after upgrade
- These tests are upgrade-specific and ordered: pre-upgrade MUST run before post-upgrade
- Uses Ordered container with Serial decorator (cluster-level namespace creation)

**5. RBAC Tests (2 tests) -- `rbac_test.go`**
- TC01: Toggle createRbacResource param -> verify RBAC resources appear/disappear
- TC02: Toggle createCABundleConfigMaps param -> verify CA bundle configmaps
- Both modify TektonConfig CR -> Serial
- Has teardown step restoring both params to "true"

**6. Roles Test (1 test) -- `roles_test.go`**
- Verifies 26 specific roles exist in openshift-pipelines namespace
- Verifies total role count matches expected list
- Read-only test but depends on operator install state

### Step Implementations (Gauge -> Ginkgo Mapping)

| Gauge Step Function | Ginkgo Helper | Location |
|---------------------|---------------|----------|
| `operator.ValidateRBAC()` | Reuse directly | pkg/operator/operator.go |
| `operator.ValidateRBACAfterDisable()` | Reuse directly | pkg/operator/operator.go |
| `operator.ValidateCABundleConfigMaps()` | Reuse directly | pkg/operator/operator.go |
| `operator.VerifyRolesArePresent()` | Reuse directly | pkg/operator/rbac.go |
| `operator.VerifyVersionedTasks()` | Reuse directly | pkg/operator/tektonaddons.go |
| `operator.VerifyVersionedStepActions()` | Reuse directly | pkg/operator/tektonaddons.go |
| `operator.ValidateOperatorInstallStatus()` | Reuse directly | pkg/operator/operator.go |
| TektonConfig patching (oc patch) | Inline or new helper | steps/operator/operator.go |
| Pruner config update | Needs new Ginkgo helper | New function needed |
| Addon config update | Needs new Ginkgo helper | New function needed |

### Key Dependencies

- `pkg/operator/` -- 9 Go files with operator validation helpers (already in this repo as copied from Gauge)
- `pkg/clients/` -- shared Kubernetes/Tekton clients (initialized in BeforeSuite)
- `pkg/oc/` -- OpenShift CLI wrapper (create, delete, annotate)
- `pkg/cmd/` -- shell command execution
- `pkg/config/` -- constants (APITimeout, APIRetry, TargetNamespace)
- `pkg/k8s/` -- Kubernetes validation helpers (ValidateDeployments)
- `testdata/pruner/` -- YAML fixtures for pruner tests (pipeline, task, namespace YAMLs)

### Critical Migration Considerations

1. **Serial Decorator Required**: ALL 33 operator tests modify cluster-wide state (TektonConfig CR, TektonAddon CR, namespace annotations, pruner cronjobs). Every Describe/Context MUST have Serial decorator.

2. **Ordered Required for Multi-Step Tests**: Addon toggle (enable -> verify -> disable -> verify -> re-enable), pruner config (set -> wait -> verify counts -> cleanup), and upgrade (pre -> post) tests need Ordered containers.

3. **DeferCleanup for TektonConfig Restoration**: Tests that modify TektonConfig (addon, RBAC, pruner) MUST save original config in BeforeAll and restore in DeferCleanup. Without this, a failed test leaves the cluster in modified state, breaking all subsequent tests.

4. **Sleep-to-Eventually Conversion**: auto-prune tests have 7 instances of `Sleep for "N" seconds` that must become `Eventually` polling assertions. Pruner cronjob execution timing is inherently async.

5. **Testdata Fixtures**: The `testdata/pruner/` directory (6 YAML files) must be copied to the target repo.

6. **Pre/Post Upgrade Tests**: These are special -- they run in separate CI phases (before and after an OLM upgrade). In Ginkgo, they should be label-filtered: `Label("pre-upgrade")` and `Label("post-upgrade")`. They should be in a single file but separated by labels.

7. **HPA test is tagged `to-do`**: Include it but consider marking with `Pending` or `Skip("not yet implemented")` unless it is known to work.

## File Structure Plan

```
tests/operator/
  suite_test.go          (already exists)
  addon_test.go          (6 tests, Serial+Ordered)
  autoprune_test.go      (15 tests, Serial+Ordered)
  hpa_test.go            (1 test, Serial)
  rbac_test.go           (2 tests, Serial+Ordered)
  roles_test.go          (1 test, read-only)
  upgrade_test.go        (8 tests, Serial+Ordered, pre/post labels)
```

The existing `operator_patterns_test.go` contains PDescribe (pending) reference patterns and can remain as documentation.

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Pruner timing sensitivity | HIGH | Tests fail intermittently | Use Eventually with generous timeouts (3-5 min) |
| TektonConfig not restored | MEDIUM | Cascading test failures | DeferCleanup in every Ordered container |
| Upgrade tests need special CI | LOW | Tests skip in normal runs | Label filtering; document in test comments |
| HPA test flakiness | MEDIUM | Test fails on resource-constrained clusters | Mark with FlakeAttempts(2) or Pending |

---
*Research completed: 2026-03-31*
