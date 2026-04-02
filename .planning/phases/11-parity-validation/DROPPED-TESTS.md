# Dropped Tests Manifest

**Date:** 2026-04-02
**Purpose:** Documents all Gauge test scenarios that were intentionally excluded from or restructured during the Ginkgo migration, with justifications for each decision.

## Summary

- **Total Gauge scenarios:** 129
- **Unique Polarion test case IDs in Gauge:** 127 (1 scenario has no Polarion ID, 1 ID appears twice due to duplicate `PIPELINES-11-TC02`)
- **Unique Polarion test case IDs in Ginkgo:** 126
- **Dropped as standalone scenario:** 1 (subsumed into OLM install ordered container)
- **Migrated as Pending (to-do in Gauge):** 13 (tests that were tagged `to-do` in Gauge and are Pending in Ginkgo)
- **Structural changes:** Multiple Gauge scenarios decomposed into Ginkgo Ordered container steps

## Dropped Tests

### Hub Area

| # | Test Name | Polarion ID | Gauge Spec File | Reason |
|---|-----------|-------------|-----------------|--------|
| 1 | Install HUB without authentication | PIPELINES-21-TC01 | `specs/hub/hub.spec` | Subsumed into OLM install ordered container (`tests/olm/olm_test.go`). The Hub installation steps (apply TektonHub, validate hub deployment) are executed as part of the full operator installation sequence. Standalone hub test was tagged `to-do` in Gauge and was never independently executable. |

## Tests Migrated as Pending

These tests were tagged `to-do` or `bug-to-fix` in the Gauge suite and have been migrated as `Pending` in Ginkgo. They exist in the Ginkgo suite for Polarion inventory coverage but do not execute.

### Triggers Area

| # | Test Name | Polarion ID | Gauge Spec File | Ginkgo Status | Reason |
|---|-----------|-------------|-----------------|---------------|--------|
| 1 | Create Eventlistener | PIPELINES-05-TC01 | `specs/triggers/eventlistener.spec` | Pending | Tagged `to-do` in Gauge -- implementation not complete in either framework |
| 2 | Create Eventlistener with github interceptor | PIPELINES-05-TC02 | `specs/triggers/eventlistener.spec` | Pending | Tagged `to-do` in Gauge -- implementation not complete in either framework |
| 3 | Create EventListener with custom interceptor | PIPELINES-05-TC03 | `specs/triggers/eventlistener.spec` | Pending | Tagged `to-do` in Gauge -- implementation not complete in either framework |
| 4 | Create EventListener with CEL interceptor with filter | PIPELINES-05-TC04 | `specs/triggers/eventlistener.spec` | Pending | Tagged `to-do` in Gauge -- implementation not complete in either framework |
| 5 | Create EventListener with CEL interceptor without filter | PIPELINES-05-TC05 | `specs/triggers/eventlistener.spec` | Pending | Tagged `to-do` in Gauge -- implementation not complete in either framework |
| 6 | Create EventListener with multiple interceptors | PIPELINES-05-TC06 | `specs/triggers/eventlistener.spec` | Pending | Tagged `to-do` in Gauge -- implementation not complete in either framework |
| 7 | Verify event message body marshalling error | PIPELINES-10-TC03 | `specs/triggers/triggerbinding.spec` | Pending | Tagged `bug-to-fix` in Gauge -- known defect, test not functional |

### PAC Area

| # | Test Name | Polarion ID | Gauge Spec File | Ginkgo Status | Reason |
|---|-----------|-------------|-----------------|---------------|--------|
| 8 | Enable/Disable PAC | PIPELINES-20-TC01 | `specs/pac/pac.spec` | Active (Ordered) | Tagged `to-do` in Gauge but fully implemented in Ginkgo as Ordered container |
| 9 | Enable/Disable PAC (application name) | PIPELINES-20-TC02 | `specs/pac/pac.spec` | PDescribe | Tagged `to-do` in Gauge -- requires GitHub App infrastructure not available |
| 10 | Auto-configure new GitHub repo | PIPELINES-20-TC03 | `specs/pac/pac.spec` | PDescribe | Tagged `to-do` in Gauge -- requires GitHub App infrastructure not available |
| 11 | Error log snippet visibility | PIPELINES-20-TC04 | `specs/pac/pac.spec` | PDescribe | Tagged `to-do` in Gauge -- requires GitHub App infrastructure not available |

### Operator Area

| # | Test Name | Polarion ID | Gauge Spec File | Ginkgo Status | Reason |
|---|-----------|-------------|-----------------|---------------|--------|
| 12 | Test HPA for tekton-pipelines-webhook deployment | PIPELINES-13-TC01 | `specs/operator/hpa.spec` | Active | Tagged `to-do` in Gauge but fully implemented in Ginkgo |

### Hub Area

| # | Test Name | Polarion ID | Gauge Spec File | Ginkgo Status | Reason |
|---|-----------|-------------|-----------------|---------------|--------|
| 13 | Install HUB without authentication | PIPELINES-21-TC01 | `specs/hub/hub.spec` | Subsumed into OLM | Tagged `to-do` in Gauge; hub install steps merged into OLM ordered container |

## Structural Changes (Not Drops)

The following tests underwent structural changes during migration but are NOT drops -- they represent the same test coverage in a different Ginkgo-native structure.

### OLM Tests Expanded into Ordered Container

The 3 Gauge scenarios in `olm.spec` (PIPELINES-09-TC01 Install, PIPELINES-09-TC02 Upgrade, PIPELINES-09-TC03 Uninstall) are migrated as a single Ordered container in `tests/olm/olm_test.go` with 38 individual `It` steps. Each Gauge scenario consisted of multiple steps that are now individual Ginkgo specs within the Ordered container, providing finer-grained failure isolation.

**Mapping:**
- PIPELINES-09-TC01 (Install) -> 27 ordered steps (subscribe, wait, configure, validate)
- PIPELINES-09-TC02 (Upgrade) -> 6 ordered steps (upgrade, wait, validate)
- PIPELINES-09-TC03 (Uninstall) -> 1 step
- PIPELINES-21-TC01 (Hub) -> subsumed into TC01 install sequence (2 steps)

### PAC Tests Expanded into Ordered Containers

The 3 GitLab PAC scenarios (PIPELINES-30-TC01/02/03) are migrated as 3 Ordered containers with multiple `It` steps each. The single PIPELINES-20-TC01 (Enable/Disable PAC) is also an Ordered container.

### Results Tests Expanded into Ordered Containers

The 2 Results scenarios (PIPELINES-26-TC01 TaskRun, PIPELINES-26-TC02 PipelineRun) are each migrated as Ordered containers with 5 steps each (setup, apply, verify stored, verify records, verify logs).

### Chains Tests Expanded into Ordered Container

The 2 Chains scenarios (PIPELINES-27-TC01, PIPELINES-27-TC02) are migrated as a single Ordered container with 5 steps covering both test cases.

### MAG Tests Expanded into Ordered Containers

The 2 MAG scenarios (PIPELINES-28-TC01 Approve, PIPELINES-28-TC02 Reject) are each migrated as Ordered containers with 6 steps each (setup, create, start, action, validate state, verify result).

### Versions Tests use DescribeTable

The 2 Versions scenarios (PIPELINES-22-TC01 Server versions, PIPELINES-22-TC02 Client versions) are migrated using DescribeTable with 9 server component entries + 5 client verification steps.

### Pipelines Sanity Duplicates

The file `tests/pipelines/pipelines_sanity_test.go` contains 7 specs that duplicate tests from `pipelinerun_test.go`, `failure_test.go`, and `resolvers_test.go` with a `sanity` label. These are intentional label-filtered duplicates to enable `--label-filter=sanity` in the pipelines package. They share the same Polarion IDs but run in a sanity-specific context.

## Polarion ID Coverage

| Category | Count | Details |
|----------|-------|---------|
| Gauge unique Polarion IDs | 127 | Across all spec files |
| Ginkgo unique Polarion IDs | 126 | Across all test files |
| Missing in Ginkgo | 1 | PIPELINES-21-TC01 (subsumed into OLM) |
| Extra in Ginkgo | 0 | None |
| Scenario without Polarion ID | 1 | `hub-resolvers.spec` ("Test the functionality of hub resolvers") -- covered by PIPELINES-32-TC01 in Ginkgo |
| Duplicate Polarion ID in Gauge | 1 | PIPELINES-11-TC02 appears in both `rbac.spec` and `roles.spec` |

## Verification

To verify these counts:

```bash
# Count Gauge scenarios
grep -rh "^## " reference-repo/release-tests/specs/ | wc -l
# Expected: 129

# Count unique Gauge Polarion IDs
grep -roh 'PIPELINES-[0-9]*-TC[0-9]*' reference-repo/release-tests/specs/ | sort -u | wc -l
# Expected: 127

# Count unique Ginkgo Polarion IDs
grep -roh 'PIPELINES-[0-9]*-TC[0-9]*' tests/ --include="*_test.go" | sort -u | wc -l
# Expected: 126

# Find IDs in Gauge but not Ginkgo
comm -23 \
  <(grep -roh 'PIPELINES-[0-9]*-TC[0-9]*' reference-repo/release-tests/specs/ | sort -u) \
  <(grep -roh 'PIPELINES-[0-9]*-TC[0-9]*' tests/ --include="*_test.go" | sort -u)
# Expected: PIPELINES-21-TC01
```

---
*Generated: 2026-04-02*
*Migration: Gauge to Ginkgo v2 -- OpenShift Pipelines release tests*
