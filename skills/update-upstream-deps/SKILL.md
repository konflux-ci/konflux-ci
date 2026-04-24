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

### Automated (daily)

`.github/workflows/update-upstream-manifests.yaml` runs at 2:00 UTC.

Renovate also proposes ref/tag bumps via `renovate.json`.

### Single Component (local)

Requires: `gh` CLI authenticated, `kustomize`

```bash
./operator/pkg/manifests/process-component.sh build-service "$(pwd)"
```

In local mode: updates refs, rebuilds manifests, reports changes.

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

## Note: Pipeline Bundles

`build-pipeline-config.yaml` contains image digests (not git SHAs). Managed separately by Renovate.
