# Technology Stack

**Analysis Date:** 2026-03-31

## Languages

**Primary:**
- Go 1.23.4 - All source code and test implementations
- Toolchain: go1.23.6

**Secondary:**
- None

## Runtime

**Environment:**
- Go 1.23.4 (minimum)
- Toolchain: go1.23.6

**Package Manager:**
- Go modules
- Lockfile: `go.sum` present

## Frameworks

**Core:**
- Ginkgo v2.13.0 - BDD testing framework (migrated from Gauge)
- Gomega v1.29.0 - Matcher/assertion library for Ginkgo

**Testing:**
- Ginkgo v2.13.0 - Primary test framework
- Gomega v1.29.0 - Assertion library
- gotest.tools/v3 v3.5.2 - Additional testing utilities (icmd for command execution)

**Build/Dev:**
- None detected (using standard Go toolchain)

## Key Dependencies

**Critical:**
- `k8s.io/client-go` v0.29.6 (replaced) - Kubernetes API client
- `k8s.io/apimachinery` v0.29.6 (replaced) - Kubernetes API machinery
- `github.com/tektoncd/pipeline` v0.68.0 - Tekton Pipeline APIs
- `github.com/tektoncd/operator` v0.75.0 - Tekton Operator APIs
- `github.com/tektoncd/triggers` v0.31.0 - Tekton Triggers APIs

**OpenShift:**
- `github.com/openshift/client-go` v0.0.0-20240523113335-452272e0496d - OpenShift API client
- `github.com/openshift-pipelines/pipelines-as-code` v0.27.2 - Pipelines as Code integration
- `github.com/openshift-pipelines/manual-approval-gate` v0.2.2 - Manual approval gate integration
- `github.com/openshift-pipelines/release-tests` v0.0.0-20250509122825-94ba39df192b - Shared test utilities from original Gauge tests

**Operator Framework:**
- `github.com/operator-framework/operator-lifecycle-manager` v0.22.0 - OLM integration for operator management

**External Services:**
- `github.com/xanzy/go-gitlab` v0.109.0 - GitLab API client for Pipelines as Code testing

**Command Execution:**
- `gotest.tools/v3/icmd` v3.5.2 - Command execution wrapper used throughout tests

**Observability:**
- `go.uber.org/zap` v1.27.0 - Structured logging
- `github.com/sirupsen/logrus` v1.9.3 - Alternative logging
- `contrib.go.opencensus.io/exporter/prometheus` v0.4.2 - Prometheus exporter
- `go.opencensus.io` v0.24.0 - OpenCensus observability

## Configuration

**Environment:**
- Configuration via environment variables (see `pkg/config/config.go`)
- Key environment variables:
  - `KUBECONFIG` - Path to kubeconfig file
  - `CHANNEL` - Operator subscription channel
  - `CATALOG_SOURCE` - Operator catalog source
  - `CSV_VERSION` - Cluster Service Version
  - `TKN_VERSION` - Tekton CLI version
  - `GITLAB_TOKEN` - GitLab API token (for PAC tests)
  - `GITLAB_WEBHOOK_TOKEN` - GitLab webhook secret (for PAC tests)
  - `KO_DOCKER_REPO` - Docker repository for test images
  - `PAC_VERSION`, `PIPELINE_VERSION`, `TRIGGERS_VERSION`, `CHAINS_VERSION`, `OPERATOR_VERSION` - Component versions
  - `ARCH` - Cluster architecture
  - `IS_DISCONNECTED` - Disconnected cluster flag

**Build:**
- `go.mod` - Go module dependencies
- No Makefile or build configuration detected (uses standard `go test` workflow)

## Platform Requirements

**Development:**
- Go 1.23.4 or later
- Access to OpenShift/Kubernetes cluster
- `oc` CLI installed
- `kubectl` installed
- Kubeconfig with appropriate cluster access

**Production:**
- OpenShift Pipelines environment
- Kubernetes cluster with Tekton Operator installed
- Target namespace: `openshift-pipelines` (default)

---

*Stack analysis: 2026-03-31*
