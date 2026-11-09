---
title: "Overview"
linkTitle: "Overview"
weight: 1
description: "What the Konflux Operator is, how it works, and what it manages."
---

The Konflux Operator is a Kubernetes-native operator that installs, configures, and
manages the [Konflux](https://konflux-ci.dev/docs/) CI/CD platform from a single
declarative Custom Resource.

## Why use the Konflux Operator?

Running a full CI/CD platform involves deploying and wiring together many components:
build controllers, release pipelines, policy engines, identity providers, ingress,
certificates, and more. Keeping them configured consistently and upgrading them safely
adds operational overhead.

The Konflux Operator removes this complexity. You describe your desired platform
configuration in one `Konflux` Custom Resource and the operator continuously reconciles
the cluster toward that state - deploying components, propagating configuration changes,
and cleaning up disabled features automatically.

It works on any Kubernetes cluster: local Kind environments, OpenShift, EKS, GKE, or
any conformant distribution.

## Features Overview

- **Single CR for the entire platform** - one `Konflux` resource controls all
  components. No need to manage individual Helm releases or manifests per service.

- **Declarative lifecycle management** - update the `Konflux` CR to change replicas,
  resource limits, ingress settings, or authentication connectors. The operator
  reconciles the change without manual intervention.

- **Installation types** - the `installationType` field controls which services the
  operator deploys. `standard` (default) deploys all core services; `spoke` deploys
  only Info and RBAC for MultiKueue spoke clusters. Three optional components
  (image controller, internal registry, telemetry) have their own `enabled` field
  and can be toggled independently of the installation type. Disabled components
  are cleaned up automatically - no orphaned resources left behind.

- **Identity provider configuration** - Dex connectors (GitHub, OpenShift, OIDC, LDAP,
  static passwords) are configured directly in the `Konflux` CR, removing the need to
  manage separate Dex configuration files.

- **Resource and replica tuning** - CPU requests, memory limits, and replica counts for
  each component are expressed in the CR and managed declaratively.

- **Status aggregation** - the operator aggregates readiness conditions from all
  components into the parent `Konflux` CR status, giving a single place to check
  platform health.

## High-level Operation

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│  kubectl apply -f my-konflux.yaml                                                │
│                                                                                  │
│                  Konflux CR  ──► Operator reconciler                             │
│                                         │                                        │
│       ┌─────────────────────────────────┼──────────────────────────┐             │
│       ▼                    ▼            ▼                ▼          ▼            │
│   KonfluxUI    KonfluxBuildService  KonfluxIntegration  KonfluxRelease  ...      │
│   reconciler   reconciler           ServiceReconciler   ServiceReconciler        │
│       │                    │                │                  │                 │
│  UI + Dex +      Build Service       Integration Service  Release Service        │
│  Ingress/TLS     + RBAC              + RBAC               + RBAC                 │
└──────────────────────────────────────────────────────────────────────────────────┘
```

1. You apply a single `Konflux` CR to the cluster.
2. The operator's main reconciler reads the CR and **fans out** to child CRs for each
   enabled platform component.
3. Each child CR has its own reconciler that applies the actual Kubernetes workloads
   for that component.
4. Changes to the `Konflux` CR propagate automatically. Optional components are removed
   when disabled.
5. The `Konflux` CR status reflects the aggregated health of all enabled components.
   `status.uiURL` is populated when the UI component is enabled and ingress is ready;
   it is cleared when the UI is disabled. The `CertManagerAvailable` condition is
   present only when the cert-manager component is enabled.

## Managed Components

The operator manages the following platform components through the `Konflux` CR:

The `installationType` field selects which components the operator deploys:
`standard` (default) deploys all core services, while `spoke` deploys only Info and
RBAC. Components marked _(opt-in)_ have their own `enabled` field and can be toggled
independently of the installation type.

| Component | Installation type | Description |
|---|---|---|
| **UI** | standard | The Konflux web interface, Caddy reverse proxy, and [Dex](https://dexidp.io/) identity provider. Supports NodePort, Ingress, and OpenShift Route. |
| **Build Service** | standard | Controller that manages Tekton-based build pipelines for application components. |
| **Integration Service** | standard | Controller that triggers integration test pipelines after each build and evaluates their results. |
| **Release Service** | standard | Controller that manages the `Release`, `ReleasePlan`, and `ReleasePlanAdmission` lifecycle for publishing artifacts. |
| **Application API** | standard | CRDs and controllers for `Application`, `Component`, `Snapshot`, and related platform resources. |
| **Conforma** | standard | Policy evaluation engine used to gate releases against supply chain compliance rules. |
| **Namespace Lister** | standard | Service that enumerates tenant namespaces for the UI workspace selector. |
| **RBAC** | standard + spoke | Cluster roles (`konflux-admin-user-actions`, `konflux-maintainer-user-actions`, `konflux-contributor-user-actions`) and bindings. |
| **Konflux Info** | standard + spoke | ConfigMap-based cluster configuration consumed by PipelineRuns (OIDC issuer, signing URLs, environment metadata). |
| **CLI** | standard | CLI tooling resources for the Konflux platform. |
| **Cert Manager** | standard | Creates a `ClusterIssuer` for TLS certificates across platform components. |
| **Default Tenant** | standard | A shared `default-tenant` namespace where all authenticated users have maintainer access (convenient for development; use `spoke` type for strict multi-tenancy). |
| **Image Controller** _(opt-in)_ | any (independent) | Automatically provisions Quay.io image repositories when components are onboarded via the UI. |
| **Internal Registry** _(opt-in)_ | any (independent) | An in-cluster OCI registry for local or air-gapped environments. |
| **Telemetry** _(opt-in)_ | any (independent) | Segment bridge for usage telemetry reporting. |

To learn more about configuring specific components, see the [Guides]({{< relref "guides" >}}) section.

To get started with an installation, see [Installation]({{< relref "installation" >}}).
