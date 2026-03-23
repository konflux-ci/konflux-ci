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

- **Optional components** - features such as the image controller, internal registry,
  default tenant namespace, and telemetry can be enabled or disabled via spec flags.
  Disabled components are cleaned up automatically - no orphaned resources left behind.

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
2. The operator's main reconciler reads the CR and **fans out** to a set of child CRs -
   one per platform component.
3. Each child CR has its own reconciler that applies the actual Kubernetes workloads
   for that component.
4. Changes to the `Konflux` CR propagate automatically. Optional components are removed
   when disabled.
5. The `Konflux` CR status reflects the aggregated health of all components and exposes
   the UI URL once ingress is ready.

## Managed Components

The operator manages the following platform components through the `Konflux` CR:

| Component | Description |
|---|---|
| **UI** | The Konflux web interface, nginx reverse proxy, and [Dex](https://dexidp.io/) identity provider. Supports NodePort, Ingress, and OpenShift Route. |
| **Build Service** | Controller that manages Tekton-based build pipelines for application components. |
| **Integration Service** | Controller that triggers integration test pipelines after each build and evaluates their results. |
| **Release Service** | Controller that manages the `Release`, `ReleasePlan`, and `ReleasePlanAdmission` lifecycle for publishing artifacts. |
| **Application API** | CRDs and controllers for `Application`, `Component`, `Snapshot`, and related platform resources. |
| **Conforma** | Policy evaluation engine used to gate releases against supply chain compliance rules. |
| **Namespace Lister** | Service that enumerates tenant namespaces for the UI workspace selector. |
| **RBAC** | Cluster roles (`konflux-admin-user-actions`, `konflux-maintainer-user-actions`, `konflux-contributor-user-actions`) and bindings. |
| **Konflux Info** | ConfigMap-based cluster configuration consumed by PipelineRuns (OIDC issuer, signing URLs, environment metadata). |
| **Cert Manager** _(optional)_ | Creates a `ClusterIssuer` for TLS certificates across platform components. |
| **Image Controller** _(optional)_ | Automatically provisions Quay.io image repositories when components are onboarded via the UI. |
| **Internal Registry** _(optional)_ | An in-cluster OCI registry for local or air-gapped environments. |
| **Default Tenant** _(optional)_ | A shared `default-tenant` namespace where all authenticated users have maintainer access (convenient for development; disable for strict multi-tenancy). |
| **Telemetry** _(optional)_ | Segment bridge for usage telemetry reporting. |

To learn more about configuring specific components, see the [Guides]({{< relref "guides" >}}) section.

To get started with an installation, see [Installation]({{< relref "installation" >}}).
