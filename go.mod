module github.com/openshift-pipelines/release-tests-ginkgo

go 1.26.3

replace (
	// tektoncd/operator v0.79.1 declares k8s.io/client-go v1.5.2 (legacy semver) which
	// Go MVS treats as higher than v0.x. Pin client-go to v0.35.1 to match
	// openshift/client-go (May 2026) and prevent the legacy semver from being selected.
	k8s.io/client-go => k8s.io/client-go v0.35.1
	// TODO(knative-migration): go.mod carries knative.dev/eventing v0.48.2 as an indirect
	// dep, but v0.48+ requires knative.dev/pkg/observability/* which doesn't exist in our
	// pinned knative.dev/pkg above. Pin to v0.45.0 which is compatible. Remove this pin
	// alongside the knative.dev/pkg pin above.
	knative.dev/eventing => knative.dev/eventing v0.45.0
	// TODO(knative-migration): Pin knative.dev/pkg to the last version that has both
	// knative.dev/pkg/metrics AND knative.dev/pkg/observability. We are in the process
	// of migrating to the newer knative which dropped knative.dev/pkg/metrics. Until
	// tektoncd/triggers, tektoncd/operator, and pipelines-as-code all migrate away from
	// knative.dev/pkg/metrics, this pin must stay. Remove once all upstream deps are updated.
	knative.dev/pkg => knative.dev/pkg v0.0.0-20250424013628-d5e74d29daa3
)

require (
	github.com/google/go-cmp v0.7.0
	github.com/onsi/ginkgo/v2 v2.28.3
	github.com/onsi/gomega v1.40.0
	github.com/openshift-pipelines/manual-approval-gate v0.8.0
	// TODO(knative-migration): Downgraded from v0.46.0. tektoncd/operator v0.79.1 pins
	// pipelines-as-code to v0.41.1 (via its own replace directive). Using v0.42.1+ causes
	// a SyncConfig() signature mismatch in operator's vendored code. Upgrade once operator
	// is updated to a version compatible with the newer knative.dev/pkg migration.
	github.com/openshift-pipelines/pipelines-as-code v0.41.1
	github.com/openshift/api v0.0.0-20260511191110-9b69e5fa27e9
	// Upgraded from v0.0.0-20240523113335 to May 2026 version which uses
	// sigs.k8s.io/structured-merge-diff/v6 and go.yaml.in/yaml/v3 (new canonical paths).
	github.com/openshift/client-go v0.0.0-20260512113608-deb4dc54551a
	github.com/operator-framework/api v0.43.0
	github.com/operator-framework/operator-lifecycle-manager v0.22.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.23.2
	github.com/prometheus/common v0.67.5
	github.com/tektoncd/operator v0.79.1
	// TODO(knative-migration): Downgraded from v1.11.1. v1.10+ requires the newer
	// knative.dev/pkg (without metrics package) which is incompatible with the current
	// knative.dev/pkg pin. MVS will resolve this to v1.9.2 via tektoncd/operator v0.79.1
	// and pipelines-as-code v0.42.1. Upgrade to v1.10+ once the knative.dev/pkg pin is lifted.
	github.com/tektoncd/pipeline v1.9.2
	github.com/tektoncd/triggers v0.35.0
	github.com/xanzy/go-gitlab v0.109.0
	gopkg.in/yaml.v2 v2.4.0
	gotest.tools/v3 v3.5.2
	k8s.io/api v0.35.4
	k8s.io/apimachinery v0.35.4
	k8s.io/client-go v1.5.2
	knative.dev/pkg v0.0.0-20260406140200-cb58ae50e894
)

require (
	cel.dev/expr v0.25.1 // indirect
	contrib.go.opencensus.io/exporter/ocagent v0.7.1-0.20230502190836-7399e0f8ee5e // indirect
	contrib.go.opencensus.io/exporter/prometheus v0.4.2 // indirect
	github.com/AlecAivazis/survey/v2 v2.3.7 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/blendle/zapdriver v1.3.1 // indirect
	github.com/census-instrumentation/opencensus-proto v0.4.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudevents/sdk-go/v2 v2.16.2 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/jsonreference v0.21.5 // indirect
	github.com/go-openapi/swag v0.25.5 // indirect
	github.com/go-openapi/swag/cmdutils v0.25.5 // indirect
	github.com/go-openapi/swag/conv v0.25.5 // indirect
	github.com/go-openapi/swag/fileutils v0.25.5 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/go-openapi/swag/jsonutils v0.25.5 // indirect
	github.com/go-openapi/swag/loading v0.25.5 // indirect
	github.com/go-openapi/swag/mangling v0.25.5 // indirect
	github.com/go-openapi/swag/netutils v0.25.5 // indirect
	github.com/go-openapi/swag/stringutils v0.25.5 // indirect
	github.com/go-openapi/swag/typeutils v0.25.5 // indirect
	github.com/go-openapi/swag/yamlutils v0.25.5 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/cel-go v0.28.1 // indirect
	github.com/google/gnostic-models v0.7.1 // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/hako/durafmt v0.0.0-20210608085754-5c1018a4e16b // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/kelseyhightower/envconfig v1.4.0 // indirect
	github.com/konflux-ci/tekton-kueue v0.0.0-20251231110853-e7a97991aa34 // indirect
	github.com/manifestival/manifestival v0.7.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mgutz/ansi v0.0.0-20200706080929-d51e80ef957d // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/openshift/apiserver-library-go v0.0.0-20230816171015-6bfafa975bfb // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/procfs v0.20.1 // indirect
	github.com/prometheus/statsd_exporter v0.28.0 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tektoncd/pruner v0.3.5 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20260312153236-7ab1446f8b90 // indirect
	golang.org/x/mod v0.35.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/term v0.43.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.44.0 // indirect
	golang.org/x/xerrors v0.0.0-20231012003039-104605ab7028 // indirect
	gomodules.xyz/jsonpatch/v2 v2.5.0 // indirect
	google.golang.org/api v0.269.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260406210006-6f92a3bedf2d // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/apiextensions-apiserver v0.35.4 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/kube-openapi v0.0.0-20260330154417-16be699c7b31 // indirect
	k8s.io/utils v0.0.0-20260319190234-28399d86e0b5 // indirect
	knative.dev/eventing v0.48.2 // indirect
	sigs.k8s.io/controller-runtime v0.23.3 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.2-0.20260122202528-d9cc6641c482 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
