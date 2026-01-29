# UI

## Overview

This component deploys the Konflux UI with the following key components:

- **proxy** - An nginx + oauth2-proxy deployment that handles authentication and proxies requests to the Kubernetes API
- **dex** - An OpenID Connect (OIDC) identity provider for authentication

## Certificate Infrastructure

### TLS Certificate for Dex

The `dex` service uses a self-signed TLS certificate for secure communication. This certificate is automatically managed by cert-manager:

1. **Certificate Resource** (`upstream-kustomizations/ui/certmanager/certificate.yaml`)
   - Defines a Certificate CR named `serving-cert` in the `konflux-ui` namespace
   - Uses `self-signed-cluster-issuer` (ClusterIssuer) to generate the certificate
   - cert-manager automatically creates and maintains the `dex-cert` secret with:
     - `tls.crt` - Dex's TLS certificate
     - `tls.key` - Dex's private key (kept secure in this secret)
     - `ca.crt` - The CA certificate (same as tls.crt for self-signed certs)

2. **Certificate Lifecycle**
   - cert-manager automatically renews the certificate before expiration
   - When renewed, cert-manager updates the `dex-cert` secret in-place
   - The dex pod automatically reloads the updated certificate

### CA Bundle Synchronization for oauth2-proxy

The `oauth2-proxy` needs to trust Dex's TLS certificate. This is achieved through a controller-managed CA bundle:

1. **Controller Sync Process** (implemented in `konfluxui_controller.go`)
   - The `reconcileDexCABundle()` function watches the `dex-cert` secret
   - When the secret is created or updated (e.g., during cert renewal), the controller:
     - Extracts `ca.crt` from the `dex-cert` secret
     - Syncs it to the `dex-ca-bundle` ConfigMap in the `konflux-ui` namespace
   - The ConfigMap is updated **in-place** (no content-based hashing)

2. **oauth2-proxy Integration**
   - The `dex-ca-bundle` ConfigMap is mounted into the oauth2-proxy container
   - The `SSL_CERT_FILE` environment variable points to the mounted CA certificate
   - This allows oauth2-proxy to verify Dex's TLS certificate during HTTPS connections

3. **Update Behavior**
   - When cert-manager renews the Dex certificate:
     - The controller detects the secret change (via watcher)
     - Updates the `dex-ca-bundle` ConfigMap with the new CA
     - Kubernetes eventually propagates the ConfigMap update to mounted volumes
     - oauth2-proxy pods pick up the new CA on their next natural restart/redeploy
   - **Note**: ConfigMap updates do not trigger automatic pod restarts. For most use cases, this is acceptable as certificate rotations are infrequent and pods are regularly redeployed. If immediate propagation is critical, consider using a tool like [Reloader](https://github.com/stakater/Reloader) to watch ConfigMaps and restart pods automatically.

### Security Considerations

- **Credential Isolation**: The `dex-cert` secret (containing the private key) is only mounted by the dex pod. The oauth2-proxy pod only accesses the public CA certificate via the ConfigMap.
- **Public CA Data**: Using a ConfigMap (not a Secret) for the CA bundle is appropriate because CA certificates are public data used only for verification, not authentication.
- **No Intermediate Certificate Creation**: The controller directly syncs the CA bytes from the existing secret, eliminating the risk of creating additional certificates with minting capabilities.

## Dependencies

- **cert-manager** - Required for TLS certificate management
- **Dex** - Required as the OIDC provider for oauth2-proxy

## Verification

To verify the certificate infrastructure is working:

```bash
# Check that cert-manager created the dex-cert secret
kubectl get secret dex-cert -n konflux-ui

# Check that the controller synced the CA to the ConfigMap
kubectl get configmap dex-ca-bundle -n konflux-ui

# Verify the CA certificate content matches
kubectl get secret dex-cert -n konflux-ui -o jsonpath='{.data.ca\.crt}' | base64 -d
kubectl get configmap dex-ca-bundle -n konflux-ui -o jsonpath='{.data.ca\.crt}'

# Check that oauth2-proxy pods have the volume mounted
kubectl get deployment proxy -n konflux-ui -o yaml | grep -A 5 oauth2-proxy-ca
```
