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
