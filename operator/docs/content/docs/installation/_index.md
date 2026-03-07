---
title: "Installation"
linkTitle: "Installation"
weight: 1
description: "Installing Konflux on Kubernetes clusters — local development, production, OLM, and from source."
---

This section covers all supported ways to install Konflux.

## Before you begin

Make sure the following conditions are met:

- A Kubernetes cluster is available, or you can create one locally with [Kind](https://kind.sigs.k8s.io/).
- The `kubectl` command-line tool is configured to communicate with your cluster.
- You have cluster-admin permissions.

## Installation methods

| Page | Description |
|------|-------------|
| [Local Deployment (Kind)]({{< relref "install-local" >}}) | Deploy Konflux locally on macOS or Linux using Kind |
| [Building and Installing from Source]({{< relref "install-from-source" >}}) | Contributing to the operator or running a custom build |
| [Installing on Kubernetes]({{< relref "install-kubernetes" >}}) | Production deployment on any Kubernetes cluster (EKS, GKE, OpenShift, etc.) |
| [GitHub Application Secrets]({{< relref "github-secrets" >}}) | Creating a GitHub App and deploying its credentials |
| [Troubleshooting]({{< relref "troubleshooting" >}}) | Solutions to common installation issues |
