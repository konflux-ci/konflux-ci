---
title: "Tenant and Managed Namespace Management"
linkTitle: "Namespace Management"
weight: 5
description: "How to create and configure tenant namespaces and managed namespaces in Konflux."
---

Konflux organizes workloads into two types of namespaces: **tenant namespaces** where
teams develop and build applications, and **managed namespaces** where release pipelines
deploy applications. This guide explains how to create, label, and configure both types.

## Namespace types

| Type | Purpose | Label |
|------|---------|-------|
| **Tenant namespace** | Development and build workloads for a team | `konflux-ci.dev/type: tenant` |
| **Managed namespace** | Release pipeline deployments managed by an ops team | `konflux-ci.dev/type: tenant` |
| **Default tenant** | Shared namespace for all authenticated users (dev/test only) | Managed by the operator |

Both tenant and managed namespaces require the `konflux-ci.dev/type: tenant` label so
that the namespace lister component can discover them and make them available in the
Konflux UI.

## Tenant namespaces

A tenant namespace is the workspace for a development team. Applications, components,
integration tests, and release plans are all created inside tenant namespaces.

### Default tenant namespace

By default, the Konflux operator creates a `default-tenant` namespace where all
authenticated users automatically receive maintainer permissions. This is convenient
for local development and testing, but not appropriate for production multi-tenant
environments where strict namespace isolation is required.

Set `spec.defaultTenant.enabled` in the `Konflux` CR to control this behaviour:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  defaultTenant:
    enabled: true   # true (default): creates default-tenant shared namespace
                    # false: disables it; create per-team namespaces instead
```

When set to `false`, create dedicated per-team namespaces as described in the
[Creating a tenant namespace](#creating-a-tenant-namespace) section below.

### Creating a tenant namespace

1. Create the namespace:

```bash
kubectl create namespace <namespace-name>
```

2. Label it so the namespace lister can discover it:

```bash
kubectl label namespace <namespace-name> konflux-ci.dev/type=tenant
```

Alternatively, use a Kubernetes manifest:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-team-tenant
  labels:
    konflux-ci.dev/type: tenant
```

### Granting users access

Konflux provides three ClusterRoles for namespace access:

| ClusterRole | Description |
|-------------|-------------|
| `konflux-admin-user-actions` | Full access to all Konflux resources including secrets |
| `konflux-maintainer-user-actions` | Partial access to Konflux resources without access to secrets |
| `konflux-contributor-user-actions` | View access to Konflux resources without access to secrets |

> **Note:** Grant `konflux-admin-user-actions` only to users who need access to
> secrets (e.g. namespace owners or cluster administrators). For most team members,
> prefer `konflux-maintainer-user-actions` for day-to-day development work, or
> `konflux-contributor-user-actions` for read-only access.

Grant a user admin access to a tenant namespace:

```bash
kubectl create rolebinding <rolebinding-name> \
  --clusterrole konflux-admin-user-actions \
  --user <user-email> \
  -n <namespace-name>
```

Grant a user maintainer access:

```bash
kubectl create rolebinding <rolebinding-name> \
  --clusterrole konflux-maintainer-user-actions \
  --user <user-email> \
  -n <namespace-name>
```

Grant a user contributor (read-only) access:

```bash
kubectl create rolebinding <rolebinding-name> \
  --clusterrole konflux-contributor-user-actions \
  --user <user-email> \
  -n <namespace-name>
```

### Example: creating a team tenant namespace

```bash
kubectl create namespace my-team-tenant
kubectl label namespace my-team-tenant konflux-ci.dev/type=tenant

# Grant admin access to a team member
kubectl create rolebinding my-team-admin \
  --clusterrole konflux-admin-user-actions \
  --user developer@example.com \
  -n my-team-tenant
```

Or using Kubernetes manifests:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-team-tenant
  labels:
    konflux-ci.dev/type: tenant
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: my-team-admin
  namespace: my-team-tenant
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: konflux-admin-user-actions
subjects:
- kind: User
  name: developer@example.com
  apiGroup: rbac.authorization.k8s.io
```

### Granting multiple users access

You can bind multiple users to the same namespace either with individual `RoleBinding`
resources or by listing multiple subjects in a single binding:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: my-team-admins
  namespace: my-team-tenant
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: konflux-admin-user-actions
subjects:
- kind: User
  name: alice@example.com
  apiGroup: rbac.authorization.k8s.io
- kind: User
  name: bob@example.com
  apiGroup: rbac.authorization.k8s.io
```

## Managed namespaces

A managed namespace is where the release service deploys released applications. It is
typically owned by the managed environment team (the team that supports the deployments
of that application) rather than the development team that builds the application.

The `ReleasePlanAdmission` resource — which defines how and where an application is
released — lives in the managed namespace. The development team creates a `ReleasePlan`
in their tenant namespace that references the managed namespace as the release target.

### Creating a managed namespace

Managed namespaces require the same `konflux-ci.dev/type: tenant` label as tenant
namespaces:

```bash
kubectl create namespace <managed-namespace-name>
kubectl label namespace <managed-namespace-name> konflux-ci.dev/type=tenant
```

Or using a manifest:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-team-managed
  labels:
    konflux-ci.dev/type: tenant
```

### Release pipeline service account

The release pipeline runs under a dedicated service account in the managed namespace.
Create the service account before creating rolebindings that reference it:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: release-pipeline
  namespace: my-team-managed
```

### Granting access to the managed namespace

The managed environment team needs admin access to manage releases, and the
`release-pipeline` service account needs its own rolebinding:

```bash
# Grant the managed environment team admin access
kubectl create rolebinding ops-team-admin \
  --clusterrole konflux-admin-user-actions \
  --user ops@example.com \
  -n my-team-managed

# Grant the release pipeline service account access
kubectl create rolebinding release-pipeline-resource-role-binding \
  --clusterrole release-pipeline-resource-role \
  --serviceaccount my-team-managed:release-pipeline \
  -n my-team-managed
```

### Example: full managed namespace setup

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-team-managed
  labels:
    konflux-ci.dev/type: tenant
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: release-pipeline
  namespace: my-team-managed
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ops-team-admin
  namespace: my-team-managed
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: konflux-admin-user-actions
subjects:
- kind: User
  name: ops@example.com
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: release-pipeline-resource-role-binding
  namespace: my-team-managed
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: release-pipeline-resource-role
subjects:
- kind: ServiceAccount
  name: release-pipeline
  namespace: my-team-managed
```

## Connecting tenant and managed namespaces for releases

Once both namespaces exist, connect them for the release flow:

1. In the **tenant namespace**, the development team creates a `ReleasePlan` that
   points to the managed namespace:

```yaml
apiVersion: appstudio.redhat.com/v1alpha1
kind: ReleasePlan
metadata:
  name: my-app-release-plan
  namespace: my-team-tenant
spec:
  application: my-application
  target: my-team-managed  # The managed namespace
```

2. In the **managed namespace**, the operations team creates a `ReleasePlanAdmission`
   that authorizes the release and defines its pipeline:

```yaml
apiVersion: appstudio.redhat.com/v1alpha1
kind: ReleasePlanAdmission
metadata:
  name: my-app-admission
  namespace: my-team-managed
spec:
  applications:
    - my-application
  origin: my-team-tenant  # The development team's tenant namespace
  pipeline:
    pipelineRef: <pipeline-ref>
    serviceAccountName: release-pipeline
  policy: default
```

When both resources exist and match, the `ReleasePlan` status shows **"Matched"**,
indicating the release flow is ready.
