---
name: companion-pr-review
description: >-
  Use when reviewing, triaging, or retro-analyzing MintMaker/Renovate dependency
  PRs and their manifest companion PRs in konflux-ci/konflux-ci. Covers which
  PRs to skip, which to review lightly, label timing, and expected lifecycle.
---

# Manifest Companion PRs

## Overview

When MintMaker/Renovate bumps pins under `operator/upstream-kustomizations/`,
`.github/scripts/export-third-party-chart-env.sh`, or shared dependencies in
`operator/go.mod` / `operator/go.sum`, a **parent PR** opens with pin-only
changes. A separate workflow
(`.github/workflows/renovate-manifest-companion.yaml`) asynchronously opens a
**companion PR** that adds regenerated manifests.

When a companion PR is created (`superseded-by-companion` or notify marker),
**merge the companion, not the parent**. When the companion workflow reports
noop (`<!-- konflux-manifest-companion-noop:N -->`), **merge the parent** —
no companion is needed.

Parent PRs that stay open after a companion merges are expected. MintMaker
closes them on its next run when the dependency is superseded — do not propose
workflows to close them earlier.

**Note:** Lock-file-only PRs are not companion-eligible — see
[pr-review](../pr-review/SKILL.md).

## Classify the PR first

| Type | How to recognize | Review? | Approve? | ready-for-merge? |
|------|------------------|---------|----------|------------------|
| **Parent (companion-eligible)** | See heuristics below | **Skip** | **No** | **No** |
| **Parent (noop)** | `<!-- konflux-manifest-companion-noop:N -->` comment | Brief note only | Yes, if CI green | Yes |
| **Parent (pending image)** | `pending-upstream-image` label or missing-image marker | **Skip** | **No** | **No** |
| **Companion** | Branch `bot/manifest-companion-pr-*`, author `konflux-ci-update-bot` | **Yes, minimal** | Yes if scope OK | Yes after approval |

## Parent PR heuristics (use before labels land)

### Classification algorithm

Follow these steps in order to decide whether a parent PR is
companion-eligible:

1. **Check author.** Is the PR author `renovate[bot]` or
   `red-hat-konflux[bot]`? If not → **not companion-eligible**, review
   normally.
2. **List every changed file** in the PR diff.
3. **Compare each file against the companion allowlist below.** If
   **every** changed file appears on the allowlist → the PR is
   **companion-eligible**. Skip review — do not dispatch sub-agents, do
   not approve, do not add `ready-for-merge`.
4. If **any** changed file is not on the allowlist → **not
   companion-eligible**, proceed with normal review.

### Companion allowlist

These paths are unconditionally allowed. A PR whose diff is limited to
these paths (and whose author passes step 1) is companion-eligible:

- `operator/upstream-kustomizations/**`
- `.github/scripts/export-third-party-chart-env.sh`
- `dependencies/registry/kustomization.yml`
- `operator/go.mod`
- `operator/go.sum`
- `test/go-tests/go.mod`
- `test/go-tests/go.sum`

#### Why each path is on the list

- **`operator/upstream-kustomizations/**`** — pin-only digest/SHA/tag
  changes that trigger a companion PR with regenerated manifests.
- **`.github/scripts/export-third-party-chart-env.sh`** — chart version
  bumps that trigger companion manifest regeneration.
- **`dependencies/registry/kustomization.yml`** — registry digest bumps
  only. In practice MintMaker always bumps this file together with
  `operator/upstream-kustomizations/registry`, so it does not appear
  alone in a PR diff.
- **`operator/go.mod` / `operator/go.sum`** — OpenShift envtest CRDs
  derived from `github.com/openshift/api`; the companion workflow
  regenerates the corresponding test CRDs.
- **`test/go-tests/go.mod` / `test/go-tests/go.sum`** — these files do
  not affect manifest regeneration and are not in the companion
  workflow's trigger list. Renovate bumps them in two scenarios:
  1. **Shared dependency** (e.g. `application-api`) updated in both Go
     modules — the PR also changes `operator/go.mod` and
     `operator/go.sum`, which ARE companion workflow triggers, so a
     companion PR is created normally. The test/go-tests files are
     carried along.
  2. **Test-only dependency** updated independently — the PR changes
     only `test/go-tests` files. No companion PR is created and no noop
     marker is posted; these PRs are resolved by Renovate auto-merge or
     MintMaker closure.

### Examples

- A PR by `red-hat-konflux[bot]` changing only `test/go-tests/go.mod`
  and `test/go-tests/go.sum` → all files are on the allowlist →
  **companion-eligible, skip review.** (No companion PR is created for
  test-only changes; the PR is resolved by auto-merge or closure — see
  the rationale above.)
- A PR by `renovate[bot]` changing `operator/go.mod`,
  `operator/go.sum`, `test/go-tests/go.mod`, and
  `test/go-tests/go.sum` → all files are on the allowlist →
  **companion-eligible, skip review.**
- A PR by `renovate[bot]` changing `operator/docs/go.mod` → file is
  **not** on the allowlist → **not companion-eligible, review normally.**
- A PR by a human author changing
  `operator/upstream-kustomizations/pipeline-service/kustomization.yaml`
  → author is not a bot (step 1 fails) → **not companion-eligible,
  review normally.**

### Applying the classification

Apply this **even before** `deps-only` / `superseded-by-companion` labels land.
Do not run full review, do not approve, do not add `ready-for-merge`.
Do not add `requires-manual-review` — the parent is not a merge target and
does not need human escalation.

**Exception:** once a noop marker appears, follow the **Parent (noop)** row in
the table above instead.

## Check companion workflow state before commenting

Before asserting that a companion PR will be created, check existing PR
comments for HTML markers posted by `renovate-manifest-companion.sh`:

| Marker | Meaning |
|--------|---------|
| `<!-- konflux-manifest-companion-notify:N -->` | Companion PR exists — review and merge it |
| `<!-- konflux-manifest-companion-noop:N -->` | No manifest diff; parent may merge directly |
| `<!-- konflux-manifest-companion-missing-image:N -->` | Upstream image not published; companion blocked |

If no marker exists yet, use hedged language (e.g. "a companion PR may be
needed") — the companion workflow may still be running.

## Companion PR review (minimal depth)

Companion PRs are generated by `renovate-manifest-companion.sh`. Expected diff:

- `operator/pkg/manifests/*/manifests.yaml` (re-rendered)
- `dependencies/cert-manager/cert-manager.yaml`
- `dependencies/trust-manager/trust-manager.yaml`
- `dependencies/prometheus-operator-crds/servicemonitors.monitoring.coreos.com.yaml`
- `operator/test/crds/` (envtest CRDs)
- Plus parent pin paths (companion branch includes parent commits)

**Also expect** parent source paths in the diff against `main` — this is
normal, not a scope violation.

Checklist:

1. Diff is mechanical (digest/tag/SHA changes, rendered manifest churn)
2. CI `verify-manifests-in-sync` passes
3. Parent PR is linked in title (`#NNN:`) or body
4. Unexpected structural changes (new resources, deleted fields) → escalate to human

When the checklist passes: approve with minimal depth. Do not spawn sub-agents
to analyze every manifest line.

## Label semantics

**Companion workflow only** (agents must never apply):

- `deps-only` / `superseded-by-companion` — parent is not mergeable; find and
  review the companion PR instead
- `pending-upstream-image` — companion blocked; wait for upstream publish

**Review agents only** (no deterministic automation sets this):

- `ready-for-merge` — applied by the review agent when it approves. Do not add
  on companion-eligible parents, on parents carrying any label above, or before
  checking companion workflow comments for a noop result.

## Expected lifecycle (not waste)

- Parent stays open after companion merge until MintMaker's next update cycle
- Parent may close and reopen when the dependency bumps again
- Multiple companion PRs per parent over time is normal (dependency churn)
- Do **not** file issues proposing auto-close workflows for parents

## Retro agent guidance

When a closed/merged PR is a companion-eligible parent:

- Do not flag "PR closed without merge" as waste
- Do not flag "open parent after companion merged" as actionable
- Do not propose closing parents via new workflows
- Do not flag `requires-manual-review` label on superseded parents as actionable — if present, it was applied in error by the review agent before this guidance was added
- Do flag: review agent ran on parent after `superseded-by-companion` was applied
