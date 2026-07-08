---
name: companion-pr-review
description: Review policy for automated manifest companion PRs. Use when reviewing a PR on a bot/manifest-companion-pr-* branch or a Renovate dependency PR with the deps-only label.
---

# Companion PR Review

## What Are Companion PRs?

When Renovate or MintMaker updates upstream pins in
`operator/upstream-kustomizations/` or bumps Helm chart versions in
`.github/scripts/export-third-party-chart-env.sh`, the
`renovate-manifest-companion.yaml` workflow runs
`renovate-manifest-companion.sh` to regenerate rendered manifests and
open a companion PR.

Companion PRs carry the regenerated output so that `main` always has
matching pins and rendered manifests. The source dependency PR receives
`deps-only` and `superseded-by-companion` labels; the companion PR
should be merged instead.

## Identifying Companion PRs

- **Branch pattern:** `bot/manifest-companion-pr-<source-pr-number>`
- **Title pattern:** `chore: sync rendered manifests (#<N>: <dep hint>)`
- **Labels on companion:** `automated`, `dependencies`
- **Labels on source PR:** `deps-only`, `superseded-by-companion`
- **Author:** `github-actions[bot]`

## Expected Diff Scope

A companion PR branches from the source Renovate PR head, so its diff
against `main` includes both the companion-committed paths and the
source PR paths.

### Companion-committed paths (regenerated output)

- `operator/pkg/manifests/*/manifests.yaml`
- `operator/test/crds/` (envtest CRD extracts)
- `dependencies/cert-manager/cert-manager.yaml`
- `dependencies/trust-manager/trust-manager.yaml`
- `dependencies/prometheus-operator-crds/servicemonitors.monitoring.coreos.com.yaml`

### Source PR paths (from the Renovate dependency bump)

These files are changed by Renovate on the source branch and appear in
the companion diff against `main`:

- `operator/upstream-kustomizations/**` (upstream ref/digest pins)
- `.github/scripts/export-third-party-chart-env.sh` (Helm chart
  version pins)
- `dependencies/registry/kustomization.yml` (registry digest pins,
  bumped by MintMaker alongside `operator/upstream-kustomizations/registry`)

### Workflow allowlist

The companion workflow's `Verify PR diff allowlist` step enforces that
the source PR only touches the paths above. If a file outside the
allowlist is modified, the workflow fails and no companion PR is
created.

## Validation Criteria

1. **SHA consistency** -- the `?ref=<SHA>` and `newTag: <SHA>` values
   in `operator/upstream-kustomizations/<component>/core/kustomization.yaml`
   must match. Each component pins a git commit and the corresponding
   container image tag to the same SHA.
2. **Rendered manifest correctness** -- CI runs the
   `verify-manifests-in-sync` workflow, which rebuilds all manifests
   from source kustomizations and Helm charts and fails if the output
   differs. This independently validates that the companion commit
   contains the correct rendered output.
3. **Image availability** -- the companion script runs
   `verify-image-refs.sh` before committing. If upstream images are not
   yet published, the companion PR is not created and the source PR
   receives the `pending-upstream-image` label.

## Review Depth Guidance

**When the diff matches expected patterns** (all changed files fall
within the paths listed above):

- Approve with minimal review depth.
- Confirm file paths are limited to expected scope.
- Verify the companion PR title references the correct source PR
  number.
- No need to review individual manifest line changes -- CI
  `verify-manifests-in-sync` validates correctness mechanically.

**Escalate to a human reviewer when:**

- Files outside the expected paths appear in the diff.
- The companion PR modifies Go source code, shell scripts (other than
  the allowlisted `export-third-party-chart-env.sh`), workflow files,
  or documentation.
- The `verify-manifests-in-sync` CI check fails.
- The source PR does not carry the `deps-only` label despite being a
  dependency bump.

## Label Semantics

| Label | Applied by | Meaning |
|-------|-----------|---------|
| `deps-only` | companion workflow | Source PR should not be merged directly; a companion PR with regenerated manifests is needed |
| `superseded-by-companion` | companion workflow | A companion PR exists; prefer merging the companion |
| `pending-upstream-image` | companion workflow | Upstream container images are not yet available; companion PR creation is blocked |
| `automated` | companion workflow | Applied to the companion PR itself |
| `dependencies` | companion workflow | Applied to the companion PR itself |

**Agent rules for these labels:**

- Agents must never apply `deps-only`, `superseded-by-companion`, or
  `pending-upstream-image`. These are set exclusively by the companion
  workflow.
- When reviewing a PR with `deps-only`: withhold approval and note
  that a companion PR is needed.
- When reviewing a PR with `superseded-by-companion`: withhold
  approval and note that the companion PR should be merged instead.
