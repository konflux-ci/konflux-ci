konflux-operator
===

<!-- toc -->

- [Description](#description)
- [Getting Started](#getting-started)
  * [Prerequisites](#prerequisites)
  * [Install the CRDs](#install-the-crds)
  * [Run the Operator](#run-the-operator)
  * [Create a Konflux Custom Resource](#create-a-konflux-custom-resource)
  * [Development Workflow](#development-workflow)
- [Deploying the Operator to a Cluster](#deploying-the-operator-to-a-cluster)
  * [Build and Push Image](#build-and-push-image)
  * [Install the CRDs](#install-the-crds-1)
  * [Deploy the Manager](#deploy-the-manager)
  * [Create Instances](#create-instances)
- [Uninstalling](#uninstalling)
  * [Delete the instances (CRs) from the cluster:](#delete-the-instances-crs-from-the-cluster)
  * [Delete the APIs(CRDs) from the cluster:](#delete-the-apiscrds-from-the-cluster)
  * [UnDeploy the controller from the cluster:](#undeploy-the-controller-from-the-cluster)
- [Project Distribution](#project-distribution)
  * [By providing a bundle with all YAML files](#by-providing-a-bundle-with-all-yaml-files)
  * [By providing a Helm Chart](#by-providing-a-helm-chart)
- [Contributing](#contributing)
- [License](#license)

<!-- tocstop -->

A Kubernetes operator for declaratively managing Konflux installations and components.

# Description
The Konflux Operator provides a declarative way to install, configure, and manage
Konflux. A cloud-native software factory focused on software supply chain security.

The operator manages all Konflux components (UI, Build Service, Integration Service,
Release Service, and more) through a single `Konflux` Custom Resource, enabling
simplified lifecycle management, upgrades, and configuration.

By using the operator, administrators can deploy and maintain Konflux installations
without manually managing individual component deployments or configuration files.

# Getting Started

## Prerequisites

For **operator development and building**:

- [Go](https://go.dev/) version v1.24.0 or newer
- [Docker](https://www.docker.com/) version 17.03+ or [Podman](https://podman.io/) equivalent
- `kubectl` version v1.11.3 or newer
- `make` (usually available via build-essential or similar packages)
- Access to a Kubernetes v1.11.3+ cluster

> [!NOTE]
> For complete Konflux deployment requirements (including machine resources, Kind setup,
> and additional dependencies), see the
> [main repository's prerequisites](https://github.com/konflux-ci/konflux-ci/blob/main/README.md#installing-software-dependencies).

## Install the CRDs

Install the Custom Resource Definitions (CRDs) into your cluster:

```bash
make install
```

## Run the Operator

Run the operator locally from your terminal. The operator will connect to your cluster
using your `kubectl` configuration:

```bash
make run
```

This will start the operator and connect it to your cluster. The operator will watch for
Konflux Custom Resources and reconcile them.

> [!NOTE]
> The operator runs locally and uses your `kubectl` configuration to connect to
> the cluster. It will automatically detect and reconcile Konflux resources.
> **Keep this terminal open and the process running** while you work with Konflux.

## Create a Konflux Custom Resource

In a separate terminal, deploy Konflux by creating a `Konflux` Custom Resource:

```bash
# Apply the sample Konflux CR
kubectl apply -f config/samples/konflux_v1alpha1_konflux.yaml

# Wait for Konflux to be ready
kubectl wait --for=condition=Ready=True konflux konflux --timeout=10m
```

## Development Workflow

For iterative development:

- After making code changes, stop the operator (Ctrl+C) and restart it: `make run`
- The operator will automatically pick up changes when restarted
- No need to rebuild images or restart deployments when running locally

> [!NOTE]
> Run `make help` for more information on all potential `make` targets.

# Deploying the Operator to a Cluster

If you want to deploy the operator as a deployment in your cluster instead of running
it locally, follow the steps below.

> [!NOTE]
> For most development scenarios, running the operator locally (as described in
> [Getting Started](#getting-started)) is recommended.

To deploy the operator as a deployment in your cluster:

## Build and Push Image

Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/konflux-operator:tag
```

> [!NOTE]
> This image ought to be published in the personal registry you specified.
> So it is required to have access to push the image to the registry and to pull it
> to the working environment. Make sure you have the proper permission to the registry
> if the above commands don't work.

## Install the CRDs

Install the CRDs into the cluster (same as [Install the CRDs](#install-the-crds) above):

```sh
make install
```

## Deploy the Manager

Deploy the Manager to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/konflux-operator:tag
```

> [!NOTE]
> If you encounter RBAC errors, you may need to grant yourself cluster-admin
> privileges or be logged in as admin.

## Create Instances

Create instances of your solution. You can apply the samples (examples) from the
config/sample:

```sh
kubectl apply -k config/samples/
```

> [!NOTE]
> Ensure that the samples has default values to test it out.

# Uninstalling

## Delete the instances (CRs) from the cluster:

```sh
kubectl delete -k config/samples/
```

## Delete the APIs(CRDs) from the cluster:

```sh
make uninstall
```

## UnDeploy the controller from the cluster:

```sh
make undeploy
```

# Project Distribution

Following the options to release and provide this solution to the users.

## By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/konflux-operator:tag
```

> [!NOTE]
> The makefile target mentioned above generates an 'install.yaml'
> file in the dist directory. This file contains all the resources built
> with Kustomize, which are necessary to install this project without its
> dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/konflux-operator/<tag or branch>/dist/install.yaml
```

## By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
operator-sdk edit --plugins=helm/v1-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

> [!NOTE]
> If you change the project, you need to update the Helm Chart
> using the same command above to sync the latest changes. Furthermore,
> if you create webhooks, you need to use the above command with
> the '--force' flag and manually ensure that any custom configuration
> previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
> is manually re-applied afterwards.

# Contributing
See the [main repository's contributing guidelines](../CONTRIBUTING.md) for details.

More information can be found via the
[Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

# License

Copyright 2025 Konflux CI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

