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

- **Selective deployment** - every component has an `enabled` field that controls
  whether it is deployed. Most components default to enabled; image controller,
  internal registry, and telemetry default to disabled (opt-in). Disabled components
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

All components support selective deployment via their `enabled` field in the `Konflux`
CR spec. Components marked _(opt-in)_ default to disabled and must be explicitly enabled;
all others default to enabled but can be disabled.

| Component | Default | Description |
|---|---|---|
| **UI** | enabled | The Konflux web interface, Caddy reverse proxy, and [Dex](https://dexidp.io/) identity provider. Supports NodePort, Ingress, and OpenShift Route. |
| **Build Service** | enabled | Controller that manages Tekton-based build pipelines for application components. |
| **Integration Service** | enabled | Controller that triggers integration test pipelines after each build and evaluates their results. |
| **Release Service** | enabled | Controller that manages the `Release`, `ReleasePlan`, and `ReleasePlanAdmission` lifecycle for publishing artifacts. |
| **Application API** | enabled | CRDs and controllers for `Application`, `Component`, `Snapshot`, and related platform resources. |
| **Conforma** | enabled | Policy evaluation engine used to gate releases against supply chain compliance rules. |
| **Namespace Lister** | enabled | Service that enumerates tenant namespaces for the UI workspace selector. |
| **RBAC** | enabled | Cluster roles (`konflux-admin-user-actions`, `konflux-maintainer-user-actions`, `konflux-contributor-user-actions`) and bindings. |
| **Konflux Info** | enabled | ConfigMap-based cluster configuration consumed by PipelineRuns (OIDC issuer, signing URLs, environment metadata). |
| **CLI** | enabled | CLI tooling resources for the Konflux platform. |
| **Cert Manager** | enabled | Creates a `ClusterIssuer` for TLS certificates across platform components. |
| **Default Tenant** | enabled | A shared `default-tenant` namespace where all authenticated users have maintainer access (convenient for development; disable for strict multi-tenancy). |
| **Image Controller** _(opt-in)_ | disabled | Automatically provisions Quay.io image repositories when components are onboarded via the UI. |
| **Internal Registry** _(opt-in)_ | disabled | An in-cluster OCI registry for local or air-gapped environments. |
| **Telemetry** _(opt-in)_ | disabled | Segment bridge for usage telemetry reporting. |

To learn more about configuring specific components, see the [Guides]({{< relref "guides" >}}) section.

To get started with an installation, see [Installation]({{< relref "installation" >}}).
