# Sample Custom Resources

This directory contains sample YAML files for Konflux Custom Resources (CRs).

<!-- toc -->

- [Functional Samples](#functional-samples)
- [Component CR Samples](#component-cr-samples)
- [Production Considerations](#production-considerations)
  * [Authentication](#authentication)
  * [Default Tenant](#default-tenant)
  * [Registry Configuration](#registry-configuration)
- [Related Documentation](#related-documentation)

<!-- tocstop -->

## Functional Samples

**konflux_v1alpha1_konflux.yaml** - Used in CI tests and local development.
Represents a complete Konflux CR configuration with all components, realistic
resource limits, and demo users for testing. Includes helpful comments for
common configurations.

**konflux-e2e.yaml** - Extends the Kind base configuration with image-controller
enabled, which is required for E2E tests. Used by GitHub Actions and Tekton Kind
e2e. Do not apply on OpenShift.

**konflux-openshift.yaml** - Default CR for `deploy-konflux-on-ocp.sh` (human
installs). Contains no demo users and disables the Dex password database; users
authenticate through the OpenShift OAuth server, which the operator wires up
automatically on OpenShift. Use this, not the Kind samples, on any OpenShift
cluster.

**konflux-openshift-e2e.yaml** - Default CR for `deploy-konflux-on-ocp.sh` when
`OPENSHIFT_CI=true` (Prow). Same as `konflux-openshift.yaml`, plus
image-controller and PaC webhook TLS skip. No Dex demo users.

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

`konflux_v1alpha1_konflux.yaml` and `konflux-e2e.yaml` include demo users whose password
is published in this repository. **They are for Kind and CI only and must never be
applied to a cluster that is reachable from the internet.**

For OpenShift, use `konflux-openshift.yaml` (or `konflux-openshift-e2e.yaml` in
Prow). `deploy-konflux-on-ocp.sh` refuses to apply any CR containing
`staticPasswords` unless `ALLOW_DEV_STATIC_PASSWORDS=true` is set.

For any other production deployment, omit the `staticPasswords` section, set
`enablePasswordDB: false`, and configure OIDC connectors (GitHub, Google, LDAP, etc.).
See `konflux-with-github-auth.yaml` for an example and the
[Dex Connectors Documentation](https://dexidp.io/docs/connectors/) for all supported
connectors. When the password database is disabled the operator drops any
`staticPasswords` from the rendered Dex config, so they never reach the cluster.

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

### Registry Configuration

For production deployments, use an external container registry. To fully onboard
components through the Konflux UI, enable the image-controller in your Konflux CR
and configure a Quay.io token. For other registries, configure push secrets manually.

See the [Registry Configuration Guide](../../docs/registry-configuration.md) for
setup instructions covering Quay.io, Docker Hub, and other OCI-compliant registries.

## Related Documentation

- [Operator Deployment Guide](../../docs/operator-deployment.md) - Full deployment instructions
- [Troubleshooting Guide](../../docs/troubleshooting.md) - Common issues and solutions
- [Root README](../../README.md) - Quick start guides
