#!/usr/bin/env bash
# Verify that container image references in kustomization files exist in their
# registries. Designed to be called by the manifest companion workflow before
# committing, so that PRs referencing unpublished images are caught early.
#
# Usage:
#   verify-image-refs.sh <kustomization-file> [<kustomization-file> ...]
#
# Exit codes:
#   0 — all images verified (or no image references found)
#   1 — script error (usage, missing skopeo/yq, YAML parse failure)
#   2 — one or more images do not exist in their registries
#
# Environment:
#   SKIP_IMAGE_VERIFY — set to "true" to skip verification (escape hatch for
#                       registry outages or network issues)
#
# Requires: skopeo, yq on PATH.
set -euo pipefail

if [[ "${SKIP_IMAGE_VERIFY:-}" == "true" ]]; then
  echo "SKIP_IMAGE_VERIFY is set; skipping image reference verification."
  exit 0
fi

if [[ $# -eq 0 ]]; then
  echo "Usage: $0 <kustomization-file> [<kustomization-file> ...]" >&2
  exit 1
fi

if ! command -v skopeo &>/dev/null; then
  echo "::error::skopeo is required for image verification but was not found on PATH." >&2
  exit 1
fi

if ! command -v yq &>/dev/null; then
  echo "::error::yq is required for image verification but was not found on PATH." >&2
  exit 1
fi

# yq_null_or_empty <value>
#   Returns 0 when the value is unset, empty, or the literal "null".
yq_null_or_empty() {
  [[ -z "${1}" || "${1}" == "null" ]]
}

# extract_image_refs <kustomization-file>
#   Prints one "IMAGE_NAME<TAB>REF_TYPE<TAB>REF_VALUE<TAB>SOURCE_FILE" per line.
extract_image_refs() {
  local kust_file="$1"
  local image_count idx img_name new_name new_tag digest ref_type ref_value

  if [[ ! -f "${kust_file}" ]]; then
    echo "::error::Kustomization file not found: ${kust_file}" >&2
    return 1
  fi

  if ! yq eval '.' "${kust_file}" >/dev/null 2>&1; then
    echo "::error::Failed to parse YAML in ${kust_file}" >&2
    return 1
  fi

  image_count="$(yq eval '.images | length' "${kust_file}" 2>/dev/null || true)"
  if yq_null_or_empty "${image_count}" || [[ "${image_count}" -eq 0 ]]; then
    return 0
  fi

  for ((idx = 0; idx < image_count; idx++)); do
    img_name="$(yq eval ".images[${idx}].name // \"\"" "${kust_file}")"
    new_name="$(yq eval ".images[${idx}].newName // \"\"" "${kust_file}")"
    new_tag="$(yq eval ".images[${idx}].newTag // \"\"" "${kust_file}")"
    digest="$(yq eval ".images[${idx}].digest // \"\"" "${kust_file}")"

    if ! yq_null_or_empty "${new_name}"; then
      img_name="${new_name}"
    fi

    if yq_null_or_empty "${img_name}"; then
      continue
    fi

    if ! yq_null_or_empty "${new_tag}"; then
      ref_type="tag"
      ref_value="${new_tag}"
    elif ! yq_null_or_empty "${digest}"; then
      ref_type="digest"
      ref_value="${digest}"
    else
      echo "::warning::Image ${img_name} in ${kust_file} has neither newTag nor digest; skipping verification" >&2
      continue
    fi

    printf '%s\t%s\t%s\t%s\n' "${img_name}" "${ref_type}" "${ref_value}" "${kust_file}"
  done
}

# check_image <name> <ref_type> <ref_value>
#   Returns 0 if the image exists, 1 otherwise.
check_image() {
  local name="$1" ref_type="$2" ref_value="$3"
  local full_ref

  if [[ "${ref_type}" == "tag" ]]; then
    full_ref="${name}:${ref_value}"
  else
    full_ref="${name}@${ref_value}"
  fi

  skopeo inspect --raw "docker://${full_ref}" &>/dev/null
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
refs=()
for kust_file in "$@"; do
  while IFS= read -r line; do
    [[ -z "${line}" ]] && continue
    refs+=("${line}")
  done < <(extract_image_refs "${kust_file}")
done

if [[ "${#refs[@]}" -eq 0 ]]; then
  echo "No image references found to verify."
  exit 0
fi

missing=0
checked=0

for line in "${refs[@]}"; do
  IFS=$'\t' read -r img_name ref_type ref_value source_file <<<"${line}"

  local_ref=""
  if [[ "${ref_type}" == "tag" ]]; then
    local_ref="${img_name}:${ref_value}"
  else
    local_ref="${img_name}@${ref_value}"
  fi

  echo "Checking image: ${local_ref}"
  checked=$((checked + 1))

  if ! check_image "${img_name}" "${ref_type}" "${ref_value}"; then
    echo "::error::Image does not exist: ${local_ref} (referenced in ${source_file})"
    missing=$((missing + 1))
  fi
done

if [[ "${missing}" -gt 0 ]]; then
  echo ""
  echo "::error::${missing} of ${checked} image(s) not found in their registries. The upstream release process may have failed to push the image(s). Re-trigger the upstream build or pin to a commit with a valid image."
  exit 2
fi

echo "✓ All ${checked} image(s) verified."
exit 0
