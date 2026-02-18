# Sample Custom Resources

This directory contains sample YAML files for Konflux Custom Resources (CRs).

<!-- toc -->

- [Functional Samples](#functional-samples)
- [Component CR Samples](#component-cr-samples)
- [Production Considerations](#production-considerations)
  * [Authentication](#authentication)
  * [Default Tenant](#default-tenant)
  * [Pipeline Configuration](#pipeline-configuration)
    + [Default Pipeline Selection](#default-pipeline-selection)
- [Related Documentation](#related-documentation)

<!-- tocstop -->

## Functional Samples

**konflux_v1alpha1_konflux.yaml** - Used in CI tests and local development.
Represents a complete Konflux CR configuration with all components, realistic
resource limits, and demo users for testing. Includes helpful comments for
common configurations.

**konflux-e2e.yaml** - Extends the base configuration with image-controller
enabled, which is required for E2E tests. Used by the CI E2E workflow.

**konflux-empty-cr.yaml** - Minimal empty spec using all default values.

**konflux-with-github-auth.yaml** - Shows GitHub authentication connector configuration for production use.

## Component CR Samples

All other sample files are provided to demonstrate the CRD structure and available
fields. These samples are **not** intended to represent meaningful functional examples,
but rather to showcase a complete-as-possible schema of each CRD type.

These samples are useful for:
- Understanding the available configuration options
- Validating CRD schema completeness (verified in unit tests)

## Production Considerations

### Authentication

The main `konflux_v1alpha1_konflux.yaml` sample includes demo users with static passwords for CI testing and local development. **These demo users are for testing only and should never be used in production.**

For production deployments, remove the `staticPasswords` section and configure OIDC connectors (GitHub, Google, LDAP, etc.) for authentication. See `konflux-with-github-auth.yaml` for an example and the [Dex Connectors Documentation](https://dexidp.io/docs/connectors/) for all supported connectors.

### Default Tenant

The operator creates a `default-tenant` namespace by default where all authenticated
users have maintainer permissions. This is convenient for local development and testing
but may not be appropriate for production multi-tenant environments.

For production deployments requiring strict namespace isolation, disable the default
tenant and create per-team tenant namespaces with appropriate RBAC:

```yaml
spec:
  defaultTenant:
    enabled: false
```

After disabling, create tenant namespaces and rolebindings as desired:

```bash
kubectl create namespace org-user-1-tenant
kubectl label namespace org-user-1-tenant konflux-ci.dev/type=tenant
kubectl create rolebinding org-user-1-tenant-maintainer --clusterrole konflux-maintainer-user-actions \
  --user user@example.com -n org-user-1-tenant
```

### Pipeline Configuration

The operator manages the `build-pipeline-config` ConfigMap in the build-service
namespace, which defines the default pipeline bundles (docker-build-oci-ta,
fbc-builder, etc.). Use the `pipelineConfig` field in the Konflux CR to
customize pipeline configuration: override defaults by name, remove specific
defaults, discard all defaults, or add custom pipelines.

```yaml
spec:
  buildService:
    spec:
      pipelineConfig:
        pipelines:
          - name: fbc-builder
            removed: true  # Remove this specific default pipeline
          - name: my-custom-pipeline
            bundle: quay.io/myorg/pipeline:latest
```

#### Default Pipeline Selection

Use `defaultPipelineName` to specify which pipeline should be selected by default
when creating new components. This overrides the operator's built-in default
(docker-build-oci-ta).

Use an existing default pipeline:

```yaml
spec:
  buildService:
    spec:
      pipelineConfig:
        defaultPipelineName: fbc-builder
```

Use a custom pipeline as the default:

```yaml
spec:
  buildService:
    spec:
      pipelineConfig:
        pipelines:
        - name: my-custom-pipeline
          bundle: quay.io/myorg/pipeline:v1.0
        defaultPipelineName: my-custom-pipeline
```

When using `removeDefaults: true`, you must specify a defaultPipelineName:

```yaml
spec:
  buildService:
    spec:
      pipelineConfig:
        removeDefaults: true
        pipelines:
        - name: only-this-pipeline
          bundle: quay.io/myorg/pipeline:v1.0
        defaultPipelineName: only-this-pipeline
```

See `konflux_v1alpha1_konfluxbuildservice.yaml` for additional configuration examples.

## Related Documentation

- [Operator Deployment Guide](../../docs/operator-deployment.md) - Full deployment instructions
- [Troubleshooting Guide](../../docs/troubleshooting.md) - Common issues and solutions
- [Root README](../../README.md) - Quick start guides
