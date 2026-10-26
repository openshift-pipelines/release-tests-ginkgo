#!/usr/bin/env bash
set -euo pipefail
umask 077
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
hub=$1 run_dir=$2
shift 2
[[ $# == 1 || $# == 2 ]] || { printf 'Expected one or two spoke names\n' >&2; exit 2; }
spoke_count=$#
h=(kubectl --kubeconfig "$hub" --request-timeout=60s)
configs=("$hub")
for spoke_name in "$@"; do configs+=("$run_dir/$spoke_name.kubeconfig"); done

for kubeconfig in "${configs[@]}"; do
  kubectl --kubeconfig "$kubeconfig" --request-timeout=60s apply -f "$ROOT/testdata/kind-multicluster/queues.yaml"
  if [[ $spoke_count == 2 ]]; then
    kubectl --kubeconfig "$kubeconfig" --request-timeout=60s patch clusterqueue release-multicluster \
      --type=merge --patch-file "$ROOT/testdata/kind-multicluster/quota-ten.yaml"
  fi
done
worker_names=()
i=0
for spoke_name in "$@"; do
  i=$((i+1))
  worker=release-spoke
  if [[ $spoke_count == 2 ]]; then worker="$worker-$i"; fi
  worker_names+=("$worker")
  s=(kubectl --kubeconfig "$run_dir/$spoke_name.kubeconfig" --request-timeout=60s)
  "${s[@]}" apply -f "$ROOT/testdata/kind-multicluster/worker-rbac.yaml"
  "${s[@]}" create token release-worker -n release-multicluster --duration=2h > "$run_dir/$worker.token"
  # Internal endpoints are reachable from hub containers, including on macOS.
  # Replace all admin credentials, not merely the current user's token.
  kind get kubeconfig --name "$spoke_name" --internal > "$run_dir/$worker-internal.kubeconfig"
  kubectl --kubeconfig "$run_dir/$worker-internal.kubeconfig" config view --raw -o json |
    jq --rawfile token "$run_dir/$worker.token" \
      '.users |= map(.user = {"token": ($token | rtrimstr("\n"))})' > "$run_dir/$worker.kubeconfig"
  "${h[@]}" create secret generic "$worker" -n kueue-system --from-file=kubeconfig="$run_dir/$worker.kubeconfig"
  "${h[@]}" apply -f - <<EOF
apiVersion: kueue.x-k8s.io/v1beta2
kind: MultiKueueCluster
metadata:
  name: $worker
spec:
  clusterSource:
    kubeConfig:
      locationType: Secret
      location: $worker
EOF
done
printf '%s\n' "${worker_names[@]}" | jq -R . | jq -s \
  '{apiVersion:"kueue.x-k8s.io/v1beta2",kind:"MultiKueueConfig",
    metadata:{name:"release-multicluster"},spec:{clusters:.}}' | "${h[@]}" apply -f -
"${h[@]}" apply -f "$ROOT/testdata/kind-multicluster/hub.yaml"
"${h[@]}" patch clusterqueue release-multicluster --type=merge \
  -p '{"spec":{"admissionChecksStrategy":{"admissionChecks":[{"name":"release-multicluster"}]}}}'

for kubeconfig in "${configs[@]}"; do
  role=Spoke override=false
  if [[ $kubeconfig == "$hub" ]]; then role=Hub override=true; fi
  kubectl --kubeconfig "$kubeconfig" --request-timeout=60s apply -f - <<EOF
apiVersion: operator.tekton.dev/v1alpha1
kind: TektonScheduler
metadata:
  name: tektonscheduler
spec:
  targetNamespace: tekton-pipelines
  disabled: false
  multi-cluster-disabled: false
  multi-cluster-role: $role
  config.yaml:
    queueName: release-multicluster
    multiKueueOverride: $override
EOF
  kubectl --kubeconfig "$kubeconfig" --request-timeout=60s wait \
    --for=condition=Ready tektonscheduler/tektonscheduler --timeout=10m
  [[ $(kubectl --kubeconfig "$kubeconfig" --request-timeout=60s get tektonscheduler/tektonscheduler \
    -o jsonpath='{.status.version}') == v0.3.1 ]]
  kubectl --kubeconfig "$kubeconfig" --request-timeout=60s wait \
    --for=jsonpath='{.webhooks[0].clientConfig.caBundle}' \
    mutatingwebhookconfiguration/tekton-kueue-mutating-webhook-configuration --timeout=5m
  kubectl --kubeconfig "$kubeconfig" --request-timeout=60s wait \
    --for=condition=Active clusterqueue/release-multicluster --timeout=5m
done
for worker in "${worker_names[@]}"; do
  "${h[@]}" wait --for=condition=Active "multikueuecluster/$worker" --timeout=5m
done
"${h[@]}" wait --for=condition=Active admissioncheck/release-multicluster --timeout=5m
"${h[@]}" wait --for=condition=Ready certificate/release-proxy -n tekton-pipelines --timeout=5m
"${h[@]}" wait --for=condition=Ready tektonmulticlusterproxyaae/multicluster-proxy-aae --timeout=5m
[[ $("${h[@]}" get tektonmulticlusterproxyaae/multicluster-proxy-aae -o jsonpath='{.status.version}') == v0.1.1 ]]
