#!/usr/bin/env bash
#
# Applies per-component git source rules (explicit sourceRepo -> remote or localPath) and
# optional image overrides to konflux-ci operator manifests, then rebuilds components that
# had git rules applied.
#
# Repo kustomization files are never modified in-place on disk: when any git rules exist,
# a temporary copy of upstream-kustomizations is used.
#
# Requires: jq, yq (mikefarah yq v4), kustomize, python3 on PATH.
# localPath git rules rewrite resources to paths relative to each kustomization (kustomize forbids absolute paths).
#
# COMPONENT_SOURCES_FILE: JSON array of { name, git: [ rules... ] } (no images in file).
# Each git rule: { sourceRepo, remote: { repo, ref } } OR { sourceRepo, localPath }.
#
set -euo pipefail

REPO_ROOT="${1:-}"
if [[ -z "${REPO_ROOT}" || ! -d "${REPO_ROOT}" ]]; then
  echo "Usage: $0 REPO_ROOT" >&2
  exit 1
fi
REPO_ROOT="$(cd "${REPO_ROOT}" && pwd)"

SOURCE_UPSTREAM_DIR="${REPO_ROOT}/operator/upstream-kustomizations"
MANIFESTS_DIR="${REPO_ROOT}/operator/pkg/manifests"
UPSTREAM_DIR="${SOURCE_UPSTREAM_DIR}"
BUILD_SRC_REL="../../upstream-kustomizations"

has_git_overrides() {
  [[ -n "${COMPONENT_SOURCES_FILE:-}" ]] && [[ -f "${COMPONENT_SOURCES_FILE}" ]] &&
    jq -e 'any(.[]; (.git | type == "array") and (.git | length > 0))' "${COMPONENT_SOURCES_FILE}" >/dev/null 2>&1
}

if has_git_overrides; then
  TMP_UPSTREAM="${REPO_ROOT}/.tmp/upstream-kustomizations"
  rm -rf "${TMP_UPSTREAM}"
  mkdir -p "$(dirname "${TMP_UPSTREAM}")"
  cp -r "${SOURCE_UPSTREAM_DIR}" "${TMP_UPSTREAM}"
  UPSTREAM_DIR="${TMP_UPSTREAM}"
  BUILD_SRC_REL="../../../.tmp/upstream-kustomizations"
  echo "Using temporary copy of upstream-kustomizations; rebuilding components with git rules." >&2
fi

# Normalize to org/repo (lowercase) for comparison.
normalize_source_repo_spec() {
  local r="${1:?}"
  r="${r%.git}"
  r="${r#"${r%%[![:space:]]*}"}"
  r="${r%"${r##*[![:space:]]}"}"
  r="${r%/}"
  if [[ "$r" =~ ^https://github\.com/([^/]+)/([^/]+)(/.*)?$ ]]; then
    echo "${BASH_REMATCH[1]}/${BASH_REMATCH[2]}"
  elif [[ "$r" =~ ^([^/]+)/([^/]+)$ ]]; then
    echo "${BASH_REMATCH[1]}/${BASH_REMATCH[2]}"
  else
    echo "error: sourceRepo must be org/repo or https://github.com/org/repo" >&2
    return 1
  fi
}

parse_github_repo() {
  local r="${1:?}"
  r="${r%.git}"
  r="${r#"${r%%[![:space:]]*}"}"
  r="${r%"${r##*[![:space:]]}"}"
  r="${r%/}"
  if [[ "$r" =~ ^https://github\.com/([^/]+)/([^/]+)$ ]]; then
    printf '%s %s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}"
  elif [[ "$r" =~ ^([^/]+)/([^/]+)$ ]]; then
    printf '%s %s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}"
  else
    echo "error: remote.repo must be https://github.com/org/repo or org/repo" >&2
    return 1
  fi
}

# First matching rule in the git[] list wins.
find_rule_for_url_oor() {
  local url_oor_needle="$1"
  local git_json="$2"
  local rule s norm
  while IFS= read -r rule; do
    [[ -z "${rule}" ]] && continue
    s="$(echo "${rule}" | jq -r '.sourceRepo')"
    norm="$(normalize_source_repo_spec "${s}")" || continue
    if [[ "${norm,,}" == "${url_oor_needle}" ]]; then
      echo "${rule}"
      return 0
    fi
  done < <(echo "${git_json}" | jq -c '.[]')
  return 1
}

apply_git_rules_to_file() {
  local file="$1"
  local git_json="$2"
  local n idx val url_pair suffix rule newv org rname rrepo rref root
  local -a remote_refs=()

  n="$(yq eval '.resources | length' "${file}")"
  for ((idx = 0; idx < n; idx++)); do
    val="$(yq eval ".resources[$idx]" "${file}")"
    if [[ "${val}" != https://github.com/* ]]; then
      continue
    fi
    if [[ "${val}" =~ ^https://github.com/([^/]+)/([^/]+)(/[^?]+)(\?ref=[^&]*)?$ ]]; then
      url_pair="${BASH_REMATCH[1]}/${BASH_REMATCH[2]}"
      suffix="${BASH_REMATCH[3]}"
    else
      continue
    fi
    rule="$(find_rule_for_url_oor "${url_pair,,}" "${git_json}")" || continue

    if echo "${rule}" | jq -e 'has("remote")' >/dev/null 2>&1; then
      rrepo="$(echo "${rule}" | jq -r '.remote.repo')"
      rref="$(echo "${rule}" | jq -r '.remote.ref')"
      read -r org rname <<< "$(parse_github_repo "${rrepo}")" || exit 1
      newv="https://github.com/${org}/${rname}${suffix}?ref=${rref}"
      remote_refs+=("${rref}")
      echo "  [git] ${file} [${idx}] ${url_pair} -> remote ${org}/${rname}?ref=${rref}" >&2
    elif echo "${rule}" | jq -e 'has("localPath")' >/dev/null 2>&1; then
      root="$(echo "${rule}" | jq -r '.localPath')"
      root="$(cd "${root}" && pwd)"
      local newv_abs kdir
      newv_abs="${root}${suffix}"
      if [[ ! -d "${newv_abs}" ]]; then
        echo "error: localPath + suffix is not a directory: ${newv_abs}" >&2
        exit 1
      fi
      # Kustomize rejects absolute resource paths; use path relative to this kustomization file.
      kdir="$(cd "$(dirname "${file}")" && pwd)"
      newv="$(python3 -c 'import os, sys; print(os.path.relpath(os.path.abspath(sys.argv[1]), sys.argv[2]))' "${newv_abs}" "${kdir}")"
      echo "  [git] ${file} [${idx}] ${url_pair} -> ${newv} (relative to kustomization dir)" >&2
    else
      echo "error: git rule for ${url_pair} must have remote or localPath" >&2
      exit 1
    fi
    export YQ_NEW_RESOURCE="${newv}"
    yq eval -i ".resources[${idx}] = strenv(YQ_NEW_RESOURCE)" "${file}"
  done

  if yq eval '.images' "${file}" 2>/dev/null | grep -qv '^null$' && [[ ${#remote_refs[@]} -gt 0 ]]; then
    local uniq
    uniq="$(printf '%s\n' "${remote_refs[@]}" | sort -u)"
    if [[ "$(echo "${uniq}" | grep -c .)" -eq 1 ]]; then
      export YQ_GIT_REF="${uniq}"
      yq eval -i '(.images[] | select(has("newTag"))) |= .newTag = strenv(YQ_GIT_REF)' "${file}"
    fi
  fi
}

apply_all_component_sources() {
  local row name dir file git_json git_len
  while IFS= read -r row; do
    name="$(echo "${row}" | jq -r '.name')"
    if [[ -z "${name}" || "${name}" == "null" ]]; then
      echo "error: override entry missing name" >&2
      exit 1
    fi
    git_len="$(echo "${row}" | jq '.git | length')"
    if [[ "${git_len}" -eq 0 ]]; then
      continue
    fi
    dir="${UPSTREAM_DIR}/${name}"
    if [[ ! -d "${dir}" ]]; then
      echo "  [source] component ${name}: directory not found, skipping" >&2
      continue
    fi
    git_json="$(echo "${row}" | jq -c '.git')"
    echo "  [source] ${name}: ${git_len} git rule(s)" >&2
    while IFS= read -r -d '' file; do
      apply_git_rules_to_file "${file}" "${git_json}"
    done < <(find "${dir}" -type f \( -name 'kustomization.yaml' -o -name 'kustomization.yml' \) -print0 2>/dev/null)
  done < <(jq -c '.[]' "${COMPONENT_SOURCES_FILE}")
}

parse_output_image() {
  local output_image="$1"
  if [[ "${output_image}" == *"@"* ]]; then
    jq -n -c --arg repo "${output_image%%@*}" --arg tag "${output_image#*@}" '[$repo, $tag]'
  elif [[ "${output_image}" == *":"* ]]; then
    jq -n -c --arg repo "${output_image%%:*}" --arg tag "${output_image#*:}" '[$repo, $tag]'
  else
    jq -n -c --arg repo "${output_image}" --arg tag "latest" '[$repo, $tag]'
  fi
}

apply_image_overrides_kustomization() {
  local overrides="${IMAGE_OVERRIDES:-}"
  if [[ -z "${overrides}" ]]; then
    return 0
  fi

  while IFS= read -r line; do
    line="${line%%#*}"
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    if [[ -z "${line}" ]]; then
      continue
    fi

    local released output_image new_name new_tag
    released="${line%%|*}"
    output_image="${line#*|}"
    if [[ -z "${released}" || -z "${output_image}" ]]; then
      continue
    fi

    new_name=$(parse_output_image "${output_image}" | jq -r '.[0]')
    new_tag=$(parse_output_image "${output_image}" | jq -r '.[1]')

    while IFS= read -r -d '' file; do
      if [[ $(RELEASED="${released}" yq eval -o=json '[.images[]? | select(.name == strenv(RELEASED) or .newName == strenv(RELEASED))]' "${file}" 2>/dev/null | jq 'length') -eq 0 ]]; then
        continue
      fi
      if [[ $(yq eval -o=json '[.images[]? | select(.digest)]' "${file}" 2>/dev/null | jq 'length') -eq 0 ]]; then
        continue
      fi
      export RELEASED NEW_NAME="${new_name}" NEW_TAG="${new_tag}"
      yq eval -i '(.images[] | select(.name == strenv(RELEASED) or .newName == strenv(RELEASED))) |= (.newName = strenv(NEW_NAME) | .newTag = strenv(NEW_TAG) | del(.digest))' "${file}" 2>/dev/null && echo "  [image] ${file}: ${released} -> newName=${new_name}, newTag=${new_tag}" >&2
    done < <(find "${UPSTREAM_DIR}" -type f \( -name 'kustomization.yaml' -o -name 'kustomization.yml' \) -print0 2>/dev/null)
  done <<< "${overrides}"
}

apply_image_overrides_in_manifests() {
  local overrides="${IMAGE_OVERRIDES:-}"
  if [[ -z "${overrides}" ]]; then
    return 0
  fi

  while IFS= read -r line; do
    line="${line%%#*}"
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    if [[ -z "${line}" ]]; then
      continue
    fi

    local released output_image regex
    released="${line%%|*}"
    output_image="${line#*|}"
    if [[ -z "${released}" || -z "${output_image}" ]]; then
      continue
    fi

    regex="^$(printf '%s' "${released}" | sed 's/\./\\./g')($|:|@)"

    find "${MANIFESTS_DIR}" -name 'manifests.yaml' -type f -print0 2>/dev/null | while IFS= read -r -d '' f; do
      match_count=$(REGEX="${regex}" yq eval-all -o=json '[.. | select(tag == "!!str" and test(strenv(REGEX)))]' "${f}" 2>/dev/null | jq -s 'map(length) | add // 0')
      if [[ "${match_count:-0}" -eq 0 ]]; then
        continue
      fi
      REGEX="${regex}" OUTPUT_IMAGE="${output_image}" yq eval-all -i '(.. | select(tag == "!!str" and test(strenv(REGEX)))) |= strenv(OUTPUT_IMAGE)' "${f}" 2>/dev/null && echo "  [manifest] ${f}: replaced ${released} with ${output_image}" >&2
    done
  done <<< "${overrides}"
}

rebuild_manifests() {
  if ! has_git_overrides; then
    return 0
  fi
  local component
  while IFS= read -r component; do
    [[ -z "${component}" ]] && continue
    local src="${UPSTREAM_DIR}/${component}"
    if [[ ! -d "${src}" ]]; then
      echo "  [rebuild] ${component}: directory not found, skipping" >&2
      continue
    fi
    mkdir -p "${MANIFESTS_DIR}/${component}"
    local src_rel="${BUILD_SRC_REL}/${component}"
    local out_manifest="${REPO_ROOT}/operator/pkg/manifests/${component}/manifests.yaml"
    local tmp_out tmp_err
    tmp_out="$(mktemp)"
    tmp_err="$(mktemp)"
    if (cd "${REPO_ROOT}/operator/pkg/manifests" && kustomize build "${src_rel}" > "${tmp_out}" 2>"${tmp_err}"); then
      mv "${tmp_out}" "${out_manifest}"
      rm -f "${tmp_err}"
      echo "  [rebuild] ${component}" >&2
    else
      rm -f "${tmp_out}"
      echo "  [rebuild] ${component}: kustomize build failed" >&2
      cat "${tmp_err}" >&2
      rm -f "${tmp_err}"
      continue
    fi
  done < <(jq -r '.[] | select((.git | type == "array") and (.git | length > 0)) | .name' "${COMPONENT_SOURCES_FILE}")
}

# ---- Main ----
if has_git_overrides; then
  echo "Applying component git source overrides..." >&2
  apply_all_component_sources
  if [[ -n "${IMAGE_OVERRIDES:-}" ]]; then
    echo "Applying image overrides (kustomization: digest -> newTag)..." >&2
    apply_image_overrides_kustomization
  fi
  echo "Rebuilding manifests (components with git rules)..." >&2
  mkdir -p "${MANIFESTS_DIR}"
  rebuild_manifests
fi

if [[ -n "${IMAGE_OVERRIDES:-}" ]]; then
  echo "Applying image overrides (built manifests)..." >&2
  apply_image_overrides_in_manifests
fi

echo "Done." >&2
