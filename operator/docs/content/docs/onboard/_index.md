---
title: "Onboard a new Application to Konflux"
linkTitle: "Onboard a new Application"
weight: 3
description: "A step-by-step tutorial for onboarding an application to Konflux, running integration tests, and configuring releases."
---

This tutorial walks you through the full lifecycle of onboarding a containerized
application to Konflux: from creating a build pipeline, through running integration
tests, to publishing a release to a container registry.

{{< alert color="info" >}}
All commands shown in this tutorial assume you are in the repository root.
{{< /alert >}}

## Tutorial Steps

| Step | Description |
|---|---|
| [Onboarding]({{< relref "onboarding" >}}) | Fork an example repository, onboard it to Konflux via the UI or Kubernetes manifests, observe the build pipeline, and pull the resulting image. |
| [Integration Tests]({{< relref "integration" >}}) | Configure Enterprise Contract integration tests to automatically validate your builds after each pipeline run. |
| [Configure Releases]({{< relref "release" >}}) | Set up `ReleasePlan` and `ReleasePlanAdmission` resources to publish your application to a container registry. |

## Prerequisites

Before starting, make sure you have:

- A running Konflux deployment. See the [Installation]({{< relref "../installation" >}}) guide.
- Access to a GitHub account where you can fork repositories and create a GitHub App.
- (For Option 1 — UI onboarding) A [Quay.io](https://quay.io) account with an
  organization for image auto-provisioning.
