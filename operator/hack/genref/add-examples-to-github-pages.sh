#!/bin/bash
# Script to add all sample YAML files as examples to the GitHub Pages homepage
# Each example gets a proper header and description based on the resource type

set -euo pipefail

SAMPLES_DIR="${1:-config/samples}"
HOMEPAGE="${2:-docs/content/docs/examples.md}"

if [ ! -d "$SAMPLES_DIR" ]; then
    echo "Error: Samples directory not found: $SAMPLES_DIR"
    exit 1
fi

# Create the examples.md file if it doesn't exist
if [ ! -f "$HOMEPAGE" ]; then
    echo "Creating examples file: $HOMEPAGE"
    mkdir -p "$(dirname "$HOMEPAGE")"
    cat > "$HOMEPAGE" << 'EOF'
---
title: "Examples"
linkTitle: "Examples"
weight: 1
description: "Example configurations for Konflux Operator Custom Resources"
---

The following examples demonstrate how to configure Konflux Operator Custom Resources:

<!-- Examples will be added here by the script -->
EOF
fi

# Function to extract Title from YAML file comments
get_title_from_file() {
    local file="$1"
    grep -E "^# Title:" "$file" | head -1 | sed 's/^# Title:[[:space:]]*//' | sed 's/[[:space:]]*$//' || echo ""
}

# Function to extract Description from YAML file comments
get_description_from_file() {
    local file="$1"
    grep -E "^# Description:" "$file" | head -1 | sed 's/^# Description:[[:space:]]*//' | sed 's/[[:space:]]*$//' || echo ""
}

# Arrays to track files with missing comments
MISSING_TITLE=()
MISSING_DESCRIPTION=()
MISSING_BOTH=()

# Process each sample file to validate Title and Description comments
while IFS= read -r sample_file; do
    if [ ! -f "$sample_file" ]; then
        continue
    fi

    sample_name=$(basename "$sample_file")

    # Skip kustomization.yaml
    if [[ "$sample_name" == "kustomization.yaml" ]]; then
        continue
    fi

    # Extract title and description from comments
    title=$(get_title_from_file "$sample_file")
    description=$(get_description_from_file "$sample_file")

    # Track missing comments
    missing_items=()
    if [ -z "$title" ]; then
        missing_items+=("Title")
    fi
    if [ -z "$description" ]; then
        missing_items+=("Description")
    fi

    if [ ${#missing_items[@]} -eq 2 ]; then
        MISSING_BOTH+=("$sample_name")
    elif [ ${#missing_items[@]} -eq 1 ]; then
        if [ "${missing_items[0]}" == "Title" ]; then
            MISSING_TITLE+=("$sample_name")
        else
            MISSING_DESCRIPTION+=("$sample_name")
        fi
    fi
done < <(find "$SAMPLES_DIR" -maxdepth 1 -name "*.yaml" -type f 2>/dev/null | sort)

# Validate all files have required comments
TOTAL_ERRORS=$((${#MISSING_TITLE[@]} + ${#MISSING_DESCRIPTION[@]} + ${#MISSING_BOTH[@]}))

if [ $TOTAL_ERRORS -gt 0 ]; then
    echo "Error: The following sample files are missing required Title or Description comments:" >&2
    echo "" >&2

    # Files missing both
    for file in "${MISSING_BOTH[@]}"; do
        echo "  - $file (missing: Title, Description)" >&2
    done

    # Files missing only Title
    for file in "${MISSING_TITLE[@]}"; do
        echo "  - $file (missing: Title)" >&2
    done

    # Files missing only Description
    for file in "${MISSING_DESCRIPTION[@]}"; do
        echo "  - $file (missing: Description)" >&2
    done

    echo "" >&2
    echo "Please add '# Title: ...' and '# Description: ...' at the beginning of each file." >&2
    exit 1
fi

echo "Success: All sample files have Title and Description comments."

# Read the current homepage
HOMEPAGE_CONTENT=$(cat "$HOMEPAGE")

# Check if examples section already exists and has content
if echo "$HOMEPAGE_CONTENT" | grep -q "^###.*Example" && [ "$(echo "$HOMEPAGE_CONTENT" | grep -c "^###")" -gt 1 ]; then
    echo "Examples section already populated in homepage, skipping update..."
    exit 0
fi

# Create examples section content
EXAMPLES_CONTENT=""

# Process each sample file again to build examples content
while IFS= read -r sample_file; do
    if [ ! -f "$sample_file" ]; then
        continue
    fi

    sample_name=$(basename "$sample_file")

    # Skip kustomization.yaml
    if [[ "$sample_name" == "kustomization.yaml" ]]; then
        continue
    fi

    # Extract title and description from comments (already validated above)
    title=$(get_title_from_file "$sample_file")
    description=$(get_description_from_file "$sample_file")

    # Add to examples (we know both are present from validation above)
    EXAMPLES_CONTENT+="### $title

$description

\`\`\`yaml
$(cat "$sample_file")
\`\`\`

"
done < <(find "$SAMPLES_DIR" -maxdepth 1 -name "*.yaml" -type f 2>/dev/null | sort)

# Create a temporary file with the new content
TMP_FILE=$(mktemp)

# Write content up to (but not including) "## API Reference" or end of file
if echo "$HOMEPAGE_CONTENT" | grep -q "^## API Reference"; then
    # Get everything before "## API Reference"
    echo "$HOMEPAGE_CONTENT" | sed '/^## API Reference/,$d' > "$TMP_FILE"
    # Add examples section
    echo "$EXAMPLES_CONTENT" >> "$TMP_FILE"
    # Add API Reference section and everything after
    echo "$HOMEPAGE_CONTENT" | sed -n '/^## API Reference/,$p' >> "$TMP_FILE"
else
    # No API Reference section, just append
    echo "$HOMEPAGE_CONTENT" > "$TMP_FILE"
    echo "" >> "$TMP_FILE"
    echo "$EXAMPLES_CONTENT" >> "$TMP_FILE"
fi

# Write updated content
mv "$TMP_FILE" "$HOMEPAGE"
echo "Added examples to homepage: $HOMEPAGE"
