#!/usr/bin/env bash
# Sourced by the driver. Export only events and logs, never resource dumps.
sanitize_log() {
  awk '
    FILENAME == ARGV[1] { token = $0; next }
    /-----BEGIN .*PRIVATE KEY-----/ { private = 1 }
    private { if (/-----END .*PRIVATE KEY-----/) private = 0; next }
    {
      if (token != "") gsub(token, "[REDACTED]")
      gsub(/eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/, "[REDACTED]")
      if ($0 ~ /[Tt]oken[" :=]|[Aa]uthorization[" :=]|client-key-data/) {
        print "[REDACTED credential-bearing line]"
      } else print
    }
  ' "$RUN_DIR/redact.token" "$1"
}

collect_diagnostics() {
  local cluster namespace deployment file relative output
  # Empty when bootstrap failed before credentials were issued.
  : > "$RUN_DIR/redact.token"
  if [[ -f "$RUN_DIR/worker.token" ]]; then
    cp "$RUN_DIR/worker.token" "$RUN_DIR/redact.token"
  fi
  if (( ${#CLUSTERS[@]} == 0 )); then return; fi
  for cluster in "${CLUSTERS[@]}"; do
    output="$ARTIFACTS_DIR/$cluster"
    mkdir -p "$output"
    # Names/reasons are sufficient for diagnostics; omit arbitrary event messages.
    kubectl --kubeconfig "$RUN_DIR/$cluster.kubeconfig" --request-timeout=15s \
      get events -A -o custom-columns=NAMESPACE:.metadata.namespace,TYPE:.type,REASON:.reason,KIND:.involvedObject.kind,NAME:.involvedObject.name \
      > "$output/events.txt" 2>/dev/null || true
    for namespace in tekton-operator tekton-pipelines kueue-system cert-manager; do
      while IFS= read -r deployment; do
        [[ -n "$deployment" ]] || continue
        file="$RUN_DIR/controller.log"
        kubectl --kubeconfig "$RUN_DIR/$cluster.kubeconfig" --request-timeout=15s \
          logs -n "$namespace" "$deployment" --all-containers --tail=2000 --timestamps \
          > "$file" 2>/dev/null || true
        sanitize_log "$file" > "$output/$namespace-${deployment##*/}.log"
      done < <(kubectl --kubeconfig "$RUN_DIR/$cluster.kubeconfig" --request-timeout=15s \
        get deployments -n "$namespace" -o name 2>/dev/null)
    done
    # Kind's raw export stays private; copy only sanitized logs, no config or JSON.
    kind export logs --name "$cluster" "$RUN_DIR/kind-$cluster" >&2 || true
    if [[ -d "$RUN_DIR/kind-$cluster" ]]; then
      while IFS= read -r file; do
        relative=${file#"$RUN_DIR/kind-$cluster/"}
        mkdir -p "$output/kind/$(dirname "$relative")"
        sanitize_log "$file" > "$output/kind/$relative"
      done < <(find "$RUN_DIR/kind-$cluster" -type f -name '*.log')
    fi
  done
}
