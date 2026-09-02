---
name: create-pr
description: Create pull requests for konflux-ci repository. Explains CI behavior differences between fork and same-repo PRs, and the /allow command. Use when creating a PR, CI not running, or asking about fork PRs.
---

# Create Pull Request

## CI Behavior

| PR Source | E2E Tests | Trigger |
|-----------|-----------|---------|
| Same-repo branch | Automatic | Push |
| Fork | Manual | Org member comments `/allow <commit-sha>` |

Fork PRs cannot access secrets. The `/allow <commit-sha>` command:
1. Verifies the provided SHA is a valid commit hash (not a branch/tag)
2. Verifies the SHA matches the current PR HEAD (prevents approving stale code)
3. Verifies code hasn't changed since comment (TOCTOU check)
4. Triggers E2E via `repository_dispatch`

See `.github/workflows/operator-test-e2e.yaml` (check-prerequisites job) and `.github/workflows/pr-comment-commands.yaml`.

## Before Creating a PR: Check Write Access

**Always check if the user has upstream write access before choosing a workflow.**

```bash
# Check if user can push to upstream
gh repo view konflux-ci/konflux-ci --json viewerPermission --jq '.viewerPermission'
# WRITE or ADMIN = has access, READ = no access
```

If user has write access → push to `upstream` (CI runs automatically)
If user does NOT have write access → push to fork (needs `/allow <commit-sha>`)

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
3. Wait for maintainer `/allow <commit-sha>`

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

**CI not running on fork PR:** Normal - wait for `/allow <commit-sha>` from maintainer.

**E2E failed after /allow <commit-sha>:** Code changed after `/allow <commit-sha>`. Maintainer must re-review and `/allow <commit-sha>` again.
