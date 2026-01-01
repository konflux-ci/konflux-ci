#!/bin/bash
set -euo pipefail

# Verify Generated Files Script
# This script verifies that generated files (CRDs, RBAC, and code) are in sync
# It checks for any changes in the operator directory after generation

# Define the target directory
TARGET_DIR="operator"

# Generate the files
make -C "$TARGET_DIR" generate manifests

# Check for any changes (Modified, Untracked, Staged)
# --porcelain ensures the output is stable and machine-readable
CHANGES=$(git status --porcelain "$TARGET_DIR" 2>/dev/null || true)

if [ -n "$CHANGES" ]; then
  echo "❌ FAIL: Changes detected in '$TARGET_DIR':"
  echo "$CHANGES"
  echo ""
  echo "Please run 'make generate' and 'make manifests' locally and commit the changes."
  exit 1
else
  echo "✅ PASS: No changes detected in '$TARGET_DIR'."
  exit 0
fi
