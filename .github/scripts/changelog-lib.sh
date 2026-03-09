#!/bin/bash
# Shared changelog library
# Provides common functions for filtering and formatting conventional commits
# from the GitHub Compare API.
#
# Sourced by: generate-changelog.sh, prepare-release-notes.sh
#
# Functions:
#   changelog_filter_commits  - jq filter: GitHub Compare API JSON → feat/fix commit messages
#   changelog_strip_prefix    - sed pipe: remove conventional commit type prefix
#   changelog_qualify_refs    - sed pipe: replace bare (#NNN) with (owner/repo#NNN) for GitHub linking
#   changelog_format_grouped  - stdin commit messages → grouped #### Features / #### Bug Fixes markdown

# Bot authors to exclude from changelog
CHANGELOG_BOT_AUTHORS='["dependabot[bot]", "renovate[bot]", "github-actions[bot]", "konflux-internal-p02[bot]", "red-hat-konflux[bot]"]'

# changelog_filter_commits filters GitHub Compare API JSON on stdin to conventional
# commit messages. Keeps only feat/fix commits, excludes bot authors, outputs the
# first line of each matching commit message.
changelog_filter_commits() {
  jq -r --argjson bots "$CHANGELOG_BOT_AUTHORS" '
    .commits[]
    | select(
        ((.author.login // "") as $login |
          ($bots | map(. == $login) | any | not))
        and
        (.commit.message | split("\n")[0] | test("^(feat|fix)(\\(.*\\))?!?:"))
      )
    | .commit.message | split("\n")[0]
  '
}

# changelog_strip_prefix removes the conventional commit type prefix from a message,
# returning just the description as a markdown list item.
# "feat(scope): add X"  → "- add X"
# "fix: broken Y"       → "- broken Y"
# "feat!: breaking Z"   → "- breaking Z"
changelog_strip_prefix() {
  sed 's/^[a-z]*([^)]*)\!*:[[:space:]]*/- /; s/^[a-z]*\!*:[[:space:]]*/- /'
}

# changelog_qualify_refs replaces bare PR references (#NNN) with qualified
# references (owner/repo#NNN) so GitHub auto-links to the correct repository.
# Reads commit messages from stdin, writes qualified messages to stdout.
# Args: $1=github_repo (e.g., "konflux-ci/build-service")
changelog_qualify_refs() {
  local repo="$1"
  sed "s|(#\([0-9]\{1,\}\))|(${repo}#\1)|g"
}

# changelog_format_grouped reads conventional commit messages from stdin,
# splits them by type (feat/fix), and outputs grouped markdown sub-sections.
# Returns 0 if stdin is empty (no commits is a valid state, not an error).
changelog_format_grouped() {
  local commits
  commits=$(cat)

  if [ -z "$commits" ]; then
    return 0
  fi

  local feat_commits fix_commits
  feat_commits=$(echo "$commits" | grep -E '^feat(\(.*\))?!?:' || true)
  fix_commits=$(echo "$commits" | grep -E '^fix(\(.*\))?!?:' || true)

  if [ -n "$feat_commits" ]; then
    echo "#### Features"
    echo "$feat_commits" | changelog_strip_prefix
    echo ""
  fi
  if [ -n "$fix_commits" ]; then
    echo "#### Bug Fixes"
    echo "$fix_commits" | changelog_strip_prefix
    echo ""
  fi
}
