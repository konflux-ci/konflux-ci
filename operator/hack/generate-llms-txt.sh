#!/bin/bash
# Generates llms.txt for the Konflux Operator documentation.
# Scans the Hugo content tree and produces a structured index
# following the llms.txt specification (https://llmstxt.org/).
#
# Usage:
#   generate-llms-txt.sh <content_dir> <output_file>
#
# Arguments:
#   content_dir  - Path to Hugo content/docs/ directory
#   output_file  - Output path for llms.txt (e.g., docs/static/llms.txt)
#
# Environment:
#   RAW_GITHUB_BASE - Base URL for raw markdown links
#                     (default: https://raw.githubusercontent.com/konflux-ci/konflux-ci/main/operator/docs/content/docs)

set -euo pipefail

CONTENT_DIR="${1:?Usage: generate-llms-txt.sh <content_dir> <output_file>}"
OUTPUT_FILE="${2:?Usage: generate-llms-txt.sh <content_dir> <output_file>}"
RAW_BASE="${RAW_GITHUB_BASE:-https://raw.githubusercontent.com/konflux-ci/konflux-ci/main/operator/docs/content/docs}"

# Extract a YAML frontmatter field value from a markdown file.
# Handles both quoted ("value") and unquoted (value) forms.
get_frontmatter() {
  local file="$1" field="$2"
  sed -n '/^---$/,/^---$/p' "$file" \
    | grep -E "^${field}:" \
    | head -1 \
    | sed "s/^${field}:[[:space:]]*//; s/^[\"']//; s/[\"'][[:space:]]*$//" \
    || true
}

# Build a "weight|link line" for a single content file.
# Outputs nothing if the file has no title.
make_entry() {
  local file="$1" indent="${2:-}"
  local title description weight relpath url

  title=$(get_frontmatter "$file" "title")
  [ -z "$title" ] && return 0

  description=$(get_frontmatter "$file" "description")
  weight=$(get_frontmatter "$file" "weight")
  weight="${weight:-999}"
  relpath="${file#"$CONTENT_DIR"/}"
  url="${RAW_BASE}/${relpath}"

  if [ -n "$description" ]; then
    printf '%s|%s- [%s](%s): %s\n' "$weight" "$indent" "$title" "$url" "$description"
  else
    printf '%s|%s- [%s](%s)\n' "$weight" "$indent" "$title" "$url"
  fi
}

# Emit a sorted markdown link list for every non-index .md file in a directory.
emit_section() {
  local dir="$1"
  local tmpfile
  tmpfile=$(mktemp)

  for file in "$dir"/*.md; do
    [ -f "$file" ] || continue
    [[ "$(basename "$file")" == "_index.md" ]] && continue
    make_entry "$file" >> "$tmpfile"
  done

  if [ -s "$tmpfile" ]; then
    sort -t'|' -k1 -n "$tmpfile" | cut -d'|' -f2-
  fi
  rm -f "$tmpfile"
}

mkdir -p "$(dirname "$OUTPUT_FILE")"

# Section ordering: directory name → display heading.
# Add new sections here when the content tree grows.
SECTION_DIRS=(  ""             "installation"  "guides"  "onboard"              "reference"    )
SECTION_HDRS=("Docs"          "Installation"  "Guides"  "Onboarding Tutorial"  "API Reference")

# Files to exclude from the main sections and place in Optional instead.
OPTIONAL_FILES=("examples.md")

is_optional() {
  local base="$1"
  for opt in "${OPTIONAL_FILES[@]}"; do
    [[ "$base" == "$opt" ]] && return 0
  done
  return 1
}

{
# --- Header (static project-level info) ---
cat << 'HEADER'
# Konflux Operator

> The Konflux Operator is a Kubernetes-native operator that installs, configures, and manages the Konflux CI/CD platform from a single declarative Custom Resource. It deploys and wires together build controllers, release pipelines, policy engines, identity providers, ingress, and more. It works on any Kubernetes cluster: local Kind environments, OpenShift, EKS, GKE, or any conformant distribution.

- Source code: https://github.com/konflux-ci/konflux-ci
- Konflux project docs: https://konflux-ci.dev/docs/
- Operator docs site: https://konflux-ci.dev/konflux-ci/docs/
HEADER

# --- Content sections ---
for i in "${!SECTION_DIRS[@]}"; do
  section_dir="${SECTION_DIRS[$i]}"
  section_hdr="${SECTION_HDRS[$i]}"

  if [ -z "$section_dir" ]; then
    # Top-level pages (overview, troubleshooting, …) — skip _index.md and optional files
    tmpfile=$(mktemp)
    for file in "$CONTENT_DIR"/*.md; do
      [ -f "$file" ] || continue
      base=$(basename "$file")
      [[ "$base" == "_index.md" ]] && continue
      is_optional "$base" && continue
      make_entry "$file" >> "$tmpfile"
    done

    if [ -s "$tmpfile" ]; then
      printf '\n## %s\n\n' "$section_hdr"
      sort -t'|' -k1 -n "$tmpfile" | cut -d'|' -f2-
    fi
    rm -f "$tmpfile"
  else
    dir="$CONTENT_DIR/$section_dir"
    [ -d "$dir" ] || continue
    section_entries=$(emit_section "$dir")
    if [ -n "$section_entries" ]; then
      printf '\n## %s\n\n' "$section_hdr"
      printf '%s\n' "$section_entries"
    fi
  fi
done

# --- Optional section ---
tmpfile=$(mktemp)
for opt in "${OPTIONAL_FILES[@]}"; do
  file="$CONTENT_DIR/$opt"
  [ -f "$file" ] || continue
  make_entry "$file" >> "$tmpfile"
done

if [ -s "$tmpfile" ]; then
  printf '\n## Optional\n\n'
  sort -t'|' -k1 -n "$tmpfile" | cut -d'|' -f2-
fi
rm -f "$tmpfile"

} > "$OUTPUT_FILE"

echo "Generated llms.txt: $OUTPUT_FILE"
