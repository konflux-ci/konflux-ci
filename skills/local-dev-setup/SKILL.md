---
name: local-dev-setup
description: Set up local Konflux development environment on Kind. Use when deploying locally, creating Kind cluster, or troubleshooting local deployment.
---

# Local Development Setup

Full documentation: `operator/docs/content/docs/installation/install-local.md`

## Quick Start

```bash
cp scripts/deploy-local.env.template scripts/deploy-local.env
# Edit deploy-local.env with GitHub App credentials
./scripts/deploy-local.sh
```

## Required Secrets

Set in `scripts/deploy-local.env`:

| Variable | Description |
|----------|-------------|
| `GITHUB_APP_ID` | Numeric App ID |
| `GITHUB_PRIVATE_KEY` or `GITHUB_PRIVATE_KEY_PATH` | PEM key (inline or path) |
| `WEBHOOK_SECRET` | Webhook secret |

GitHub App setup: `operator/docs/content/docs/guides/github-secrets.md`

## Operator Install Methods

Set `OPERATOR_INSTALL_METHOD`:

| Value | Use |
|-------|-----|
| `release` (default) | Latest GitHub release |
| `build` | Build from source (operator dev) |
| `local` | Kustomize from checkout |
| `none` | Manual: `cd operator && make install && make run` |

## Verification

- **UI:** https://localhost:9443
- **Credentials:** `user1@konflux.dev` / `password` (also `user2@konflux.dev`)
- **Registry:** `localhost:5001` (if `ENABLE_REGISTRY_PORT=1`)

## Podman

```bash
export KIND_EXPERIMENTAL_PROVIDER=podman
./scripts/deploy-local.sh
```

## Key Files

| File | Purpose |
|------|---------|
| `scripts/deploy-local.sh` | Main script |
| `scripts/deploy-local.env.template` | Config template |
| `scripts/setup-kind-local-cluster.sh` | Kind setup |
| `deploy-deps.sh` | Dependencies |
| `scripts/deploy-secrets.sh` | Secrets |

## Optional: Quay Integration

For image-controller, set `QUAY_TOKEN` and `QUAY_ORGANIZATION`, and use a CR that enables it:
```bash
KONFLUX_CR=operator/config/samples/konflux-e2e.yaml ./scripts/deploy-local.sh
```
