#!/usr/bin/env bash
# Run from any directory. All kubeconfigs and clusters belong to this invocation.
set -euo pipefail
umask 077

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
if [[ ${1:-} == --help ]]; then
  printf 'Usage: bash scripts/kind-multicluster.sh\nRequires Kind v0.31.0, Docker, kubectl, curl, jq, Go, and sha256sum or shasum.\n'
  exit 0
fi
[[ $# == 0 ]] || { printf 'Unexpected arguments\n' >&2; exit 2; }
for tool in kind docker kubectl curl jq go; do
  command -v "$tool" >/dev/null || { printf 'Missing tool: %s\n' "$tool" >&2; exit 2; }
done
[[ $(kind version) == "kind v0.31.0 "* ]] || { printf 'Kind v0.31.0 is required\n' >&2; exit 2; }
docker info >/dev/null
export KIND_EXPERIMENTAL_PROVIDER=docker
RUN_DIR=$(mktemp -d "$ROOT/.kind-multicluster.XXXXXXXX")
export KUBECONFIG="$RUN_DIR/unused.kubeconfig"
CLUSTERS=()
cleanup() {
  local status=$? failed=0 cluster
  trap - EXIT INT TERM
  set +e
  if (( ${#CLUSTERS[@]} > 0 )); then
    for cluster in "${CLUSTERS[@]}"; do
      kind delete cluster --name "$cluster" --kubeconfig "$RUN_DIR/$cluster.kubeconfig" || failed=1
    done
  fi
  rm -rf "$RUN_DIR"
  if [[ $status == 0 && $failed != 0 ]]; then status=1; fi
  exit "$status"
}
# Registered before downloads, compilation, and the first provisioning attempt.
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# shellcheck source=scripts/kind-multicluster/install.sh
source "$ROOT/scripts/kind-multicluster/install.sh"
download_artifacts
cd "$ROOT"
go test -c -o "$RUN_DIR/kind.test" ./tests/kindmulticluster

suffix=$(basename "$RUN_DIR" | tr '[:upper:]' '[:lower:]')
suffix=${suffix#.kind-multicluster.}
HUB="rtg-$suffix-hub"
SPOKE="rtg-$suffix-spoke"
existing=$(kind get clusters)
for cluster in "$HUB" "$SPOKE"; do
  if printf '%s\n' "$existing" | grep -Fxq "$cluster"; then
    printf 'Refusing existing cluster %s\n' "$cluster" >&2
    exit 1
  fi
done
for cluster in "$HUB" "$SPOKE"; do
  # Include partial Kind creations in cleanup, but never an existing cluster.
  CLUSTERS+=("$cluster")
  kind create cluster --name "$cluster" --kubeconfig "$RUN_DIR/$cluster.kubeconfig" \
    --image kindest/node:v1.35.0@sha256:452d707d4862f52530247495d180205e029056831160e22870e37e3f6c1ac31f \
    --wait 5m
  install_components "$RUN_DIR/$cluster.kubeconfig"
done

bash "$ROOT/scripts/kind-multicluster/configure.sh" \
  "$RUN_DIR/$HUB.kubeconfig" "$RUN_DIR/$SPOKE.kubeconfig" "$SPOKE" "$RUN_DIR"

KIND_HUB_KUBECONFIG="$RUN_DIR/$HUB.kubeconfig" \
KIND_SPOKE_KUBECONFIG="$RUN_DIR/$SPOKE.kubeconfig" \
  "$RUN_DIR/kind.test" -test.v -test.timeout=20m -ginkgo.v
