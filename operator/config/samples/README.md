# Sample Custom Resources

This directory contains sample YAML files for Konflux Custom Resources (CRs).

## Functional Samples

**konflux_v1alpha1_konflux.yaml** - Used in CI tests and local development. Represents a complete Konflux CR configuration with all components, realistic resource limits, and demo users for testing. Includes helpful comments for common configurations.

**konflux-empty-cr.yaml** - Minimal empty spec using all default values.

**konflux-with-github-auth.yaml** - Shows GitHub authentication connector configuration for production use.

## Component CR Samples

All other sample files are provided to demonstrate the CRD structure and available
fields. These samples are **not** intended to represent meaningful functional examples,
but rather to showcase a complete-as-possible schema of each CRD type.

These samples are useful for:
- Understanding the available configuration options
- Validating CRD schema completeness (verified in unit tests)

## Authentication

The main `konflux_v1alpha1_konflux.yaml` sample includes demo users with static passwords for CI testing and local development. **These demo users are for testing only and should never be used in production.**

For production deployments, remove the `staticPasswords` section and configure OIDC connectors (GitHub, Google, LDAP, etc.) for authentication. See the Dex documentation for available connector configuration - https://dexidp.io/docs/connectors

## Related Documentation

- [Operator Deployment Guide](../../docs/operator-deployment.md) - Full deployment instructions
- [Troubleshooting Guide](../../docs/troubleshooting.md) - Common issues and solutions
- [Root README](../../README.md) - Quick start guides
