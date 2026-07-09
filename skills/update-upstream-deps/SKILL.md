---
name: update-upstream-deps
description: Update upstream Konflux component versions. Use when bumping component versions, updating refs, or syncing to latest upstream.
---

# Update Upstream Dependencies

## How Versions Are Pinned

Each `operator/upstream-kustomizations/<component>/core/kustomization.yaml` pins:
- `?ref=<SHA>` — git commit
- `newTag: <SHA>` — image tag (must match)

## Component → Upstream Repo

| Component | Upstream |
|-----------|----------|
| build-service | `konflux-ci/build-service` |
| integration | `konflux-ci/integration-service` |
| release | `konflux-ci/release-service` |
| image-controller | `konflux-ci/image-controller` |
| application-api | `redhat-appstudio/application-api` |
| enterprise-contract | `conforma/crds` |

Other components (cli, ui, registry, namespace-lister, etc.) have local-only configs or different structures.

## Update Methods

### Automated (weekly)

`.github/workflows/update-upstream-manifests.yaml` runs weekly on Mondays at 02:00 UTC; use `workflow_dispatch` for an on-demand run.

Renovate also proposes ref/tag bumps via `renovate.json`. MintMaker/Renovate pin-only PRs may spawn a manifest companion PR — see [companion-pr-review](../companion-pr-review/SKILL.md) for review/skip rules (agents); see `CONTRIBUTING.md` for maintainer merge guidance.

### Single Component (local)

Requires: `gh` CLI authenticated, `kustomize`

```bash
./operator/pkg/manifests/process-component.sh build-service "$(pwd)"
```

In local mode: updates refs, rebuilds manifests, extracts envtest CRDs, reports changes.

### All Components

```bash
./operator/pkg/manifests/process-all-components.sh "$(pwd)"
```

### Manual

1. Get latest SHA from upstream repo
2. Edit `operator/upstream-kustomizations/<component>/core/kustomization.yaml`:
   - Update `?ref=<SHA>`
   - Update `newTag: <SHA>`
3. Rebuild:
   ```bash
   kustomize build operator/upstream-kustomizations/<component> > \
     operator/pkg/manifests/<component>/manifests.yaml
   ```

## Scripts

| Script | Purpose |
|--------|---------|
| `operator/pkg/manifests/update-upstream-refs.sh` | Resolves latest SHA |
| `operator/pkg/manifests/process-component.sh` | Single component update |
| `operator/pkg/manifests/process-all-components.sh` | All components |

## Regenerating Manifests After Source Changes

Files under `operator/upstream-kustomizations/<component>/` are **source inputs** to `kustomize build`. The rendered output lives at `operator/pkg/manifests/<component>/manifests.yaml`. After modifying any file in the `upstream-kustomizations/` tree (scripts, patches, `kustomization.yaml`, etc.) you **must** regenerate the corresponding rendered manifests before committing.

**Rebuild a single component** (same command as the Manual method above):

```bash
kustomize build operator/upstream-kustomizations/<component> \
  > operator/pkg/manifests/<component>/manifests.yaml
```

**Rebuild all components** (when changes span multiple components):

```bash
./operator/pkg/manifests/rebuild-upstream-manifests.sh "$(pwd)"
```

> **Note:** `process-component.sh` also runs `update-upstream-refs.sh` before building, which may overwrite manual edits to kustomization files. Use the raw `kustomize build` command above when you only need to regenerate manifests after a local edit.

The CI check **`verify-manifests-in-sync`** will fail if rendered manifests are out of sync with their source kustomizations.

## Note: Pipeline Bundles

`build-pipeline-config.yaml` contains image digests (not git SHAs). Managed separately by Renovate.
