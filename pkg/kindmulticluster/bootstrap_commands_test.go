package kindmulticluster

// Command-boundary fakes prove topology, explicit kubeconfigs, and ownership.
// They do not substitute for real component readiness or artifact validation.
const fakeProvisioner = `#!/usr/bin/env bash
set -eu
tool=${0##*/}
printf '%s %s\n' "$tool" "$*" >> "$CALLS"
case "$tool:$1" in
  kind:version) printf 'kind v0.31.0 go1.26.4 linux/amd64\n' ;;
  kind:get) printf 'unrelated-cluster\n' ;;
  kind:create)
    count=$(grep -c '^kind create cluster ' "$CALLS")
    if [[ $FAIL_AT == create-last && $count == "$EXPECTED_CLUSTERS" ]]; then exit 42; fi ;;
  go:*)
    if [[ $FAIL_AT == compile ]]; then exit 42; fi
    while [[ $# -gt 0 ]]; do
      if [[ $1 == -o ]]; then
        printf '%s\n' '#!/usr/bin/env bash' \
          'printf "suite %s hub=%s spoke=%s second=%s\n" "$*" "$KIND_HUB_KUBECONFIG" "$KIND_SPOKE_KUBECONFIG" "$KIND_SECOND_SPOKE_KUBECONFIG" >> "$CALLS"' > "$2"
        chmod u+x "$2"
        break
      fi
      shift
    done ;;
  kubectl:*)
    case " $* " in
      *" get tektonpipeline/pipeline "*) printf v1.12.0 ;;
      *" get tektonscheduler/tektonscheduler "*) printf v0.3.1 ;;
      *" get tektonmulticlusterproxyaae/multicluster-proxy-aae "*) printf v0.1.1 ;;
      *" -f - "*) while IFS= read -r line; do :; done ;;
    esac ;;
  jq:*) while IFS= read -r line; do :; done; printf '{}\n' ;;
  sha256sum:*) while IFS= read -r line; do :; done ;;
  curl:*)
    if [[ $FAIL_AT == download ]]; then exit 42; fi
    while [[ $# -gt 0 ]]; do
      if [[ $1 == -o ]]; then : > "$2"; break; fi
      shift
    done ;;
esac
`
