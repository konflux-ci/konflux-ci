# Legacy Konflux Manifests

> [!WARNING]
> This directory contains **legacy deployment manifests** and will be removed in a future release.
>
> **For new deployments, use the [Konflux Operator](../docs/operator-deployment.md) instead.**

These manifests are used by `deploy-konflux.sh` for non-operator deployments. They remain
for compatibility with environments (e.g., Fedora cluster) that still use this method.

## Migration

To migrate to the operator-based deployment:

1. See [Operator Deployment Guide](../docs/operator-deployment.md)
2. For local development, use `./scripts/deploy-local.sh`
