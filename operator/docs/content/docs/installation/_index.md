---
title: "Installation"
linkTitle: "Installation"
weight: 2
description: "Installing Konflux on Kubernetes clusters - local development, OpenShift, production, OLM, and from source."
---

This section covers all supported ways to install Konflux. Use the table below to find
the right guide for your situation:

| My situation | Guide |
|---|---|
| Quick local development | [Local Deployment (Kind)]({{< relref "install-local" >}}) |
| Existing OpenShift cluster | [Installing on OpenShift]({{< relref "install-openshift" >}}) |
| Any Kubernetes cluster, building the operator from source | [Building and Installing from Source]({{< relref "install-from-source" >}}) |
| Any Kubernetes cluster, release bundle | [Installing from Release]({{< relref "install-release" >}}) |
| Any Kubernetes cluster with OLM installed | [Installing from OLM]({{< relref "install-olm" >}}) |

## Before you begin

Make sure the following conditions are met:

- A Kubernetes cluster is available, or you can create one locally with [Kind](https://kind.sigs.k8s.io/).
- The `kubectl` command-line tool is configured to communicate with your cluster.
- You have cluster-admin permissions.
