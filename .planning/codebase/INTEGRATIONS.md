# External Integrations

**Analysis Date:** 2026-03-31

## APIs & External Services

**Kubernetes/OpenShift:**
- Kubernetes API - Core cluster operations
  - SDK/Client: `k8s.io/client-go` v0.29.6
  - Auth: Via kubeconfig file (`KUBECONFIG` env var or `~/.kube/config`)
  - Dynamic client: `k8s.io/client-go/dynamic`
  - Access: `pkg/clients/clients.go` - `NewClients()` creates all clientsets

**Tekton:**
- Tekton Pipeline API - Pipeline and task management
  - SDK/Client: `github.com/tektoncd/pipeline/pkg/client/clientset/versioned`
  - Connection: Same kubeconfig as Kubernetes
  - Clients: PipelineClient, TaskClient, TaskRunClient, PipelineRunClient
  - Location: `pkg/clients/clients.go`

- Tekton Triggers API - Event-driven pipelines
  - SDK/Client: `github.com/tektoncd/triggers/pkg/client/clientset/versioned`
  - Connection: Same kubeconfig as Kubernetes
  - Location: `pkg/clients/clients.go`

- Tekton Operator API - Tekton component lifecycle
  - SDK/Client: `github.com/tektoncd/operator/pkg/client/clientset/versioned`
  - Connection: Same kubeconfig as Kubernetes
  - Interfaces: TektonPipeline, TektonTrigger, TektonChains, TektonHub, TektonDashboard, TektonAddon, TektonConfig
  - Location: `pkg/clients/clients.go`

**OpenShift:**
- OpenShift Config API - Cluster configuration
  - SDK/Client: `github.com/openshift/client-go/config/clientset/versioned/typed/config/v1`
  - Components: ProxyConfig, ClusterVersion
  - Location: `pkg/clients/clients.go`

- OpenShift Route API - Route management
  - SDK/Client: `github.com/openshift/client-go/route/clientset/versioned/typed/route/v1`
  - Location: `pkg/clients/clients.go`

- OpenShift Console API - Console CLI downloads
  - SDK/Client: `github.com/openshift/client-go/console/clientset/versioned/typed/console/v1`
  - Used for: ConsoleCLIDownload resources, console plugin management
  - Location: `pkg/clients/clients.go`, `pkg/oc/oc.go` - `EnableConsolePlugin()`

**Pipelines as Code:**
- Pipelines as Code API - GitOps-style pipeline configuration
  - SDK/Client: `github.com/openshift-pipelines/pipelines-as-code/pkg/generated/clientset/versioned/typed/pipelinesascode/v1alpha1`
  - Connection: Same kubeconfig as Kubernetes
  - Location: `pkg/clients/clients.go`

**Manual Approval Gate:**
- Manual Approval Gate API - Human approval for pipelines
  - SDK/Client: `github.com/openshift-pipelines/manual-approval-gate/pkg/client/clientset/versioned/typed/approvaltask/v1alpha1`
  - Connection: Same kubeconfig as Kubernetes
  - Location: `pkg/clients/clients.go`

**GitLab:**
- GitLab API - Source control and webhook integration
  - SDK/Client: `github.com/xanzy/go-gitlab` v0.109.0
  - Auth: `GITLAB_TOKEN` environment variable
  - Webhook secret: `GITLAB_WEBHOOK_TOKEN` environment variable
  - Location: `pkg/pac/pac.go` - `InitGitLabClient()`
  - Purpose: Testing Pipelines as Code integration with GitLab

**Operator Lifecycle Manager:**
- OLM API - Operator installation and management
  - SDK/Client: `github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned`
  - Connection: Same kubeconfig as Kubernetes
  - Location: `pkg/clients/clients.go`

## Data Storage

**Databases:**
- None - Test suite does not manage databases directly
  - Note: Tekton Hub component uses PostgreSQL, but managed by operator

**File Storage:**
- Local filesystem only
  - Test templates: `template/` directory (relative to `pkg/config/`)
  - Temp files: `tmp/` directory (created on demand via `config.TempDir()`)

**Caching:**
- None

## Authentication & Identity

**Auth Provider:**
- Kubernetes/OpenShift native authentication
  - Implementation: Via kubeconfig file with cluster credentials
  - Service Account: Tests use `tekton-pipelines-controller` service account
  - Secret management: Generic secrets created via `oc create secret`

**Secret Creation:**
- GitLab webhook secrets: `pkg/oc/oc.go` - `CreateSecretForWebhook()`
- Git resolver secrets: `pkg/oc/oc.go` - `CreateSecretForGitResolver()`
- Chains image registry: `pkg/oc/oc.go` - `CreateChainsImageRegistrySecret()`
- Trigger secret tokens: `pkg/oc/oc.go` - `CreateSecretWithSecretToken()`

## Monitoring & Observability

**Error Tracking:**
- None - Uses Ginkgo test failure reporting

**Logs:**
- Standard Go logging via `log` package
- Structured logging available via `go.uber.org/zap`
- Kubernetes events: `pkg/oc/oc.go` - `VerifyKubernetesEventsForEventListener()`

**Metrics:**
- Prometheus exporters available (OpenCensus)
- Console quickstart for pipeline metrics configuration

## CI/CD & Deployment

**Hosting:**
- OpenShift cluster (test target environment)
  - Default namespace: `openshift-pipelines`
  - Test namespaces: Created dynamically per test

**CI Pipeline:**
- Not detected in repository
  - Tests appear to be run manually or via external CI

**Test Execution:**
- Run via: `go test` with Ginkgo framework
- Uses `oc` CLI for cluster operations
- Uses `kubectl` CLI for generic Kubernetes operations

## Environment Configuration

**Required env vars:**
- `KUBECONFIG` - Path to cluster kubeconfig
- `GITLAB_TOKEN` - GitLab API token (for PAC tests)
- `GITLAB_WEBHOOK_TOKEN` - GitLab webhook secret (for PAC tests)

**Optional env vars:**
- `CHANNEL` - Operator subscription channel
- `CATALOG_SOURCE` - Operator catalog source
- `SUBSCRIPTION_NAME` - Operator subscription name
- `INSTALL_PLAN` - Installation plan approval (Automatic/Manual)
- `CSV_VERSION` - Operator version
- `TKN_VERSION` - Tekton CLI version
- `KO_DOCKER_REPO` - Docker repository for test images
- `PAC_VERSION` - Pipelines as Code version
- `PIPELINE_VERSION` - Pipeline version
- `TRIGGERS_VERSION` - Triggers version
- `CHAINS_VERSION` - Chains version
- `OPERATOR_VERSION` - Operator version
- `OSP_VERSION` - OpenShift Pipelines version
- `TKN_CLIENT_VERSION` - TKN client version
- `ARCH` - Cluster architecture
- `IS_DISCONNECTED` - Disconnected cluster flag

**Secrets location:**
- Kubernetes secrets created in test namespaces
- GitLab webhook config: Secret `gitlab-webhook-config` in test namespace

## Webhooks & Callbacks

**Incoming:**
- Not applicable - Test suite does not expose webhooks
  - Note: Tests verify webhook functionality in Tekton components

**Outgoing:**
- GitLab webhooks - Configured during Pipelines as Code tests
  - Configuration: `pkg/pac/pac.go`
  - Secret: `gitlab-webhook-config`

## Command-Line Integrations

**CLI Tools Used:**
- `oc` - OpenShift CLI (primary cluster interaction)
  - Location: `pkg/oc/oc.go` - Wrapper functions for oc commands
  - Execution: `pkg/cmd/cmd.go` - Command runner with timeout handling

- `kubectl` - Kubernetes CLI
  - Used for generic Kubernetes operations
  - Execution: Via same command runner

- `opc` - OpenShift Pipelines CLI
  - Location: `pkg/opc/opc.go`
  - Commands: version, pipeline start, pac info, hub search, pipelinerun list
  - Purpose: Testing OpenShift Pipelines CLI functionality

- `tkn` - Tekton CLI
  - Downloaded from cluster: `pkg/opc/opc.go` - `DownloadCLIFromCluster()`
  - Purpose: CLI version validation

- `tkn-pac` - Pipelines as Code CLI
  - Downloaded from cluster: `pkg/opc/opc.go` - `DownloadCLIFromCluster()`
  - Purpose: CLI version validation

- `curl` - HTTP client
  - Used for: Downloading CLI binaries from cluster

- `tar` - Archive extraction
  - Used for: Extracting downloaded CLI binaries

- `bash` - Shell commands
  - Used for: Complex operations like secret copying with jq

- `jq` - JSON processing
  - Used for: Secret manipulation in `pkg/oc/oc.go` - `CopySecret()`

## Client Configuration

**Kubernetes Client:**
- QPS: 100
- Burst: 200
- Location: `pkg/clients/clients.go` lines 69-70
- Rationale: High limits for polling operations

**Timeouts:**
- API operations: 10 minutes (`config.APITimeout`)
- API retry interval: 5 seconds (`config.APIRetry`)
- CLI commands: 90 seconds (`config.CLITimeout`)
- Resource operations: 60 seconds (`config.ResourceTimeout`)
- Delete operations: 300 seconds (increased for Tekton Results finalizers)
- Location: `pkg/config/config.go`

---

*Integration audit: 2026-03-31*
