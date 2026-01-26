# Demo Users Configuration

Demo users provide a simple authentication method for local development and testing of Konflux. They use static passwords stored in the Dex configuration.

**WARNING:** Demo users are for **TESTING ONLY**. Never use them in production environments.

## Default Demo Credentials

- **user1@konflux.dev** / password
- **user2@konflux.dev** / password

## Quick Start

### Automated Setup

Use the deployment script to automatically configure demo users:

```bash
./scripts/deploy-demo-resources.sh
```

This script:
- Configures demo users via the KonfluxUI custom resource
- Deploys demo namespaces and RBAC for testing

### Manual Configuration

Configure demo users via the `KonfluxUI` custom resource:

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

Alternatively, include `staticPasswords` in your Konflux CR when deploying:

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
  # ... rest of your Konflux configuration
```

## How It Works

The Konflux operator manages Dex configuration through the `KonfluxUI` custom resource. Demo users are configured by:

1. Setting `spec.dex.config.enablePasswordDB: true`
2. Adding entries to `spec.dex.config.staticPasswords[]`

The operator automatically:
- Generates a Dex ConfigMap with the static passwords
- Restarts Dex when the configuration changes
- Maintains the configuration (manual patches to Dex will be reverted)

**Do NOT manually patch the Dex deployment or ConfigMap** - the operator owns these resources and will revert changes.

Verify the Dex configuration was applied:

```bash
# Check that Dex pods restarted
kubectl get pods -n konflux-ui -l app=dex

# Verify the configuration includes your static passwords
kubectl get konfluxui konflux-ui -n konflux-ui -o jsonpath='{.spec.dex.config.staticPasswords}' | jq
```

Test the login at https://localhost:9443 with the demo credentials (`user1@konflux.dev` / `password`).

## Adding Custom Demo Users

### Generate Password Hash

Demo users require bcrypt-hashed passwords. Generate a hash:

```bash
echo "your-password" | htpasswd -BinC 10 admin | cut -d: -f2
```

Or using Python:

```bash
python3 -c 'import bcrypt; print(bcrypt.hashpw(b"your-password", bcrypt.gensalt(10)).decode())'
```

### Add to KonfluxUI CR

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

### Generate Unique User ID

```bash
uuidgen | tr '[:upper:]' '[:lower:]'
```

## Removing Demo Users

### Remove All Demo Users

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=json -p='[
  {"op": "remove", "path": "/spec/dex/config/staticPasswords"}
]'
```

### Remove Specific Demo User

View current users:

```bash
kubectl get konfluxui konflux-ui -n konflux-ui -o jsonpath='{.spec.dex.config.staticPasswords}' | jq
```

Update with only the users you want to keep:

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=merge -p '
spec:
  dex:
    config:
      staticPasswords:
      - email: "user1@konflux.dev"
        # ... only users you want to keep
'
```

## Demo Namespaces and RBAC

The demo resources script also deploys test namespaces and RBAC:

- **ns1**, **ns2** - User namespaces for demo users
- **managed-ns1**, **managed-ns2** - Managed namespaces for release testing
- **ClusterRoles** - Permissions for demo users to create components, applications, etc.

These are defined in `test/resources/demo-users/user/`.

To deploy only the namespaces without demo users:

```bash
kubectl apply -k test/resources/demo-users/user/
```

## Troubleshooting

### Demo Login Fails After Configuration

**Symptom:** Login with demo credentials fails even after patching KonfluxUI CR.

**Solutions:**

1. Verify the configuration was applied:
   ```bash
   kubectl get konfluxui konflux-ui -n konflux-ui -o jsonpath='{.spec.dex.config.staticPasswords}' | jq
   ```

2. Check if Dex has restarted:
   ```bash
   kubectl get pods -n konflux-ui -l app=dex
   kubectl rollout status deployment/dex -n konflux-ui
   ```

3. Verify the Dex ConfigMap contains static passwords:
   ```bash
   kubectl get configmap -n konflux-ui | grep dex
   kubectl get configmap <dex-configmap-name> -n konflux-ui -o yaml | grep -A 10 staticPasswords
   ```

4. Check Dex logs for authentication errors:
   ```bash
   kubectl logs -n konflux-ui deployment/dex
   ```

### Operator Reverts Manual Changes

**Symptom:** Manual patches to Dex deployment or ConfigMap are reverted.

**Explanation:** This is expected behavior. The operator owns Dex resources and reconciles them to match the `KonfluxUI` CR specification.

**Solution:** Always configure Dex through the `KonfluxUI` CR, never manually.

## Security Considerations

### Why Demo Users Are Insecure

1. **Hardcoded passwords** - Everyone knows "password"
2. **Stored in Git** - Hashes are public in the repository
3. **No password rotation** - Static credentials never change
4. **No audit logging** - Can't track who used which demo account
5. **Shared credentials** - Multiple people use the same accounts

### Production Authentication

For production or shared environments, use real identity providers:

- **GitHub OAuth** - Authenticate with GitHub accounts
- **Google OAuth** - Authenticate with Google accounts
- **LDAP** - Authenticate against corporate directory
- **OpenShift** - Use cluster's built-in authentication (on OpenShift)

See `operator/config/samples/konflux_v1alpha1_konfluxui.yaml` for connector examples.

### Disabling Password Database

To disable local password authentication entirely:

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=merge -p '
spec:
  dex:
    config:
      enablePasswordDB: false
'
```

Note: Ensure you have at least one connector configured before disabling the password database.

## References

- [Dex Documentation](https://dexidp.io/docs/)
- [Dex Static Password Configuration](https://dexidp.io/docs/connectors/local/)
- [KonfluxUI CRD Reference](../operator/config/crd/bases/konflux.konflux-ci.dev_konfluxuis.yaml)
- [Demo User Resources](../test/resources/demo-users/)
