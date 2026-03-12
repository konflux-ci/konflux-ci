---
title: "Applying the Konflux Custom Resource"
linkTitle: "Applying the Konflux Custom Resource"
weight: 6
description: "How to create a Konflux Custom Resource and verify the installation is ready."
---

The Konflux Operator deploys all Konflux components from a single `Konflux` Custom Resource (CR).
This page covers how to apply a CR and verify that all components are ready.

## Create the Konflux Custom Resource

Apply one of the samples from `operator/config/samples/` (or create your own) and wait for
Konflux to be ready. See the [Examples]({{< relref "../examples" >}}) page for all example
configurations.

{{< alert color="warning" >}}
Do <strong>not</strong> use <code>konflux_v1alpha1_konflux.yaml</code> for production — it
contains demo users with static passwords intended for local testing only. Use OIDC
authentication instead.
{{< /alert >}}

```bash
kubectl apply -f operator/config/samples/<one of the sample files>
```

## Verify the Konflux CR is ready

Wait for the `Ready` condition if the deployment is still in progress:

```bash
kubectl wait --for=condition=Ready=True konflux konflux --timeout=15m
```

Check the Konflux CR status. When Konflux CR is ready, the output includes the UI URL:

```bash
kubectl get konflux konflux
NAME      READY   UI-URL                                                    AGE
konflux   True    https://konflux-ui-konflux-ui.apps.<cluster-domain>       10m

````

## What's next

- [Onboard a new Application]({{< relref "onboard" >}}) — onboard an application, run builds, tests, and releases
- [GitHub Application Secrets]({{< relref "github-secrets" >}}) — create a GitHub App and configure webhook delivery
- [Registry Configuration]({{< relref "registry-configuration" >}}) — configure an external container registry for build and release pipelines
- [API Reference]({{< relref "../reference/konflux.v1alpha1" >}}) — full CR field reference
- [Troubleshooting]({{< relref "../troubleshooting" >}}) — solutions to common installation and runtime issues
- [Examples]({{< relref "../examples" >}}) — sample Konflux CR configurations
