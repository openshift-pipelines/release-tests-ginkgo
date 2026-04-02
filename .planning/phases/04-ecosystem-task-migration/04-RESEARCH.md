# Phase 4 Research: Ecosystem Task Migration

**Researched:** 2026-03-31
**Phase:** 04-ecosystem-task-migration
**Requirements:** ECO-01, ECO-02

## Source Analysis

### Test Inventory (36 Tests Across 3 Spec Files)

**ecosystem.spec (PIPELINES-29) -- 21 tests:**

| TC | Name | Tags | Pattern | Resources Created | Special Steps |
|----|------|------|---------|-------------------|---------------|
| TC01 | buildah pipelinerun | e2e, sanity | simple-create-verify | pipeline + pvc + pipelinerun | -- |
| TC02 | buildah disconnected | disconnected-e2e | simple-create-verify | pipeline + pvc + pipelinerun | -- |
| TC03 | git-cli pipelinerun | e2e | simple-create-verify | pipeline + pvc + pipelinerun | -- |
| TC04 | git-cli read private repo | e2e | secret-link-verify | pipeline + pvc + secret + pipelinerun | Link secret to SA |
| TC05 | git-cli read private repo (diff SA) | e2e | secret-link-verify | pipeline + pvc + secret + SA + rolebinding + pipelinerun | Link secret to custom SA |
| TC06 | git-clone read private repo | e2e, sanity | secret-link-verify | pipeline + pvc + secret + pipelinerun | Link secret to SA |
| TC07 | git-clone read private repo (diff SA) | e2e | secret-link-verify | pipeline + pvc + secret + SA + rolebinding + pipelinerun | Link secret to custom SA |
| TC08 | openshift-client pipelinerun | e2e | simple-create-verify | pipelinerun only | -- |
| TC09 | skopeo-copy pipelinerun | e2e | simple-create-verify | pipelinerun only | -- |
| TC10 | tkn pipelinerun | e2e | simple-create-verify | pipelinerun only | -- |
| TC11 | tkn pac pipelinerun | e2e | simple-create-verify + log-check | pipelinerun only | Verify tkn-pac version from logs |
| TC12 | tkn version pipelinerun | e2e | simple-create-verify + log-check | pipelinerun only | Verify tkn version from logs |
| TC13 | maven pipelinerun | e2e | simple-create-verify | pipeline + pvc + configmap + pipelinerun | -- |
| TC14 | step action resolvers | e2e, sanity | simple-create-verify | task + pvc + pipelinerun | -- |
| TC15 | cache-upload stepaction | e2e, sanity | cache-pipeline-start | pipeline + pvc | Start pipeline via opc, validate logs, run twice |
| TC16 | cache upload revision change | e2e | cache-pipeline-start | pipeline + pvc | Start pipeline via opc, validate logs, change revision |
| TC17 | helm-upgrade-from-repo | e2e | simple-create-verify + wait-deploy | pipeline + pvc + pipelinerun | Wait for deployment |
| TC18 | helm-upgrade-from-source | e2e | simple-create-verify + wait-deploy | pipeline + pvc + pipelinerun | Wait for deployment |
| TC19 | pull-request pipelinerun | e2e | secret-copy-verify | pipeline + pvc + pipelinerun | Copy secret from openshift-pipelines NS |
| TC20 | buildah-ns pipelinerun | e2e, sanity | simple-create-verify | pipeline + pvc + pipelinerun | -- |
| TC21 | opc task pipelinerun | e2e, sanity | simple-create-verify | pipeline + pipelinerun | -- |

**ecosystem-s2i.spec (PIPELINES-33) -- 9 tests:**

| TC | Name | Tags | Pattern | Special Steps |
|----|------|------|---------|---------------|
| TC01 | S2I nodejs | e2e, sanity | s2i-full-flow | Create resources, verify pipelinerun, expose DC, get route, validate route |
| TC02 | S2I dotnet | e2e, skip_linux/ppc64le | s2i-imagestream-start | Get imagestream tags, start pipeline per version via opc |
| TC03 | S2I golang | e2e, sanity | s2i-imagestream-start | Get imagestream tags, start pipeline per version via opc |
| TC04 | S2I java | ecosystem | s2i-imagestream-start | Get imagestream tags, start pipeline per version via opc |
| TC05 | S2I nodejs | e2e | s2i-imagestream-start | Get imagestream tags, start pipeline per version via opc |
| TC06 | S2I perl | e2e | s2i-imagestream-start | Get imagestream tags, start pipeline per version via opc |
| TC07 | S2I php | e2e | s2i-imagestream-start | Get imagestream tags, start pipeline per version via opc |
| TC08 | S2I python | e2e | s2i-imagestream-start | Get imagestream tags, start pipeline per version via opc |
| TC09 | S2I ruby | e2e | s2i-imagestream-start | Get imagestream tags, start pipeline per version via opc |

**ecosystem-multiarch.spec (PIPELINES-32) -- 6 tests:**

| TC | Name | Tags | Pattern | Special Steps |
|----|------|------|---------|---------------|
| TC01 | jib-maven | linux/amd64, sanity | simple-create-verify | -- |
| TC02 | jib-maven P&Z | linux/ppc64le, linux/s390x, linux/arm64, sanity | simple-create-verify | -- |
| TC03 | kn-apply | e2e, linux/amd64 | simple-create-verify | -- |
| TC04 | kn-apply p&z | e2e, linux/ppc64le, linux/s390x | simple-create-verify | -- |
| TC05 | kn | e2e, linux/amd64 | simple-create-verify | -- |
| TC06 | kn p&z | e2e, linux/ppc64le, linux/s390x | simple-create-verify | -- |

### Migration Pattern Classification

The 36 tests fall into 6 distinct patterns:

1. **Simple Create-Verify (16 tests):** Create YAML resources via `oc create`, verify pipelinerun status. Most common. Direct DescribeTable candidate.
   - TC01, TC02, TC03, TC08, TC09, TC10, TC13, TC14, TC20, TC21 (PIPELINES-29)
   - TC01, TC02, TC03, TC04, TC05, TC06 (PIPELINES-32)

2. **Secret-Link-Verify (4 tests):** Create resources + secret, link secret to SA, create pipelinerun, verify. Requires extra setup step before pipelinerun creation.
   - TC04, TC05, TC06, TC07 (PIPELINES-29)

3. **Secret-Copy-Verify (1 test):** Copy secret from another namespace, then create-verify.
   - TC19 (PIPELINES-29)

4. **Create-Verify + Log Check (2 tests):** Same as simple create-verify but also verify specific content in pipelinerun logs.
   - TC11, TC12 (PIPELINES-29)

5. **Create-Verify + Wait Deploy (2 tests):** Same as simple create-verify but also wait for a deployment to become ready.
   - TC17, TC18 (PIPELINES-29)

6. **S2I ImageStream Start (8 tests):** Create pipeline + PVC, get imagestream tags from openshift namespace, start pipeline per version via `opc` CLI, validate each run succeeds.
   - TC02-TC09 (PIPELINES-33)

7. **S2I Full Flow (1 test):** Create resources, verify pipelinerun, expose deployment config, get route, validate route response.
   - TC01 (PIPELINES-33)

8. **Cache Pipeline Start (2 tests):** Create pipeline + PVC, start pipeline via `opc` with specific params, validate logs contain expected content, run twice with same/different params.
   - TC15, TC16 (PIPELINES-29)

### Gauge Step-to-Ginkgo Mapping

| Gauge Step | Ginkgo Equivalent | Helper Used |
|------------|-------------------|-------------|
| `Create <table>` | `oc.Create(resource, namespace)` in a loop | `pkg/oc` |
| `Verify pipelinerun <table>` | `pipelines.ValidatePipelineRun(clients, name, status, ns)` | `pkg/pipelines` |
| `Link secret <secret> to service account <sa>` | `oc.LinkSecretToSA(secret, sa, ns)` | `pkg/oc` |
| `Copy secret from <ns> to autogenerated namespace` | `oc.CopySecret(name, sourceNS, targetNS)` | `pkg/oc` |
| `Get tags of the imagestream <is> from namespace <ns>` | `openshift.GetImageStreamTags(clients, ns, is)` | `pkg/openshift` |
| `Start and verify pipeline <name> with param...` | `opc.StartPipeline(...)` + `pipelines.ValidatePipelineRun(...)` | `pkg/opc`, `pkg/pipelines` |
| `Verify <binary> version from the pipelinerun logs` | `pipelines.CheckLogVersion(clients, binary, ns)` | `pkg/pipelines` |
| `Wait for <deployment> deployment` | `k8s.ValidateDeployments(clients, ns, name)` | `pkg/k8s` |
| `Expose Deployment config <name> on port <port>` | `triggers.ExposeDC(name, port, ns)` (in triggers pkg) | `pkg/triggers` |
| `Get route url of the route <name>` | `triggers.GetRouteURL(name, ns)` | `pkg/triggers` |
| `Validate that route URL contains <text>` | `cmd.MustSuccedIncreasedTimeout(...)` + string check | `pkg/cmd` |
| `Validate pipelinerun stored in variable <var> with task <task> logs contains <text>` | `cmd.MustSucceed("oc", "logs", ...)` + string check | `pkg/cmd` |
| `Start the <pipeline> pipeline with params...` | `opc.StartPipeline(...)` + `opc.GetOpcPrList(...)` | `pkg/opc` |

### Existing Infrastructure

**Suite entry point exists:** `tests/ecosystem/suite_test.go` with `BeforeSuite` initializing `sharedClients` and `Label("ecosystem")`.

**Pattern reference exists:** `tests/ecosystem/ecosystem_patterns_test.go` demonstrates DescribeTable with Entry, tree-construction-time warnings, and DeferCleanup. This file should be REPLACED by the real test code.

**Helper packages available** (all in `pkg/`):
- `oc` -- `Create`, `CreateNewProject`, `DeleteProjectIgnoreErors`, `LinkSecretToSA`, `CopySecret`, `SecretExists`
- `pipelines` -- `ValidatePipelineRun`, `CheckLogVersion`
- `opc` -- `StartPipeline`, `GetOpcPrList`
- `openshift` -- `GetImageStreamTags`
- `k8s` -- `ValidateDeployments`, `NewClientSet`
- `cmd` -- `MustSucceed`, `MustSuccedIncreasedTimeout`
- `config` -- `Flags`, `APIRetry`, `APITimeout`, `TargetNamespace`
- `clients` -- `Clients`, `NewClients`

### Key Design Decisions

1. **DescribeTable vs individual Describe/It:** The 16 simple-create-verify tests and 6 multiarch tests are direct DescribeTable candidates. The more complex patterns (S2I, secret-link, cache) need individual `It` or `Ordered` blocks because they have multi-step setup logic that varies per test.

2. **Namespace lifecycle:** Each test creates a fresh namespace in BeforeEach. The Gauge code creates namespace in `BeforeScenario` hook, which maps directly to `BeforeEach` or per-Entry setup in the DescribeTable body function.

3. **Tree-construction-time parameter safety:** Entry parameters must be string/int/bool literals. Dynamic values (namespace name, imagestream tags) must be computed inside the table body function, not passed as Entry args. This is the ECO-02 requirement.

4. **Labels mapping:** Gauge tags map directly to Ginkgo Labels. Architecture-specific tests (linux/amd64, linux/ppc64le) should use Skip based on `config.Flags.ClusterArch`, not separate DescribeTable entries, since DescribeTable entries generate separate JUnit entries regardless of skip.

5. **Polarion test case IDs:** Each Entry description must include the Polarion ID, e.g., `Entry("buildah pipelinerun: PIPELINES-29-TC01", ...)`. This generates a unique JUnit testcase entry for Polarion mapping.

6. **store package elimination:** Gauge tests pass data via `store.PutScenarioData`/`store.GetScenarioData`. In Ginkgo, data flows through closure-scoped variables within the table body function or It block. No global store needed.

### Migration Grouping for Plans

**Plan 1 (Wave 1): Simple and multiarch ecosystem tests via DescribeTable**
- 16 simple-create-verify tests (PIPELINES-29: TC01-TC03, TC08-TC10, TC13-TC14, TC20-TC21; PIPELINES-32: TC01-TC06)
- 2 create-verify + log-check tests (PIPELINES-29: TC11-TC12)
- 2 create-verify + wait-deploy tests (PIPELINES-29: TC17-TC18)
- 1 secret-copy-verify test (PIPELINES-29: TC19)
- Total: ~21 tests
- All follow the "create resources, optionally do extra setup, verify pipelinerun" pattern
- Can be grouped into 2-3 DescribeTables by variant

**Plan 2 (Wave 1, parallel with Plan 1): Complex ecosystem tests**
- 4 secret-link-verify tests (PIPELINES-29: TC04-TC07)
- 2 cache pipeline tests (PIPELINES-29: TC15-TC16)
- 9 S2I tests (PIPELINES-33: TC01-TC09)
- Total: ~15 tests
- These require more complex setup (secret linking, imagestream tag retrieval, opc pipeline start, route validation)

## Sources

- `reference-repo/release-tests/specs/ecosystem/ecosystem.spec` -- 21 Gauge scenarios
- `reference-repo/release-tests/specs/ecosystem/ecosystem-s2i.spec` -- 9 Gauge scenarios
- `reference-repo/release-tests/specs/ecosystem/ecosystem-multiarch.spec` -- 6 Gauge scenarios
- `reference-repo/release-tests/steps/cli/oc.go` -- Create, Link secret, Copy secret step implementations
- `reference-repo/release-tests/steps/cli/opc.go` -- Start pipeline, verify S2I step implementations
- `reference-repo/release-tests/steps/pipeline/pipeline.go` -- Verify pipelinerun step
- `reference-repo/release-tests/steps/openshift/openshift.go` -- Get imagestream tags step
- `reference-repo/release-tests/steps/utility/utility.go` -- Wait deployment, validate route steps
- `reference-repo/release-tests/steps/hooks.go` -- BeforeScenario/AfterScenario lifecycle
- `reference-repo/release-tests/pkg/pipelines/pipelines.go` -- ValidatePipelineRun, CheckLogVersion
- `tests/ecosystem/suite_test.go` -- Existing Ginkgo suite entry point
- `tests/ecosystem/ecosystem_patterns_test.go` -- Existing DescribeTable pattern reference

---

*Research completed: 2026-03-31*
