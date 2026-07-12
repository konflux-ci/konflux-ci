---
name: companion-pr-review
description: >-
  Use when reviewing, triaging, or retro-analyzing MintMaker/Renovate dependency
  PRs and their manifest companion PRs in konflux-ci/konflux-ci. Covers which
  PRs to skip, which to review lightly, label timing, and expected lifecycle.
---

# Manifest Companion PRs

## Overview

When MintMaker/Renovate bumps pins under `operator/upstream-kustomizations/`
or `.github/scripts/export-third-party-chart-env.sh`, a **parent PR** opens
with pin-only changes. A separate workflow
(`.github/workflows/renovate-manifest-companion.yaml`) asynchronously opens a
**companion PR** that adds regenerated manifests.

When a companion PR is created (`superseded-by-companion` or notify marker),
**merge the companion, not the parent**. When the companion workflow reports
noop (`<!-- konflux-manifest-companion-noop:N -->`), **merge the parent** —
no companion is needed.

Parent PRs that stay open after a companion merges are expected. MintMaker
closes them on its next run when the dependency is superseded — do not propose
workflows to close them earlier.

## Classify the PR first

| Type | How to recognize | Review? | Approve? | ready-for-merge? |
|------|------------------|---------|----------|------------------|
| **Parent (companion-eligible)** | See heuristics below | **Skip** | **No** | **No** |
| **Parent (noop)** | `<!-- konflux-manifest-companion-noop:N -->` comment | Brief note only | Yes, if CI green | Yes |
| **Parent (pending image)** | `pending-upstream-image` label or missing-image marker | **Skip** | **No** | **No** |
| **Companion** | Branch `bot/manifest-companion-pr-*`, author `konflux-ci-update-bot` | **Yes, minimal** | Yes if scope OK | Yes after approval |

## Parent PR heuristics (use before labels land)

Skip review and retro when **both**:

1. Author is `renovate[bot]` or `red-hat-konflux[bot]`
2. Diff is limited to the companion allowlist:
   - `operator/upstream-kustomizations/**`
   - `.github/scripts/export-third-party-chart-env.sh`
   - `dependencies/registry/kustomization.yml` (registry digest bumps only)

Apply this **even before** `deps-only` / `superseded-by-companion` labels land.
Do not run full review, do not approve, do not add `ready-for-merge`.
Do not add `requires-manual-review` — the parent is not a merge target and
does not need human escalation.

**Exception:** once a noop marker appears, follow the **Parent (noop)** row in
the table above instead.

**Not companion-eligible** (review normally):

- Changes to `test/go-tests/go.mod`, `operator/docs/go.mod`, `operator/go.mod`
- Non-allowlist paths
- Human-authored PRs touching upstream kustomizations

### Reviewing `dependencies/` kustomization patches on version bumps

Several dependencies under `dependencies/` use kustomize overlays with JSON
Patch operations (`op: remove`, `op: replace`, `op: add`) that target absolute
JSON Pointer paths in upstream manifests (e.g.,
`/spec/template/spec/initContainers/0/securityContext/runAsUser`). These
patches assume specific upstream manifest structure — container ordering,
field presence, and resource naming. If an upstream version restructures its
manifests, the patches silently break at deploy time with a kustomize error.

**`verify-manifests-in-sync` CI does not cover `dependencies/` changes.** That
check only validates `operator/pkg/manifests/` content. Structural
incompatibilities in `dependencies/` patches surface only at cluster deploy
time, so extra review scrutiny is warranted on major or minor version bumps.

When a Renovate PR bumps a version in `dependencies/*/kustomization.yaml` (or
`.yml`) and the file contains JSON Patch operations, check:

1. **Patch compatibility** — verify that every `op: remove` and `op: replace`
   path still exists in the new upstream manifest. Absolute paths like
   `/spec/template/spec/containers/0/...` are fragile if upstream reorders
   containers, removes security context fields, or renames resources.
2. **Stale version comments** — flag inline comments that reference the old
   version (e.g., `# upstream 1.18.1 sets runAsUser/runAsGroup 65534`).
   Update or remove them so they reflect the new version.
3. **Patch type risk** — `op: remove` and `op: replace` fail hard if the
   target path is absent; `op: add` is safer (creates the path if missing).
   Weight review effort toward `remove`/`replace` patches.

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
