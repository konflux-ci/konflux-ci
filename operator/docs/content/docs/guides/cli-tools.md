---
title: "CLI Tools"
linkTitle: "CLI Tools"
weight: 10
description: "How to download and use the CLI scripts shipped with the operator for tenant and release setup."
---

The Konflux operator ships helper CLI scripts alongside its deployment. These scripts
are stored as ConfigMaps in the `konflux-cli` namespace and are guaranteed to be
compatible with the installed operator version and services.

## Available scripts

| ConfigMap | Script | Purpose |
|-----------|--------|---------|
| `create-tenant` | `create-tenant.sh` | Create a new tenant namespace with all required RBAC resources |
| `setup-release` | `setup-release.sh` | Set up a managed namespace with release pipeline resources |

## Downloading the scripts

Extract a script from its ConfigMap and make it executable:

```bash
kubectl get configmap create-tenant -n konflux-cli -o jsonpath='{.data.create-tenant\.sh}' > create-tenant.sh
chmod +x create-tenant.sh
```

```bash
kubectl get configmap setup-release -n konflux-cli -o jsonpath='{.data.setup-release\.sh}' > setup-release.sh
chmod +x setup-release.sh
```

## create-tenant.sh

Creates a new tenant namespace with a ServiceAccount for integration pipelines and
RoleBindings for both the pipeline runner and an admin user.

```bash
./create-tenant.sh -n <namespace> -u <admin-user>
```

Run `./create-tenant.sh -h` for the full list of options.

## setup-release.sh

Sets up a complete release pipeline for a Konflux application. Creates a managed
namespace with ImageRepositories, copies an EnterpriseContractPolicy, creates a
ReleasePlanAdmission, and wires up a ReleasePlan in the tenant namespace.

```bash
./setup-release.sh -a <application> -t <tenant-namespace> -m <managed-namespace>
```

Run `./setup-release.sh -h` for the full list of options.

## Why scripts are shipped with the operator

Delivering the scripts as part of the operator deployment ensures that the tools
are always compatible with the installed operator version and the services it manages.
Users download the scripts directly from the cluster, so there is no risk of version
mismatch between the CLI tools and the running platform.
