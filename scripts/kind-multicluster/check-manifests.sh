#!/usr/bin/env bash
# Offline rendering only. Downloads pinned artifacts; never contacts a cluster.
set -euo pipefail
umask 077
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
RUN_DIR=$(mktemp -d "$ROOT/.kind-multicluster.render.XXXXXXXX")
trap 'rm -rf "$RUN_DIR"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# shellcheck source=scripts/kind-multicluster/install.sh
source "$ROOT/scripts/kind-multicluster/install.sh"
download_artifacts
kubectl kustomize "$RUN_DIR/artifacts" > /dev/null
mkdir "$RUN_DIR/fixture"
for file in queues.yaml hub.yaml worker-rbac.yaml quota-ten.yaml; do
  cp "$ROOT/testdata/kind-multicluster/$file" "$RUN_DIR/fixture/"
done
cat > "$RUN_DIR/fixture/kustomization.yaml" <<'EOF'
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - queues.yaml
  - hub.yaml
  - worker-rbac.yaml
EOF
kubectl kustomize "$RUN_DIR/fixture" > /dev/null
cat >> "$RUN_DIR/fixture/kustomization.yaml" <<'EOF'
patches:
  - path: quota-ten.yaml
EOF
kubectl kustomize "$RUN_DIR/fixture" > /dev/null
printf 'CHECKSUMS_AND_BOTH_FIXTURE_RENDERS_OK\n'
