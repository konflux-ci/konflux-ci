---
name: create-pr
description: Create pull requests for konflux-ci repository. Explains CI behavior differences between fork and same-repo PRs, and the /allow command. Use when creating a PR, CI not running, or asking about fork PRs.
---

# Create Pull Request

## CI Behavior

| PR Source | E2E Tests | Trigger |
|-----------|-----------|---------|
| Same-repo branch | Automatic | Push |
| Fork | Manual | Org member comments `/allow` |

Fork PRs cannot access secrets. The `/allow` command:
1. Verifies code hasn't changed since comment (TOCTOU check)
2. Triggers E2E via `repository_dispatch`

See `.github/workflows/operator-test-e2e.yaml` (check-prerequisites job) and `.github/workflows/pr-comment-commands.yaml`.

## Before Creating a PR: Check Write Access

**Always check if the user has upstream write access before choosing a workflow.**

```bash
# Check if user can push to upstream
gh repo view konflux-ci/konflux-ci --json viewerPermission --jq '.viewerPermission'
# WRITE or ADMIN = has access, READ = no access
```

If user has write access → push to `upstream` (CI runs automatically)
If user does NOT have write access → push to fork (needs `/allow`)

## Workflow

**With upstream write access (preferred):**
```bash
git checkout -b feature-branch
git push -u upstream feature-branch
gh pr create --repo konflux-ci/konflux-ci
```

**Without write access (fork):**
1. Fork and clone
2. Push to fork, open PR against `konflux-ci/konflux-ci`
3. Wait for maintainer `/allow`

## Pre-PR Checklist

From `CONTRIBUTING.md`:

1. **KubeLinter** (if editing kustomizations):
   ```bash
   mkdir -p .kube-linter
   find . -name "kustomization.yaml" -o -name "kustomization.yml" | while read -r file; do
       dir=$(dirname "$file"); dir=${dir#./}
       kustomize build "$dir" > ".kube-linter/out-$(echo "$dir" | tr "/" "-").yaml"
   done
   kube-linter lint .kube-linter
   ```

2. **Table of contents** (if markdown structure changed):
   See `CONTRIBUTING.md` for the exact command.

## Troubleshooting

**CI not running on fork PR:** Normal - wait for `/allow` from maintainer.

**E2E failed after /allow:** Code changed after `/allow`. Maintainer must re-review and `/allow` again.
