---
title: "Resource Management"
linkTitle: "Resource Management"
weight: 6
description: "Tuning CPU and memory for Konflux core services and Tekton workloads in resource-constrained environments."
---

The Konflux Kind environment targets workstations, CI runners, and other
resource-constrained systems. Some workloads, however, prioritize performance over
resource conservation.

Resource consumption for Konflux core services is configured through the Konflux
Custom Resource, which the operator manages declaratively.

## Workloads Deployed with Konflux

Konflux is deployed using an Operator. Resource consumption for its components is
configured through the Konflux Custom Resource rather than by patching manifests
directly. The operator manages these resources declaratively.

To adjust resource consumption, edit your Konflux CR and specify resource `requests`
and `limits` in the component specifications. For example:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  buildService:
    spec:
      buildControllerManager:
        manager:
          resources:
            requests:
              cpu: 30m
              memory: 128Mi
            limits:
              cpu: 30m
              memory: 128Mi
  # Similar configuration for other components...
```

See the [sample Konflux CR]({{< relref "../examples#konflux-configuration" >}})
for resource configuration examples across all components.
