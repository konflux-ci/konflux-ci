---
title: "Build Pipeline Customization"
linkTitle: "Build Pipeline Customization"
weight: 10
description: "How to customize build pipelines by overriding, removing, or adding entries in the build-pipeline-config ConfigMap."
---

The Konflux operator manages a `build-pipeline-config` ConfigMap in the
`build-service` namespace. This ConfigMap tells the build-service which Tekton
pipeline bundles are available and which one is the default for new components.

By default the operator ships a curated set of pipelines. The `pipelineConfig`
field in the Konflux CR lets you customize this list without manually editing the
ConfigMap.

## When you need this

- **Pin a pipeline bundle** — override a default pipeline with a specific image
  digest for reproducibility or testing.
- **Remove a pipeline** — exclude a pipeline that is not relevant to your
  environment (e.g. remove `fbc-builder` if you do not build FBC images).
- **Add a custom pipeline** — include an in-house pipeline alongside the
  defaults.
- **Replace all defaults** — discard every operator-provided pipeline and supply
  your own complete list.

## Configuration

Add the `pipelineConfig` field under `spec.buildService.spec` in the Konflux CR.

### Override a default pipeline bundle

Replace the bundle reference for an existing default pipeline by providing a
pipeline with the same `name` and a new `bundle`:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  buildService:
    spec:
      pipelineConfig:
        pipelines:
          - name: docker-build-oci-ta
            bundle: quay.io/my-org/custom-docker-build@sha256:abc123...
```

The operator merges this with the defaults — only the named pipeline is
replaced. All other defaults remain unchanged, and the description from the
default entry is preserved.

### Remove a default pipeline

Mark a pipeline as `removed: true` to exclude it:

```yaml
spec:
  buildService:
    spec:
      pipelineConfig:
        pipelines:
          - name: fbc-builder
            removed: true
```

### Add a custom pipeline

Provide a `name` and `bundle` for a pipeline that does not exist in the defaults.
You can optionally include a `description`:

```yaml
spec:
  buildService:
    spec:
      pipelineConfig:
        pipelines:
          - name: my-custom-pipeline
            bundle: quay.io/my-org/my-pipeline@sha256:def456...
            description: "Internal pipeline for containerized builds"
```

### Change the default pipeline

Set `defaultPipelineName` to the name of the pipeline that should be used as the
default for new components:

```yaml
spec:
  buildService:
    spec:
      pipelineConfig:
        defaultPipelineName: docker-build-oci-ta
        pipelines:
          - name: docker-build-oci-ta
            bundle: quay.io/my-org/custom-docker-build@sha256:abc123...
```

### Replace all defaults

Set `removeDefaults: true` to discard every operator-provided pipeline. When
using this option you must provide at least one pipeline with a bundle and set
`defaultPipelineName`:

```yaml
spec:
  buildService:
    spec:
      pipelineConfig:
        removeDefaults: true
        defaultPipelineName: my-only-pipeline
        pipelines:
          - name: my-only-pipeline
            bundle: quay.io/my-org/my-pipeline@sha256:abc123...
```

### Combined example

Override one default, remove another, add a custom pipeline, and change the
default — all in a single CR:

```yaml
spec:
  buildService:
    spec:
      pipelineConfig:
        defaultPipelineName: docker-build-oci-ta
        pipelines:
          - name: docker-build-oci-ta
            bundle: quay.io/my-org/custom-docker-build@sha256:abc123...
            description: "Custom docker build with internal base images"
          - name: fbc-builder
            removed: true
          - name: my-custom-pipeline
            bundle: quay.io/my-org/my-pipeline@sha256:def456...
            description: "Internal pipeline for containerized builds"
```

## Merge behavior

When `pipelineConfig` is set, the operator merges the user-specified pipelines
with the operator-provided defaults:

| Condition | Behavior |
|-----------|----------|
| Pipeline name matches a default | User entry **overrides** the default bundle |
| Pipeline name does not match any default | Entry is **appended** to the list |
| Pipeline has `removed: true` | Matching default is **excluded** |
| `removeDefaults: true` | All defaults are **discarded** before applying user pipelines |

When overriding a default pipeline, the default description is preserved unless
you provide your own `description` in the pipeline entry.

If `defaultPipelineName` is set, it replaces the operator-provided default. If
the effective default pipeline ends up missing from the merged list (e.g. it was
removed without setting a replacement), the operator automatically selects the
first available pipeline and logs a warning.

## Validation

The operator validates the `pipelineConfig` at admission time (via CRD
validation rules) to catch common configuration errors before reconciliation:

- A pipeline entry must have either a `bundle` or `removed: true` — not both,
  and not neither.
- When `removeDefaults` is `true`, `defaultPipelineName` must be set.
- When `removeDefaults` is `true`, at least one non-removed pipeline must be
  provided.
- When `removeDefaults` is `true`, `defaultPipelineName` must reference a
  non-removed pipeline in the list.
- `defaultPipelineName` must not reference a pipeline that has `removed: true`.

For full field details, see the
[API Reference]({{< relref "../reference" >}}).
