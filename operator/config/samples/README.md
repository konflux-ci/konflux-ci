# Sample Custom Resources

This directory contains sample YAML files for Konflux Custom Resources (CRs).

## Top-Level CR Sample

The `konflux_v1alpha1_konflux.yaml` sample is used in CI tests and represents a
functional example of the main Konflux CR that configures all components.

## Component CR Samples

All other sample files are provided to demonstrate the CRD structure and available
fields. These samples are **not** intended to represent meaningful functional examples,
but rather to showcase a complete-as-possible schema of each CRD type.

These samples are useful for:
- Understanding the available configuration options
- Validating CRD schema completeness (verified in unit tests)

## Authentication and Demo Users

**Security Best Practice:** These samples do **not** include demo users with static passwords.

Production deployments should use OIDC connectors (GitHub, Google, LDAP, etc.) for authentication, not static passwords. The samples demonstrate the proper configuration structure with `enablePasswordDB: true` and `passwordConnector: local`, but intentionally omit the insecure `staticPasswords` section.

### For Testing: Demo Resources

If you need demo users for local testing, use the demo resources script:

```bash
# Deploy Konflux first, then add demo resources
./scripts/deploy-demo-resources.sh
```

This deploys test users (`user1@konflux.dev` / `password`) to any cluster running Konflux.

**WARNING:** Demo users are insecure and must never be used in production.

See [Demo Users Guide](../../docs/demo-users.md) for detailed instructions on configuring, adding, or removing demo users.

### For Production: OIDC Connectors

Configure authentication using real OIDC providers. Example:

```yaml
dex:
  config:
    enablePasswordDB: false
    connectors:
      - type: "github"
        id: "github"
        name: "GitHub"
        config:
          clientID: "$GITHUB_CLIENT_ID"
          clientSecret: "$GITHUB_CLIENT_SECRET"
          redirectURI: "https://konflux.example.com/idp/callback"
```

See [Operator Deployment Guide](../../docs/operator-deployment.md) for complete configuration examples.

## Related Documentation

- [Operator Deployment Guide](../../docs/operator-deployment.md) - Full deployment instructions
- [Demo Users Guide](../../docs/demo-users.md) - Configure demo users for testing
- [Mac Setup Guide](../../docs/mac-setup.md) - macOS-specific setup
- [Root README](../../README.md) - Quick start guides
