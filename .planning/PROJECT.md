# OpenShift Pipelines Release Tests — Ginkgo Migration

## What This Is

A migration of the OpenShift Pipelines release test suite from Gauge to Ginkgo (Go BDD framework). The original tests live in `openshift-pipelines/release-tests` (Gauge-based); this repo (`release-tests-ginkgo`) is the migration target. ~110 OpenShift-specific tests will be ported; ~25 tests already covered by upstream Tekton are dropped.

## Core Value

Every OpenShift-specific release test that currently passes in Gauge must pass identically in Ginkgo, with the same cluster coverage and JUnit XML output for Polarion.

## Requirements

### Validated

- ✓ Kubernetes/OpenShift client initialization (`pkg/clients`) — existing
- ✓ Thread-safe test scenario data store (`pkg/store`) — existing
- ✓ CLI command execution with timeouts and assertions (`pkg/cmd`) — existing
- ✓ OpenShift CLI operations — create/delete resources, secrets, namespaces (`pkg/oc`) — existing
- ✓ OpenShift Pipelines CLI operations — version checks, pipeline runs, hub search (`pkg/opc`) — existing
- ✓ PAC helper operations — GitLab client, webhook secrets (`pkg/pac`) — existing
- ✓ Configuration constants — timeouts, namespaces, deployment names, env flags (`pkg/config`) — existing

### Active

- [ ] Clone original release-tests repo as local reference source
- [ ] Ginkgo suite entry points and test directory structure (`test/` organized by area)
- [ ] Migrate sanity tests (~9 tests)
- [ ] Migrate ecosystem task tests (~36 tests — buildah, s2i, git-clone, etc.)
- [ ] Migrate triggers tests (~23 tests — EventListeners, TriggerBindings, etc.)
- [ ] Migrate operator tests (~33 tests — auto-install, auto-prune, addon, RBAC, etc.)
- [ ] Migrate pipelines core tests (~16 tests — PipelineRuns, TaskRuns, workspaces, etc.)
- [ ] Migrate PAC tests (~7 tests — expand existing scaffolding)
- [ ] Migrate chains tests (~2 tests)
- [ ] Migrate results tests (~2 tests)
- [ ] Migrate manual approval gate tests (~2 tests)
- [ ] Migrate metrics test (~1 test)
- [ ] Migrate versions/sanity tests (~2 tests)
- [ ] Migrate OLM/install tests (~3 tests)
- [ ] Migrate console icon test (~1 test)
- [ ] Ginkgo label system (replacing Gauge tags: sanity, smoke, e2e, disconnected)
- [ ] JUnit XML reporting compatible with Polarion uploader
- [ ] CI pipeline configuration (Docker image, test commands)
- [ ] Parity validation — same pass/fail results as Gauge on same cluster

### Out of Scope

- Upstream Tekton tests (~25) — already covered by Tekton CI in Konflux on OpenShift
- Gauge framework maintenance — this repo is Ginkgo-only
- Changes to `pkg/` helper logic — only thin Gauge connector layer (`steps/`) is being replaced
- Mobile/UI testing — these are integration/API tests against OpenShift clusters

## Context

- **Source repo**: `openshift-pipelines/release-tests` (Gauge-based, 135 tests across 31 files)
- **Migration plan**: PR #739 on source repo defines 4-phase, 10-week rollout
- **Existing scaffolding**: This repo already has `pkg/` helpers ported and one partial PAC test
- **Test inventory**: 128 tests by area count (36 ecosystem + 23 triggers + 33 operator + 16 pipelines + 7 PAC + 2 chains + 2 results + 2 MAG + 1 metrics + 2 versions + 3 OLM + 1 console)
- **Gauge→Ginkgo mapping**: spec headers → `Describe`, scenarios → `It`, tags → `Label`, data tables → `DescribeTable`/`Entry`, concepts → shared Go helpers
- **Source files will be cloned locally** for reference during migration

## Constraints

- **Framework**: Ginkgo v2 + Gomega — already in go.mod
- **Go version**: 1.23.4+ (toolchain 1.23.6)
- **Cluster**: Tests run against live OpenShift clusters with Tekton Operator installed
- **CLI**: `ginkgo run` preferred over `go test` for label filtering and parallelization
- **Reporting**: JUnit XML output must remain compatible with existing Polarion uploader
- **Module path**: Currently `github.com/srivickynesh/release-tests-ginkgo` — may need updating to `github.com/openshift-pipelines/release-tests-ginkgo`

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Separate repo for migration | Allows side-by-side validation without breaking existing Gauge CI | — Pending |
| Drop ~25 upstream-covered tests | Upstream Tekton CI already runs these in Konflux on OpenShift | — Pending |
| Clone source repo locally | Direct file access for migration reference | — Pending |
| Follow PR #739 phased rollout | Proven plan: sanity first, then ecosystem/operator/core, cleanup last | — Pending |
| Use `ginkgo run` not `go test` | Better label filtering, parallelization, and reporting control | — Pending |

---
*Last updated: 2026-03-31 after initialization*
