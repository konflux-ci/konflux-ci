# Demo Users Configuration

**WARNING:** Demo users are for TESTING ONLY and must NEVER be used in production environments.

Demo users use publicly known passwords that provide no security. Anyone with access to this documentation knows the credentials. Use demo users only for local development, testing authentication flows, and demonstration environments that contain no sensitive data.

## Security Considerations

Demo users create these security risks:

Hardcoded passwords that everyone knows ("password"), credentials stored in public Git repositories where anyone can read the bcrypt hashes, no password rotation mechanism, no audit logging to track who used which demo account, and shared credentials used by multiple people.

**Production authentication:** Configure proper identity providers through the Konflux CR. Use GitHub OAuth for GitHub accounts, Google OIDC for Google Workspace, LDAP for corporate directories, or OpenShift's built-in authentication on OpenShift clusters.

See the [Operator Deployment Guide](operator-deployment.md#authentication) for connector configuration examples.

## Default Credentials

The automated deployment creates two demo users:

- Username: `user1@konflux.dev` / Password: `password`
- Username: `user2@konflux.dev` / Password: `password`

Each user has a corresponding namespace (`user-ns1`, `user-ns2`) and managed namespace (`managed-ns1`, `managed-ns2`) for testing releases.

## Automated Deployment

The local development script deploys demo users by default for convenience.

Deploy using the script:

```bash
./scripts/deploy-demo-resources.sh
```

This script configures demo users in the KonfluxUI custom resource, creates demo namespaces with appropriate labels, deploys RBAC resources granting demo users access to their namespaces, and creates managed namespaces for testing releases.

To disable demo user deployment, set `DEPLOY_DEMO_RESOURCES=0` in `scripts/deploy-local-dev.env` before running `./scripts/deploy-local-dev.sh`.

## Manual Configuration

Configure demo users manually through the KonfluxUI custom resource rather than patching Dex directly.

Patch the existing KonfluxUI resource:

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=merge -p '
spec:
  dex:
    config:
      enablePasswordDB: true
      staticPasswords:
      - email: "user1@konflux.dev"
        username: "user1"
        userID: "7138d2fe-724e-4e86-af8a-db7c4b080e20"
        hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W" # gitleaks:allow
      - email: "user2@konflux.dev"
        username: "user2"
        userID: "ea8e8ee1-2283-4e03-83d4-b00f8b821b64"
        hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W" # gitleaks:allow
'
```

Or include `staticPasswords` in your Konflux CR during deployment:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  ui:
    spec:
      dex:
        config:
          enablePasswordDB: true
          staticPasswords:
          - email: "user1@konflux.dev"
            username: "user1"
            userID: "7138d2fe-724e-4e86-af8a-db7c4b080e20"
            hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W" # gitleaks:allow
          - email: "user2@konflux.dev"
            username: "user2"
            userID: "ea8e8ee1-2283-4e03-83d4-b00f8b821b64"
            hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W" # gitleaks:allow
```

The operator manages Dex configuration through the KonfluxUI custom resource. When you update the CR, the operator generates a new Dex ConfigMap and restarts Dex automatically.

**Do not manually patch the Dex deployment or ConfigMap.** The operator owns these resources and reverts manual changes during reconciliation.

## Adding Custom Demo Users

Create additional demo users for specific testing scenarios.

### Generate Password Hash

Demo users require bcrypt-hashed passwords. Generate a hash using htpasswd:

```bash
echo "your-password" | htpasswd -BinC 10 admin | cut -d: -f2
```

Or using Python:

```bash
python3 -c 'import bcrypt; print(bcrypt.hashpw(b"your-password", bcrypt.gensalt(10)).decode())'
```

### Generate Unique User ID

Create a unique UUID for the user:

```bash
uuidgen | tr '[:upper:]' '[:lower:]'
```

### Add to Configuration

Patch the KonfluxUI CR with the new user:

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=merge -p '
spec:
  dex:
    config:
      staticPasswords:
      - email: "newuser@example.com"
        username: "newuser"
        userID: "unique-uuid-here"
        hash: "$2a$10$..your-bcrypt-hash.."
'
```

This merges the new user into the existing `staticPasswords` array without removing existing users.

## Demo Namespaces and RBAC

Demo resources include namespaces and role bindings for testing.

The deployment creates these resources:

- **user-ns1**, **user-ns2** - Development namespaces where demo users create applications and components
- **managed-ns1**, **managed-ns2** - Managed namespaces simulating production environments for release testing
- **ClusterRoles** - Permissions allowing demo users to create Konflux resources

These resources are defined in `test/resources/demo-users/user/`.

Deploy only the namespaces without configuring demo users:

```bash
kubectl apply -k test/resources/demo-users/user/
```

This creates the namespace structure and RBAC while leaving authentication configuration unchanged.

## Removing Demo Users

Remove demo users completely or selectively based on your needs.

### Remove All Demo Users

Disable the password database entirely:

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=merge -p '
spec:
  dex:
    config:
      enablePasswordDB: false
'
```

Or remove the `staticPasswords` array while keeping the password database enabled:

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=json -p='[
  {"op": "remove", "path": "/spec/dex/config/staticPasswords"}
]'
```

Ensure you have at least one connector configured before disabling the password database, or you will lock yourself out.

### Remove Specific Demo User

View current users:

```bash
kubectl get konfluxui konflux-ui -n konflux-ui -o jsonpath='{.spec.dex.config.staticPasswords}' | jq
```

Update the CR with only the users you want to keep:

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=merge -p '
spec:
  dex:
    config:
      staticPasswords:
      - email: "user1@konflux.dev"
        username: "user1"
        userID: "7138d2fe-724e-4e86-af8a-db7c4b080e20"
        hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W" # gitleaks:allow
'
```

This replaces the entire `staticPasswords` array with your specified users.

### Remove Demo Namespaces

Delete the demo user namespaces and RBAC:

```bash
kubectl delete namespace user-ns1 user-ns2 managed-ns1 managed-ns2
kubectl delete clusterrole konflux-admin-user-actions
kubectl delete -k test/resources/demo-users/user/
```

## Troubleshooting

### Demo Login Fails

Verify the configuration was applied to the KonfluxUI CR:

```bash
kubectl get konfluxui konflux-ui -n konflux-ui -o jsonpath='{.spec.dex.config.staticPasswords}' | jq
```

Check if Dex restarted after the configuration change:

```bash
kubectl get pods -n konflux-ui -l app=dex
kubectl rollout status deployment/dex -n konflux-ui
```

Verify the Dex ConfigMap contains static passwords. The operator generates this ConfigMap from the KonfluxUI CR:

```bash
kubectl get configmap -n konflux-ui -o name | grep dex
kubectl get configmap <dex-configmap-name> -n konflux-ui -o yaml | grep -A 10 staticPasswords
```

Check Dex logs for authentication errors:

```bash
kubectl logs -n konflux-ui deployment/dex
```

### Operator Reverts Manual Changes

The operator owns the Dex deployment and ConfigMap. It reconciles these resources to match the KonfluxUI CR specification. Manual patches to Dex resources are reverted automatically.

Always configure Dex through the KonfluxUI CR. Never manually edit the Dex deployment, ConfigMap, or service.

### Password Hash Not Working

Verify you generated the hash correctly. The hash must use bcrypt with cost factor 10:

```bash
# Test your hash
python3 -c 'import bcrypt; print(bcrypt.checkpw(b"password", b"$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W"))'
# Should print: True
```

Ensure the hash in your CR includes the full bcrypt string including the `$2a$10$` prefix.

### Cannot Access UI After Disabling Demo Users

If you disabled the password database without configuring a connector, you locked yourself out.

Re-enable the password database temporarily:

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=merge -p '
spec:
  dex:
    config:
      enablePasswordDB: true
      staticPasswords:
      - email: "admin@konflux.dev"
        username: "admin"
        userID: "temporary-admin-id"
        hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W" # gitleaks:allow
'
```

Configure your desired connector, test that it works, then disable the password database again.

## References

- [Dex Documentation](https://dexidp.io/docs/) - Complete Dex reference
- [Dex Static Password Configuration](https://dexidp.io/docs/connectors/local/) - Password connector details
- [Dex Connectors](https://dexidp.io/docs/connectors/) - Production identity providers
- [Operator Deployment Guide](operator-deployment.md#authentication) - Connector examples
