#!/usr/bin/env bash
# Read override YAML from OVERRIDES_YAML_PATH or OVERRIDES_YAML, validate, write
# .tmp/component-sources.json (name + git rules only), then run apply-overrides.sh.
#
# Each list item:
#   name: <component under operator/upstream-kustomizations/>
#   git: []   # rules (may be empty if only images)
#   images: []   # { orig, replacement } (may be empty if only git)
#
# Git rule (first matching sourceRepo per resource URL wins):
#   sourceRepo: org/repo or https://github.com/org/repo
#   remote: { repo, ref }     # branch, tag, or SHA
#   # OR
#   localPath: /clone-root
#
# Per item: at least one of git or images must be non-empty.
#
# Usage: OVERRIDES_YAML_PATH=... [KONFLUX_READY_TIMEOUT=15m] $0 REPO_ROOT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${1:?usage: OVERRIDES_YAML[_PATH]=... $0 REPO_ROOT}" && pwd)"

mkdir -p "${REPO_ROOT}/.tmp"
if [[ -n "${OVERRIDES_YAML_PATH:-}" ]]; then
  if [[ ! -f "${OVERRIDES_YAML_PATH}" ]]; then
    echo "error: OVERRIDES_YAML_PATH is not a readable file: ${OVERRIDES_YAML_PATH}" >&2
    exit 1
  fi
  cp "${OVERRIDES_YAML_PATH}" "${REPO_ROOT}/.tmp/overrides.yaml"
else
  : "${OVERRIDES_YAML:?set OVERRIDES_YAML or OVERRIDES_YAML_PATH}"
  echo "${OVERRIDES_YAML}" > "${REPO_ROOT}/.tmp/overrides.yaml"
fi

OV_JSON="$(yq -o=json '.' "${REPO_ROOT}/.tmp/overrides.yaml")"
COMPONENT_SOURCES_FILE="${REPO_ROOT}/.tmp/component-sources.json"
echo "${OV_JSON}" | jq '[.[] | del(.images)]' > "${COMPONENT_SOURCES_FILE}"

if ! echo "${OV_JSON}" | jq -e '
  type == "array" and length > 0 and all(.[];
    (.name | type == "string" and length > 0)
    and (.git | type == "array")
    and (.images | type == "array")
    and ((.git | length > 0) or (.images | length > 0))
    and all(.images[]; (.orig | type == "string" and length > 0) and (.replacement | type == "string" and length > 0))
    and all(.git[];
      (.sourceRepo | type == "string" and length > 0)
      and (
        (has("remote") and (.remote.repo | type == "string" and length > 0) and (.remote.ref | type == "string" and length > 0) and (has("localPath") | not))
        or
        (has("localPath") and (.localPath | type == "string" and length > 0) and (has("remote") | not))
      )
    )
  )
' >/dev/null; then
  echo "error: need non-empty list; each item: name, git[] (sourceRepo + remote or localPath), images[] (orig, replacement); per item at least one of git or images non-empty" >&2
  exit 1
fi

unset IMAGE_OVERRIDES 2>/dev/null || true
if echo "${OV_JSON}" | jq -e 'any(.[]; (.images | length) > 0)' >/dev/null 2>&1; then
  mapfile -t _img_lines < <(echo "${OV_JSON}" | jq -r '.[] | .images[]? | "\(.orig)|\(.replacement)"')
  if [[ ${#_img_lines[@]} -gt 0 ]]; then
    IMAGE_OVERRIDES=$(printf '%s\n' "${_img_lines[@]}")
    export IMAGE_OVERRIDES
  fi
fi

export COMPONENT_SOURCES_FILE
bash "${SCRIPT_DIR}/apply-overrides.sh" "${REPO_ROOT}"

echo ""
echo "Image overrides (must exist within Konflux ready timeout: ${KONFLUX_READY_TIMEOUT:-15m}):"
echo "${OV_JSON}" | jq -r '.[] | .images[]? | "  \(.orig) -> \(.replacement)"' || true
echo "If these images are built in another job (e.g. Tekton), ensure they are pushed before the timeout expires."
