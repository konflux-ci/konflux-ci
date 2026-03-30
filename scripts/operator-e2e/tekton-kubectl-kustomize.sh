# shellcheck shell=bash
# Source from bash before scripts that invoke `kustomize build` (e.g. apply-overrides.sh).
# Uses kubectl-embedded kustomize; no standalone kustomize binary required.
if ! command -v kustomize >/dev/null 2>&1; then
  # shellcheck disable=SC2329
  kustomize() {
    if [[ "${1:-}" != build ]]; then
      echo 'kustomize wrapper: only "kustomize build <dir>" is supported (kubectl kustomize)' >&2
      return 1
    fi
    shift
    kubectl kustomize "$@"
  }
  export -f kustomize
fi
