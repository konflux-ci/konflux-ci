---
name: pr-review
description: >-
  Use when reviewing pull requests in konflux-ci/konflux-ci. Covers
  upstream/downstream hygiene and other repo-wide review checks that are easy
  to miss in a focused feature review.
---

# PR Review

Apply these checks on human-authored PRs (skip companion-eligible MintMaker/Renovate
parents — see [companion-pr-review](../companion-pr-review/SKILL.md)).

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

## Also apply when relevant

| Diff touches | Skill / rule |
|--------------|--------------|
| `go.mod` / Go pins | [go-toolchain-upgrade](../go-toolchain-upgrade/SKILL.md) |
| MintMaker/Renovate companion flow | [companion-pr-review](../companion-pr-review/SKILL.md) |
| Ginkgo tests | [ginkgo-testing](../ginkgo-testing/SKILL.md) |
| `operator/upstream-kustomizations/` | Rebuild manifests; [update-upstream-deps](../update-upstream-deps/SKILL.md) |
