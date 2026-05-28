# UI

## Overview

This component deploys the Konflux UI, which includes a static SPA, a
Caddy-based reverse proxy, oauth2-proxy for authentication, and Dex as
the identity broker.

## Dependencies

Dex is required for oauth2-proxy to be deployed.

---

# Proxy Architecture

The proxy is a Caddy-based reverse proxy that sits in front of the
Kubernetes API, backend services (Tekton Results, KubeArchive), and the
static SPA. It handles TLS termination, authentication (via oauth2-proxy),
user/group impersonation, and dynamic token management.

## Architecture

```mermaid
flowchart LR
  browser["Browser"]

  subgraph pod ["Proxy Pod"]
    direction LR
    oauth2["oauth2-proxy :6000"]
    caddy["Caddy :9443"]

    caddy -- forward_auth --> oauth2
  end

  browser -- HTTPS --> caddy

  subgraph upstreams [" "]
    kubeAPI["Kube API"]
    nsLister["Namespace Lister"]
    tekton["Tekton Results"]
    dex["Dex"]
  end

  caddy -- reverse_proxy --> upstreams
```

## Pod Structure

The proxy runs as a Kubernetes `Deployment` with two containers and two
init containers:

| Container | Type | Image | Purpose |
|-----------|------|-------|---------|
| `copy-static-content` | Init | `konflux-ui` | Copies SPA assets to emptyDir |
| `generate-proxy-config` | Init | `konflux-ui` | Resolves backends, generates TLS config |
| `reverse-proxy` | Main | `reverse-proxy` | Custom Caddy build on :9443 (HTTPS) and :2112 (metrics) |
| `oauth2-proxy` | Main | `oauth2-proxy` | OIDC authentication on :6000 |

## Request Flow

Every authenticated request follows this path:

```
Browser ──HTTPS──▶ Caddy (:9443)
                     │
                     ├─ Strip Impersonate-*/X-User/X-Group headers (prevent spoofing)
                     │
                     ├─ Match route (handle_path / handle / @matcher)
                     │
                     ├─ forward_auth ──▶ oauth2-proxy (:6000)
                     │                    │
                     │                    ├─ Has valid session cookie? ──▶ 200 + X-Auth-Request-Email
                     │                    │                                    + X-Auth-Request-Groups
                     │                    └─ No cookie? ──▶ 401 (Caddy returns login redirect)
                     │
                     ├─ impersonate: set Impersonate-User / Impersonate-Group headers
                     │
                     └─ reverse_proxy ──▶ upstream (Kube API / Tekton Results / etc.)
```

## Authentication Flow

Users authenticate via OIDC. The proxy never sees credentials directly —
authentication is fully delegated to oauth2-proxy and Dex.

```mermaid
sequenceDiagram
    participant B as Browser
    participant C as Caddy
    participant O as oauth2-proxy
    participant D as Dex
    participant IdP as Identity Provider
    participant K as Kube API

    B->>C: GET /api/k8s/... (no cookie)
    C->>O: forward_auth /oauth2/auth
    O-->>C: 401
    C-->>B: redirect to /oauth2/start

    B->>O: /oauth2/start
    O-->>B: redirect to Dex
    B->>D: /idp/auth
    D-->>B: redirect to IdP
    B->>IdP: authenticate
    IdP-->>B: redirect to Dex callback
    B->>D: /idp/callback
    D-->>B: redirect to oauth2-proxy callback
    B->>O: /oauth2/callback (code)
    O->>D: exchange code for token
    D-->>O: ID token (email, groups)
    O-->>B: set session cookie, redirect to original URL

    B->>C: GET /api/k8s/... (with cookie)
    C->>O: forward_auth /oauth2/auth
    O-->>C: 200 + X-Auth-Request-Email + X-Auth-Request-Groups
    Note over C: Split groups into Impersonate-Group headers
    C->>K: GET /... + Authorization (SA token) + Impersonate-User + Impersonate-Group
    K-->>C: response
    C-->>B: response
```

### Step by step

1. **First request (no session):** The browser hits a Kube API route.
   Caddy's `forward_auth` sends a subrequest to oauth2-proxy, which
   returns 401 (no valid cookie). Caddy redirects the browser to start
   the OIDC flow.

2. **OIDC flow:** oauth2-proxy redirects the browser to Dex, which
   redirects to the configured identity provider (GitHub, LDAP, OIDC,
   etc.). After the user authenticates, the browser follows callbacks
   back through Dex to oauth2-proxy, which exchanges the authorization
   code for an ID token containing the user's email and group memberships.

3. **Session established:** oauth2-proxy sets an encrypted session cookie
   and redirects the browser back to the original URL.

4. **Subsequent requests (with session):** Caddy's `forward_auth` sends
   a subrequest to oauth2-proxy, which validates the cookie and returns
   200 with `X-Auth-Request-Email` and `X-Auth-Request-Groups` headers.
   Caddy copies these onto the request, and the `impersonate` handler
   plugin sets `Impersonate-User` and individual `Impersonate-Group`
   headers (see [Group Impersonation](#group-impersonation)). The request
   is then forwarded to the Kube API with the proxy's own service account
   token in the `Authorization` header.

5. **Kube API impersonation:** The API server authenticates the proxy's
   service account, verifies it has impersonation permissions, and then
   processes the request as if it came from the impersonated user with
   the specified groups.

## Group Impersonation

The Kubernetes API requires each group as a separate `Impersonate-Group`
header. oauth2-proxy returns all groups in a single comma-separated string.

The proxy uses a custom Caddy build from
[konflux-ci/reverse-proxy](https://github.com/konflux-ci/reverse-proxy)
that includes the `impersonate` handler plugin. This plugin reads
`X-Auth-Request-Email` and `X-Auth-Request-Groups` from the auth response,
sets `Impersonate-User`, and splits the comma-separated groups into
individual `Impersonate-Group` headers — with no arbitrary group limit.
It also always appends `system:authenticated`.

For the namespace-lister, the plugin is configured with custom target
headers (`X-User` / `X-Group`) instead of the default Kubernetes
impersonation headers.

### Limitations

- Group changes in the identity provider (Dex) require the user to log out
  and log back in to get a fresh ID token.

## Token Management

Two projected service account tokens are mounted into the pod:

| Volume | Audience | Path | Purpose |
|--------|----------|------|---------|
| `kube-api-token` | *(default)* | `/var/run/secrets/konflux-ci.dev/serviceaccount/token` | Kube API impersonation |
| `backend-token` | `konflux-backend` | `/var/run/secrets/konflux-ci.dev/backend/token` | Backend services (TokenReview) |

The `backend-token` uses a dedicated audience to enforce the principle of
least privilege. The proxy's `kube-api-token` carries impersonation
permissions — if that same token were forwarded to a backend service and
the backend were compromised, an attacker could use it to impersonate any
user against the Kubernetes API. The `konflux-backend` audience token is
rejected by the Kube API for direct operations, limiting the blast radius
to TokenReview-based identity verification only.

The backend token infrastructure is in place but not yet active: Tekton
Results currently does not support custom audiences in its TokenReview
calls, so the proxy falls back to `kube_token` for now. We opened
[tektoncd/results#1331](https://github.com/tektoncd/results/issues/1331)
to request this capability upstream. Once supported, switching to the
backend token requires changing a single `header_up` line in the backend's
Caddy snippet (see the TODO in `tekton-results.caddy`).

### Dynamic File Rotation

All dynamic file content (tokens, certificates, CA bundles) is handled by
Caddy plugins from the
[reverse-proxy](https://github.com/konflux-ci/reverse-proxy) build — no
sidecar is needed.

| What changes | Frequency | Plugin | Reload? |
|---|---|---|---|
| Bearer tokens | Every 600s | `filewatcher` (`cache`) | No — atomic pointer swap |
| Serving TLS cert | Every 60–90 days | `certwatcher` (`get_certificate file`) | No — fsnotify |
| CA trust bundles | Rare (months/years) | `filewatcher` (`watch`) | Yes — SIGUSR1 seamless reload |

The `filewatcher` plugin caches token files in `atomic.Pointer[string]`
values — reads are a single pointer load with zero allocations and zero
syscalls per request. The `inject_cached_vars` middleware sets cached
values as `{http.vars.*}` placeholders for use in `header_up` directives.

The `certwatcher` plugin watches the serving certificate and key via
fsnotify and serves the latest version during TLS handshakes without a
Caddy reload.

CA bundle directories are watched by `filewatcher`; changes trigger
SIGUSR1, which causes Caddy to seamlessly re-provision its TLS config
(required because Go's `x509.CertPool` is immutable once created).

## Extensible Backends

Backend services (Tekton Results, KubeArchive, etc.) are added via
**Caddy snippet templates** and an **init container**:

1. Templates live in ConfigMaps mounted at `/mnt/caddy-templates/`
   (e.g. `tekton-results.caddy`). They use placeholders like
   `__TEKTON_RESULTS_HOSTNAME__`.

2. The `generate-proxy-config` init container resolves hostnames via DNS,
   substitutes placeholders, and writes rendered snippets to
   `/mnt/caddy-snippets/`.

3. The Caddyfile imports all rendered snippets via:
   ```
   import /mnt/caddy-snippets/*.caddy
   ```

4. If a backend is not available (DNS resolution fails), its snippet is
   simply not generated and Caddy starts without that route.

To add a new backend:

1. Create a template file (e.g. `kubearchive.caddy`) following the
   `tekton-results.caddy` pattern.
2. Add it to the `proxy-caddy-templates` ConfigMap in `kustomization.yaml`.
3. Add hostname resolution logic to `generate-proxy-config.sh`.

## TLS and Certificates

Each upstream uses a different trust anchor:

| Upstream | CA Source | Config |
|----------|-----------|--------|
| Kubernetes API | Mounted SA CA | `tls_trust_pool file /var/run/secrets/kubernetes.io/serviceaccount/ca.crt` |
| Dex | cert-manager Secret | `tls_trust_pool file /mnt/serving-cert/ca.crt` |
| Namespace-lister | trust-manager ConfigMap | `tls_trust_pool file /mnt/trusted-ca/ca-bundle.crt` |
| Backend services | Platform-dependent | `import /mnt/caddy-snippets/backend-tls.conf` |

Backend TLS is determined at init time by `generate-proxy-config.sh`:
- **OpenShift**: uses the service CA from `openshift-service-ca.crt` ConfigMap
  (auto-injected by the service-ca-operator).
- **Other platforms**: falls back to `tls_insecure_skip_verify`. To use
  proper verification, deploy cert-manager and configure it to issue
  certificates trusted by the proxy.

## Metrics

Caddy exposes native Prometheus metrics on `:2112/metrics`. A TODO exists
to protect this endpoint with
[kube-rbac-proxy](https://github.com/openshift/kube-rbac-proxy).

## File Reference

All proxy files live under `core/proxy/`:

| File | Purpose |
|------|---------|
| `Caddyfile` | Main server configuration |
| `tekton-results.caddy` | Template for Tekton Results backend route |
| `generate-proxy-config.sh` | Init container script: resolves backends, generates TLS config |
| `proxy.yaml` | Deployment and Service manifests |
| `rbac.yaml` | ServiceAccount and RBAC for the proxy |
| `kustomization.yaml` | Kustomize overlay tying everything together |
