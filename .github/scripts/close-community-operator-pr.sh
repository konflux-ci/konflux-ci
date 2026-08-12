#!/bin/bash
set -euo pipefail

# Close a Community Operator PR opened by konflux-ci-bot.
#
# Usage:
#   close-community-operator-pr.sh <pr_number> [--comment <text>]
#
# Arguments:
#   pr_number  - Pull request number on community-operators-prod (e.g., 10746)
#   --comment  - Optional comment to leave when closing
#
# Environment Variables (required):
#   GITHUB_TOKEN or GH_TOKEN - PAT for konflux-ci-bot (same as create/update workflows)
#
# Examples:
#   GITHUB_TOKEN=ghp_xxx close-community-operator-pr.sh 10746
#   GITHUB_TOKEN=ghp_xxx close-community-operator-pr.sh 10746 --comment "Superseded by v0.2.3"

UPSTREAM_REPO="${UPSTREAM_REPO:-redhat-openshift-ecosystem/community-operators-prod}"
FORK_REPO="${FORK_REPO:-konflux-ci/community-operators-prod}"
BOT_LOGIN="${BOT_LOGIN:-konflux-ci-bot}"
OPERATOR_NAME="${OPERATOR_NAME:-konflux}"

PR_NUMBER=""
COMMENT=""

while [ $# -gt 0 ]; do
  case "$1" in
    --comment)
      if [ $# -lt 2 ]; then
        echo "Error: --comment requires a value"
        exit 1
      fi
      COMMENT="$2"
      shift 2
      ;;
    -*)
      echo "Error: Unknown option $1"
      exit 1
      ;;
    *)
      if [ -z "${PR_NUMBER}" ]; then
        PR_NUMBER="$1"
      else
        echo "Error: Unexpected argument $1"
        exit 1
      fi
      shift
      ;;
  esac
done

if [ -z "${PR_NUMBER}" ]; then
  echo "Error: pr_number is required"
  echo "Usage: $0 <pr_number> [--comment <text>]"
  exit 1
fi

if ! [[ "${PR_NUMBER}" =~ ^[0-9]+$ ]]; then
  echo "Error: pr_number must be a positive integer (got: ${PR_NUMBER})"
  exit 1
fi

if [ -z "${GITHUB_TOKEN:-${GH_TOKEN:-}}" ]; then
  echo "Error: GITHUB_TOKEN or GH_TOKEN is required"
  exit 1
fi

GH_TOKEN="${GITHUB_TOKEN:-${GH_TOKEN}}"

echo "=== Closing Community Operator PR #${PR_NUMBER} ==="
echo "Repository: ${UPSTREAM_REPO}"

PR_JSON="$(GH_TOKEN="${GH_TOKEN}" gh pr view "${PR_NUMBER}" \
  --repo "${UPSTREAM_REPO}" \
  --json number,state,title,author,headRefName,headRepositoryOwner,headRepository,url)"

STATE="$(echo "${PR_JSON}" | jq -r '.state')"
AUTHOR="$(echo "${PR_JSON}" | jq -r '.author.login')"
HEAD_OWNER="$(echo "${PR_JSON}" | jq -r '.headRepositoryOwner.login')"
HEAD_REPO="$(echo "${PR_JSON}" | jq -r '.headRepository.name')"
HEAD_BRANCH="$(echo "${PR_JSON}" | jq -r '.headRefName')"
TITLE="$(echo "${PR_JSON}" | jq -r '.title')"
PR_URL="$(echo "${PR_JSON}" | jq -r '.url')"

echo "PR: ${PR_URL}"
echo "Title: ${TITLE}"
echo "State: ${STATE}"
echo "Author: ${AUTHOR}"
echo "Head: ${HEAD_OWNER}/${HEAD_REPO}:${HEAD_BRANCH}"

if [ "${AUTHOR}" != "${BOT_LOGIN}" ]; then
  echo "Error: PR #${PR_NUMBER} is not authored by ${BOT_LOGIN} (author: ${AUTHOR})"
  exit 1
fi

if [ "${HEAD_OWNER}" != "${FORK_REPO%%/*}" ] || [ "${HEAD_REPO}" != "${FORK_REPO##*/}" ]; then
  echo "Error: PR #${PR_NUMBER} head is not from fork ${FORK_REPO} (${HEAD_OWNER}/${HEAD_REPO})"
  exit 1
fi

EXPECTED_BRANCH_PREFIX="${OPERATOR_NAME}-"
if [[ "${HEAD_BRANCH}" != "${EXPECTED_BRANCH_PREFIX}"* ]]; then
  echo "Error: PR #${PR_NUMBER} head branch ${HEAD_BRANCH} does not match expected ${OPERATOR_NAME}-<version>"
  exit 1
fi

if [ "${STATE}" = "CLOSED" ] || [ "${STATE}" = "MERGED" ]; then
  echo "PR #${PR_NUMBER} is already ${STATE}."
  exit 0
fi

if [ "${STATE}" != "OPEN" ]; then
  echo "Error: PR #${PR_NUMBER} is not open (state: ${STATE})"
  exit 1
fi

CLOSE_ARGS=(pr close "${PR_NUMBER}" --repo "${UPSTREAM_REPO}")
if [ -n "${COMMENT}" ]; then
  CLOSE_ARGS+=(--comment "${COMMENT}")
fi

GH_TOKEN="${GH_TOKEN}" gh "${CLOSE_ARGS[@]}"

echo ""
echo "=== PR closed ==="
echo "PR URL: ${PR_URL}"
