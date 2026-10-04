---
name: pr-review
description: >-
  Use when reviewing pull requests in konflux-ci/konflux-ci. Covers
  upstream/downstream hygiene and other repo-wide review checks that are easy
  to miss in a focused feature review.
---

# PR Review

Apply these checks on every PR, including ones opened by agents (for example
fullsend). Skip companion-eligible MintMaker/Renovate parents and lock-file-only
PRs (see fast-path sections below) before dispatching sub-agents.

## Companion-eligible MintMaker/Renovate parents

Before running any sub-agent review pipeline, check whether the PR is a
companion-eligible parent that should be fast-pathed:

1. **Check conditions** — both must be true:
   - Author is `renovate[bot]` or `red-hat-konflux[bot]`
   - All changed files are on the companion allowlist:
     - `operator/upstream-kustomizations/**`
     - `.github/scripts/export-third-party-chart-env.sh`
     - `dependencies/registry/kustomization.yml` (registry digest bumps only)
     - `operator/go.mod` / `operator/go.sum`
     - `test/go-tests/go.mod` / `test/go-tests/go.sum`

   If either condition fails, fall through to normal review.

2. **Check for noop marker** — search PR comments for
   `<!-- konflux-manifest-companion-noop:N -->` authored by
   `github-actions[bot]`. Ignore markers posted by other accounts.
   If found: **approve without sub-agents** and apply `ready-for-merge`
   if CI is green (same fast-path as lock-file-only PRs below).

3. **No noop marker found** — do not run full review, do not approve, do
   not add `ready-for-merge`. The companion workflow may still be running,
   or it may have posted a different marker (notify or missing-image).
   Use hedged language if commenting (e.g. "a companion PR may be
   needed") unless a notify or missing-image marker is already present.

For full classification details, label semantics, companion PR review
guidance, and the companion lifecycle, see
[companion-pr-review](../companion-pr-review/SKILL.md).

## Lock-file-only PRs

Skip review for PRs that only update lock files (`package-lock.json`,
`yarn.lock`). Approve without sub-agents and apply `ready-for-merge` if CI
passes. If the diff also touches non-lock files, fall through to normal review.

## Upstream / downstream hygiene

This repo is upstream. Diffs must not name specific downstream consumers.

```bash
# Flag only occurrences introduced by this PR (covers .github/, .tekton/, etc.).
# Allow AGENTS.md and this skill (they document the ban by example).
git diff origin/main...HEAD -- . ':!AGENTS.md' ':!skills/pr-review/**' \
  | rg -n '^\+.*infra-deployments'
```

If that prints matches:

- **Request changes** — replace with generic phrasing ("in some environments",
  "by external policies", "legacy / external consumers") that still flags
  possible downstream impact without naming a consumer.
- Do **not** accept "based on" / "copied from" comments that link a named
  downstream repo.

Also flag other named consumer repos or internal deployment URLs introduced
without a clear upstream need.

## Test framework consistency

When a PR touches test files, verify framework and assertion consistency with the
target file and its neighbors. The repo intentionally uses mixed styles
(Ginkgo/Gomega, `testing.T`+Gomega, testify, plain `testing.T`) across different
packages — see AGENTS.md § Code Style for the locality rule. Do not request
framework conversion unless the PR itself introduces an inconsistent mix
**within** a package that has no precedent for that style.

Apply [ginkgo-testing](../ginkgo-testing/SKILL.md) patterns only to tests that
use Ginkgo/Gomega.

## Also apply when relevant

| Diff touches | Skill / rule |
|--------------|--------------|
| `go.mod` / Go pins | [go-toolchain-upgrade](../go-toolchain-upgrade/SKILL.md) |
| MintMaker/Renovate companion flow | [companion-pr-review](../companion-pr-review/SKILL.md) |
| Ginkgo tests | [ginkgo-testing](../ginkgo-testing/SKILL.md) (applies only when Ginkgo/Gomega is the established style) |
| `operator/upstream-kustomizations/` | Rebuild manifests; [update-upstream-deps](../update-upstream-deps/SKILL.md) |
