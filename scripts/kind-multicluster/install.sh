#!/usr/bin/env bash
# Sourced by kind-multicluster.sh after its cleanup trap is registered.

download_artifacts() {
  local name url digest
  mkdir "$RUN_DIR/artifacts"
  while read -r name digest url; do
    [[ -n "$name" && $name != \#* ]] || continue
    curl --fail --location --silent --show-error --connect-timeout 15 --max-time 180 \
      "$url" -o "$RUN_DIR/artifacts/$name.yaml"
    if command -v sha256sum >/dev/null; then
      printf '%s  %s\n' "$digest" "$RUN_DIR/artifacts/$name.yaml" | sha256sum --check
    else
      printf '%s  %s\n' "$digest" "$RUN_DIR/artifacts/$name.yaml" | shasum -a 256 --check
    fi
  done < "$ROOT/testdata/kind-multicluster/artifacts.lock"
  cp "$ROOT/testdata/kind-multicluster/kustomization.yaml" "$RUN_DIR/artifacts/"
  cp "$ROOT/testdata/kind-multicluster/kueue-config.yaml" "$RUN_DIR/artifacts/"
}

install_components() {
  local kubeconfig=$1
  local -a k=(kubectl --kubeconfig "$kubeconfig" --request-timeout=60s)
  # Disable autoinstall in the rendered operator manifest before its pods start.
  "${k[@]}" apply --server-side -k "$RUN_DIR/artifacts"
  "${k[@]}" wait --for=condition=Established --timeout=2m \
    crd/tektonpipelines.operator.tekton.dev crd/tektonschedulers.operator.tekton.dev \
    crd/certificates.cert-manager.io crd/multikueueclusters.kueue.x-k8s.io
  "${k[@]}" rollout status -n cert-manager deployment/cert-manager --timeout=5m
  "${k[@]}" rollout status -n cert-manager deployment/cert-manager-cainjector --timeout=5m
  "${k[@]}" rollout status -n cert-manager deployment/cert-manager-webhook --timeout=5m
  "${k[@]}" rollout status -n tekton-operator deployment/tekton-operator --timeout=5m
  "${k[@]}" rollout status -n tekton-operator deployment/tekton-operator-webhook --timeout=5m
  "${k[@]}" apply -f "$ROOT/testdata/kind-multicluster/pipeline.yaml"
  "${k[@]}" wait --for=condition=Ready tektonpipeline/pipeline --timeout=10m
  [[ $("${k[@]}" get tektonpipeline/pipeline -o jsonpath='{.status.version}') == v1.12.0 ]]
  "${k[@]}" rollout status -n kueue-system deployment/kueue-controller-manager --timeout=5m
}
