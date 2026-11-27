#!/usr/bin/env bash
# Delete stale Quay repositories and robot accounts left behind by e2e runs.
#
# Logic lives here so it can be run locally, e.g.:
#   set -a && source scripts/deploy-local.env && set +a
#   DRY_RUN=true .github/scripts/cleanup-quay-e2e-resources.sh
#
# Usage:
#   cleanup-quay-e2e-resources.sh
#
# Supported on modern GNU/Linux bash environments (including GitHub Actions
# ubuntu runners). Not supported on macOS/BSD: for example, robot age parsing
# uses GNU `date -u -d ...`, which BSD/macOS date rejects.
# Requires: bash, curl, jq, grep, GNU date.
#
# Required environment variables:
#   QUAY_TOKEN          - Quay OAuth token with org admin permissions
#   QUAY_ORG            - Quay organization to clean
#                         (QUAY_ORGANIZATION is accepted as a fallback for local env files)
#
# Optional environment variables:
#   QUAY_API_URL        - Quay API base (default: https://quay.io/api/v1)
#   MAX_AGE_HOURS       - Delete resources older than this many hours (default: 12,
#                         max: 8760)
#   DRY_RUN             - Must be true or false; if true, only print what would be
#                         deleted (default: false)
#   NAME_PREFIX_REGEX   - Optional ERE matched against repo names and robot
#                         short names. Unset/empty matches everything.
#   DELETION_STAGGER_SEC - Pause between deletes (default: 0.1)
#   REQUIRE_MIN_DELETES - Must be true or false; if true, non-dry-run must delete
#                         at least MIN_REPOS_DELETED repos and MIN_ROBOTS_DELETED
#                         robots (default: false)
#   MIN_REPOS_DELETED   - Minimum repos required when REQUIRE_MIN_DELETES is true (default: 1)
#   MIN_ROBOTS_DELETED  - Minimum robots required when REQUIRE_MIN_DELETES is true (default: 1)
#
# Exit codes:
#   0 - success (including dry-run with candidates)
#   1 - usage / dependency / fatal API failure
#   2 - delete errors, or REQUIRE_MIN_DELETES unmet on a non-dry-run

set -euo pipefail

QUAY_API_URL="${QUAY_API_URL:-https://quay.io/api/v1}"
MAX_AGE_HOURS="${MAX_AGE_HOURS:-12}"
DRY_RUN="${DRY_RUN:-false}"
NAME_PREFIX_REGEX="${NAME_PREFIX_REGEX:-}"
DELETION_STAGGER_SEC="${DELETION_STAGGER_SEC:-0.1}"
REQUIRE_MIN_DELETES="${REQUIRE_MIN_DELETES:-false}"
MIN_REPOS_DELETED="${MIN_REPOS_DELETED:-1}"
MIN_ROBOTS_DELETED="${MIN_ROBOTS_DELETED:-1}"

# Quay naming constraints (reject anything outside these before interpolating into URLs).
# Robot shortname regex matches Quay API: ^([a-z0-9]+(?:[._-][a-z0-9]+)*)$
readonly QUAY_ORG_RE='^[a-z0-9][a-z0-9_-]{0,253}$'
readonly QUAY_ROBOT_SHORT_RE='^[a-z0-9]+([._-][a-z0-9]+)*$'
readonly QUAY_REPO_SEGMENT_RE='^[a-z0-9]+([._-][a-z0-9]+)*$'
readonly QUAY_ROBOT_CREATED_RE='^[A-Z][a-z]{2}, [0-9]{2} [A-Z][a-z]{2} [0-9]{4} [0-9]{2}:[0-9]{2}:[0-9]{2} [+-][0-9]{4}$'
readonly QUAY_API_URL_RE='^https://[A-Za-z0-9._~:/?#%+=&@,-]+$'
readonly MAX_AGE_HOURS_LIMIT=8760

REPO_DELETED=0
REPO_WOULD_DELETE=0
REPO_SKIPPED_AGE=0
REPO_SKIPPED_FILTER=0
REPO_SKIPPED_INVALID=0
REPO_ERRORS=0

ROBOT_DELETED=0
ROBOT_WOULD_DELETE=0
ROBOT_SKIPPED_AGE=0
ROBOT_SKIPPED_FILTER=0
ROBOT_SKIPPED_INVALID=0
ROBOT_ERRORS=0

OVERALL_RC=0
LISTINGS_DIR=""

log() {
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

die() {
  log "ERROR: $*"
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

validate_bool() {
  local name="$1"
  local value="$2"
  case "${value}" in
    true|false) ;;
    *) die "${name} must be 'true' or 'false' (got: ${value})" ;;
  esac
}

# URL-encode a single path segment (same approach as generate-changelog.sh / prepare-release-notes.sh).
uri_encode() {
  printf '%s' "$1" | jq -sRr @uri
}

# Encode a Quay repository path while preserving '/' separators between segments.
uri_encode_repo_path() {
  local repo_name="$1"
  local segment encoded="" first=1
  while IFS= read -r -d '/' segment || [[ -n "${segment}" ]]; do
    if [[ -z "${segment}" ]]; then
      return 1
    fi
    if ((first)); then
      encoded="$(uri_encode "${segment}")"
      first=0
    else
      encoded+="/$(uri_encode "${segment}")"
    fi
  done < <(printf '%s' "${repo_name}")
  [[ -n "${encoded}" ]] || return 1
  printf '%s' "${encoded}"
}

validate_org_name() {
  local org="$1"
  [[ "${org}" =~ ${QUAY_ORG_RE} ]] || die "QUAY_ORG has invalid characters: ${org}"
}

validate_api_url() {
  local url="$1"
  [[ "${url}" =~ ${QUAY_API_URL_RE} ]] || die "QUAY_API_URL must be an https URL"
  if [[ "${url}" == */ ]]; then
    die "QUAY_API_URL must not end with '/'"
  fi
}

validate_repo_name() {
  local repo_name="$1"
  local segment
  [[ -n "${repo_name}" ]] || return 1
  # Reject absolute/parent path tricks before segment checks.
  [[ "${repo_name}" != /* && "${repo_name}" != */ && "${repo_name}" != *..* ]] || return 1
  while IFS= read -r -d '/' segment || [[ -n "${segment}" ]]; do
    [[ "${segment}" =~ ${QUAY_REPO_SEGMENT_RE} ]] || return 1
  done < <(printf '%s' "${repo_name}")
  return 0
}

validate_robot_short_name() {
  local short_name="$1"
  [[ "${short_name}" =~ ${QUAY_ROBOT_SHORT_RE} ]]
}

validate_name_prefix_regex() {
  local regex="$1"
  local rc=0
  [[ -z "${regex}" ]] && return 0
  # grep -E: 0/1 = regex compiled (match/no-match), >=2 = compile/usage error.
  printf '' | grep -Eq -- "${regex}" 2>/dev/null || rc=$?
  if [[ "${rc}" -ge 2 ]]; then
    die "NAME_PREFIX_REGEX is not a valid extended regex"
  fi
}

quay_get() {
  local path="$1"
  curl -fsS \
    -H "Authorization: Bearer ${QUAY_TOKEN}" \
    -H "Accept: application/json" \
    -- \
    "${QUAY_API_URL}${path}"
}

quay_delete() {
  local path="$1"
  # Do not use curl -f: callers branch on the HTTP status code.
  curl -sS -o /dev/null -w "%{http_code}" \
    -X DELETE \
    -H "Authorization: Bearer ${QUAY_TOKEN}" \
    -H "Accept: application/json" \
    -- \
    "${QUAY_API_URL}${path}"
}

matches_prefix() {
  local name="$1"
  if [[ -z "${NAME_PREFIX_REGEX}" ]]; then
    return 0
  fi
  # Use grep -E instead of bash [[ =~ ]] so the pattern is always treated as an
  # ERE and cannot be confused with shell globs. -- prevents option injection.
  printf '%s\n' "${name}" | grep -Eq -- "${NAME_PREFIX_REGEX}"
}

is_older_than_max_age_epoch() {
  local epoch="$1"
  local now cutoff
  [[ "${epoch}" =~ ^[0-9]+$ ]] || return 1
  now="$(date -u +%s)"
  cutoff=$((now - MAX_AGE_HOURS * 3600))
  [[ "${epoch}" -le "${cutoff}" ]]
}

# Robot Created field: "Mon, 02 Jan 2006 15:04:05 -0700"
robot_created_epoch() {
  local created="$1"
  [[ "${created}" =~ ${QUAY_ROBOT_CREATED_RE} ]] || return 1
  date -u -d "${created}" +%s 2>/dev/null
}

list_all_repositories() {
  local next_page=""
  local response
  local -a curl_args

  # Explicit status checks: callers may invoke us from `cmd || die`, where set -e
  # is suppressed for the duration of cmd.
  while true; do
    curl_args=(
      -fsS
      -H "Authorization: Bearer ${QUAY_TOKEN}"
      -H "Accept: application/json"
      -G
      --data-urlencode "namespace=${QUAY_ORG}"
      --data-urlencode "last_modified=true"
      --data-urlencode "public=true"
      --data-urlencode "private=true"
    )
    if [[ -n "${next_page}" ]]; then
      curl_args+=(--data-urlencode "next_page=${next_page}")
    fi
    if ! response="$(curl "${curl_args[@]}" -- "${QUAY_API_URL}/repository")"; then
      return 1
    fi
    if ! jq -c '.repositories // [] | .[]' <<<"${response}"; then
      return 1
    fi
    if ! next_page="$(jq -r '.next_page // empty' <<<"${response}")"; then
      return 1
    fi
    if [[ -z "${next_page}" ]]; then
      break
    fi
  done
}

list_all_robots() {
  local encoded_org response
  encoded_org="$(uri_encode "${QUAY_ORG}")"
  # Quay returns the full org robot list when limit is high enough; no next_page.
  if ! response="$(quay_get "/organization/${encoded_org}/robots?token=false&limit=5000")"; then
    return 1
  fi
  if ! jq -c '.robots // [] | .[]' <<<"${response}"; then
    return 1
  fi
}

delete_repository() {
  local repo_name="$1"
  local encoded_org encoded_repo code
  encoded_org="$(uri_encode "${QUAY_ORG}")"
  encoded_repo="$(uri_encode_repo_path "${repo_name}")" || {
    log "ERROR: failed to encode repository path: ${repo_name}"
    REPO_ERRORS=$((REPO_ERRORS + 1))
    OVERALL_RC=2
    return 0
  }

  if [[ "${DRY_RUN}" == "true" ]]; then
    log "DRY_RUN would delete repository: ${QUAY_ORG}/${repo_name}"
    REPO_WOULD_DELETE=$((REPO_WOULD_DELETE + 1))
    return 0
  fi
  code="$(quay_delete "/repository/${encoded_org}/${encoded_repo}" || true)"
  case "${code}" in
    204|200|202)
      log "DELETED repository: ${QUAY_ORG}/${repo_name}"
      REPO_DELETED=$((REPO_DELETED + 1))
      ;;
    404)
      log "repository already absent: ${QUAY_ORG}/${repo_name}"
      ;;
    *)
      log "ERROR deleting repository ${QUAY_ORG}/${repo_name}: HTTP ${code:-curl-failed}"
      REPO_ERRORS=$((REPO_ERRORS + 1))
      OVERALL_RC=2
      ;;
  esac
  sleep "${DELETION_STAGGER_SEC}"
}

delete_robot() {
  local short_name="$1"
  local full_name="$2"
  local encoded_org encoded_robot code
  encoded_org="$(uri_encode "${QUAY_ORG}")"
  encoded_robot="$(uri_encode "${short_name}")"

  if [[ "${DRY_RUN}" == "true" ]]; then
    log "DRY_RUN would delete robot: ${full_name}"
    ROBOT_WOULD_DELETE=$((ROBOT_WOULD_DELETE + 1))
    return 0
  fi
  code="$(quay_delete "/organization/${encoded_org}/robots/${encoded_robot}" || true)"
  case "${code}" in
    204|200|202)
      log "DELETED robot: ${full_name}"
      ROBOT_DELETED=$((ROBOT_DELETED + 1))
      ;;
    404)
      log "robot already absent: ${full_name}"
      ;;
    *)
      log "ERROR deleting robot ${full_name}: HTTP ${code:-curl-failed}"
      ROBOT_ERRORS=$((ROBOT_ERRORS + 1))
      OVERALL_RC=2
      ;;
  esac
  sleep "${DELETION_STAGGER_SEC}"
}

cleanup_repositories() {
  local repo_json name last_modified
  local count=0
  local list_file="${LISTINGS_DIR}/repositories.jsonl"

  log "Listing repositories in organization ${QUAY_ORG}..."
  # Write the full listing first so producer failures are not hidden by process substitution.
  if ! list_all_repositories >"${list_file}"; then
    die "failed to list repositories from Quay"
  fi

  while IFS= read -r repo_json; do
    [[ -z "${repo_json}" ]] && continue
    count=$((count + 1))
    # Fixed jq programs only; never interpolate untrusted strings into jq code.
    name="$(jq -r '.name // empty' <<<"${repo_json}")"
    last_modified="$(jq -r '.last_modified // 0' <<<"${repo_json}")"
    [[ -z "${name}" ]] && continue

    if ! validate_repo_name "${name}"; then
      log "ERROR: skipping repository with unexpected name: ${name}"
      REPO_SKIPPED_INVALID=$((REPO_SKIPPED_INVALID + 1))
      REPO_ERRORS=$((REPO_ERRORS + 1))
      OVERALL_RC=2
      continue
    fi

    if ! matches_prefix "${name}"; then
      REPO_SKIPPED_FILTER=$((REPO_SKIPPED_FILTER + 1))
      continue
    fi

    if ! [[ "${last_modified}" =~ ^[0-9]+$ ]]; then
      log "ERROR: skipping repository ${name}: non-numeric last_modified"
      REPO_SKIPPED_INVALID=$((REPO_SKIPPED_INVALID + 1))
      REPO_ERRORS=$((REPO_ERRORS + 1))
      OVERALL_RC=2
      continue
    fi

    # Missing/zero last_modified is treated as stale (same as e2e-tests mage cleanup).
    if ! is_older_than_max_age_epoch "${last_modified}"; then
      REPO_SKIPPED_AGE=$((REPO_SKIPPED_AGE + 1))
      continue
    fi

    delete_repository "${name}"
  done <"${list_file}"

  log "Repository scan complete: listed=${count}"
}

cleanup_robots() {
  local robot_json full_name short_name created created_epoch
  local count=0
  local list_file="${LISTINGS_DIR}/robots.jsonl"

  log "Listing robot accounts in organization ${QUAY_ORG}..."
  if ! list_all_robots >"${list_file}"; then
    die "failed to list robot accounts from Quay"
  fi

  while IFS= read -r robot_json; do
    [[ -z "${robot_json}" ]] && continue
    count=$((count + 1))
    full_name="$(jq -r '.name // empty' <<<"${robot_json}")"
    created="$(jq -r '.created // empty' <<<"${robot_json}")"
    [[ -z "${full_name}" ]] && continue

    if [[ "${full_name}" == *"+"* ]]; then
      short_name="${full_name#*+}"
    else
      short_name="${full_name}"
    fi

    if ! validate_robot_short_name "${short_name}"; then
      log "ERROR: skipping robot with unexpected name: ${full_name}"
      ROBOT_SKIPPED_INVALID=$((ROBOT_SKIPPED_INVALID + 1))
      ROBOT_ERRORS=$((ROBOT_ERRORS + 1))
      OVERALL_RC=2
      continue
    fi

    if ! matches_prefix "${short_name}"; then
      ROBOT_SKIPPED_FILTER=$((ROBOT_SKIPPED_FILTER + 1))
      continue
    fi

    if [[ -z "${created}" ]]; then
      log "ERROR: robot ${full_name} missing created timestamp"
      ROBOT_ERRORS=$((ROBOT_ERRORS + 1))
      OVERALL_RC=2
      continue
    fi

    created_epoch="$(robot_created_epoch "${created}" || true)"
    if [[ -z "${created_epoch}" ]]; then
      log "ERROR: failed to parse created timestamp for robot ${full_name}: ${created}"
      ROBOT_ERRORS=$((ROBOT_ERRORS + 1))
      OVERALL_RC=2
      continue
    fi

    if ! is_older_than_max_age_epoch "${created_epoch}"; then
      ROBOT_SKIPPED_AGE=$((ROBOT_SKIPPED_AGE + 1))
      continue
    fi

    delete_robot "${short_name}" "${full_name}"
  done <"${list_file}"

  log "Robot scan complete: listed=${count}"
}

main() {
  require_cmd curl
  require_cmd jq
  require_cmd date
  require_cmd grep

  # Prefer QUAY_ORG (workflow/secret name); accept QUAY_ORGANIZATION for deploy-local.env.
  QUAY_ORG="${QUAY_ORG:-${QUAY_ORGANIZATION:-}}"

  [[ -n "${QUAY_TOKEN:-}" ]] || die "QUAY_TOKEN is required"
  [[ -n "${QUAY_ORG}" ]] || die "QUAY_ORG is required"
  [[ "${MAX_AGE_HOURS}" =~ ^[0-9]+$ ]] || die "MAX_AGE_HOURS must be an integer"
  ((MAX_AGE_HOURS <= MAX_AGE_HOURS_LIMIT)) || die "MAX_AGE_HOURS must be <= ${MAX_AGE_HOURS_LIMIT}"
  [[ "${DELETION_STAGGER_SEC}" =~ ^[0-9]+([.][0-9]+)?$ ]] || die "DELETION_STAGGER_SEC must be a number"
  [[ "${MIN_REPOS_DELETED}" =~ ^[0-9]+$ ]] || die "MIN_REPOS_DELETED must be an integer"
  [[ "${MIN_ROBOTS_DELETED}" =~ ^[0-9]+$ ]] || die "MIN_ROBOTS_DELETED must be an integer"

  validate_bool DRY_RUN "${DRY_RUN}"
  validate_bool REQUIRE_MIN_DELETES "${REQUIRE_MIN_DELETES}"
  validate_api_url "${QUAY_API_URL}"
  validate_org_name "${QUAY_ORG}"
  validate_name_prefix_regex "${NAME_PREFIX_REGEX}"

  LISTINGS_DIR="$(mktemp -d)"
  trap 'rm -rf "${LISTINGS_DIR}"' EXIT

  if [[ "${DRY_RUN}" == "true" ]]; then
    log "Starting Quay cleanup in DRY_RUN mode (org=${QUAY_ORG}, max_age_hours=${MAX_AGE_HOURS})"
  else
    log "Starting Quay cleanup (org=${QUAY_ORG}, max_age_hours=${MAX_AGE_HOURS})"
  fi
  if [[ -n "${NAME_PREFIX_REGEX}" ]]; then
    log "NAME_PREFIX_REGEX=${NAME_PREFIX_REGEX}"
  else
    log "NAME_PREFIX_REGEX unset; matching all names"
  fi

  cleanup_repositories
  cleanup_robots

  log "QUAY REPOSITORY CLEANUP REPORT:"
  log "  deleted=${REPO_DELETED} would_delete=${REPO_WOULD_DELETE} skipped_age=${REPO_SKIPPED_AGE} skipped_filter=${REPO_SKIPPED_FILTER} skipped_invalid=${REPO_SKIPPED_INVALID} errors=${REPO_ERRORS}"
  log "QUAY ROBOT CLEANUP REPORT:"
  log "  deleted=${ROBOT_DELETED} would_delete=${ROBOT_WOULD_DELETE} skipped_age=${ROBOT_SKIPPED_AGE} skipped_filter=${ROBOT_SKIPPED_FILTER} skipped_invalid=${ROBOT_SKIPPED_INVALID} errors=${ROBOT_ERRORS}"

  if [[ "${DRY_RUN}" != "true" && "${REQUIRE_MIN_DELETES}" == "true" ]]; then
    if ((REPO_DELETED < MIN_REPOS_DELETED)); then
      log "ERROR: expected to delete at least ${MIN_REPOS_DELETED} repository(ies), deleted ${REPO_DELETED}"
      OVERALL_RC=2
    fi
    if ((ROBOT_DELETED < MIN_ROBOTS_DELETED)); then
      log "ERROR: expected to delete at least ${MIN_ROBOTS_DELETED} robot(s), deleted ${ROBOT_DELETED}"
      OVERALL_RC=2
    fi
  fi

  exit "${OVERALL_RC}"
}

main "$@"
