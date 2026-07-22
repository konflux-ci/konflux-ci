# Konflux PKI (Public Key Infrastructure)

Konflux uses [cert-manager](https://cert-manager.io/) to manage TLS certificates
for internal service-to-service communication. Most services share a cluster-wide
trust root (`konflux-issuer`). The UI namespace uses its own self-signed root for the
OpenShift route trust chain (see "Why the UI uses a self-signed root CA" below).

## Architecture

```
konflux-bootstrap-issuer (SelfSigned ClusterIssuer)
│
│   Bootstraps both the cluster root CA and the UI namespace root.
│
├── konflux-ca (Certificate, isCA: true, cert-manager ns)
│       │
│       ▼
│   Secret "konflux-ca-secret" (cert-manager namespace)
│   Contains the cluster root CA key + cert.
│       │
│       ▼
│   konflux-issuer (ClusterIssuer)
│   The issuer for cluster-wide services.
│       │
│       ├── namespace-lister cert (leaf, in namespace-lister ns)
│       ├── registry cert (leaf, in kind-registry ns)
│       ├── metrics-certs → metrics-server-cert (leaf + ca.crt, in each
│       │     metrics-enabled operand ns)
│       └── cluster-root-ref (leaf, in konflux-ui ns — only for ca.crt)
│
└── ui-ca (Certificate, isCA: true, self-signed, in konflux-ui ns)
        │
        ▼
    ui-ca-issuer (namespace Issuer, in konflux-ui ns)
        │
        ├── serving-cert (leaf, proxy TLS on port 9443)
        ├── dex-cert (leaf, Dex TLS)
        └── oauth2-proxy-cert (CA bundle for oauth2-proxy → Dex)

Operator manager metrics (install-time exception — not under konflux-issuer):

metrics-selfsigned-issuer (namespace Issuer, SelfSigned, in konflux-operator)
    └── metrics-certs → metrics-server-cert (same Secret shape as operands)
```

## Bootstrap sequence

1. **`konflux-bootstrap-issuer`** — A SelfSigned ClusterIssuer. Its only job is
   to issue the root CA certificate. If you could manually create a CA key pair
   and store it in a Secret, you wouldn't need this at all. It's purely a
   cert-manager pattern for declarative root CA generation.

2. **`konflux-ca` Certificate** — An `isCA: true` Certificate issued by the
   bootstrap issuer. cert-manager generates a key pair and stores it in
   `konflux-ca-secret` in the `cert-manager` namespace.

3. **`konflux-issuer` ClusterIssuer** — A CA issuer backed by
   `konflux-ca-secret`. This is the issuer most Konflux services reference
   (operand metrics, namespace-lister, registry, UI `cluster-root-ref`). The
   **operator manager** metrics Certificate is the install-time exception
   (see below): it cannot wait for this step.

Ordering for metrics: cert-manager must already be installed; the operator
must be Running so `KonfluxCertManager` can create `konflux-issuer`; then
operand `metrics-certs` Certificates can become Ready. Operand Deployments
use `optional: false` on the metrics volume, so pods stay Pending until
cert-manager has written `metrics-server-cert`.

## How services get their certificates

Services reference `konflux-issuer` (or a namespace-scoped sub-CA issued by it)
in their Certificate resources:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: my-service
  namespace: my-namespace
spec:
  dnsNames:
    - my-service.my-namespace.svc.cluster.local
  issuerRef:
    kind: ClusterIssuer
    name: konflux-issuer
  secretName: my-service-tls
```

cert-manager creates a Secret containing:

| Field     | Content                                    |
|-----------|--------------------------------------------|
| `tls.crt` | The leaf certificate (+ chain if intermediate) |
| `tls.key` | The private key                            |
| `ca.crt`  | The CA certificate that signed this cert   |

### Metrics scrape endpoints

Verified metrics scrape TLS uses that Secret shape with fixed names:

| Piece | Value |
|-------|--------|
| Certificate | `metrics-certs` |
| Secret | `metrics-server-cert` |
| Pod mount | `tls.crt` / `tls.key` only |
| ServiceMonitor trust | `tlsConfig.ca.secret` → `metrics-server-cert` / `ca.crt`, plus `serverName` matching the metrics Service DNS; **not** `insecureSkipVerify: true` |

Scrapers trust **that endpoint’s** Secret `ca.crt`. There is no cluster-wide
metrics trust bundle — so the operator’s SelfSigned leaf is fine for its own
ServiceMonitor even though it does not chain to `konflux-ca`.

**Operands** (build-service, image-controller, release-service): Certificate
`issuerRef` is `ClusterIssuer/konflux-issuer`. Manifests live under
`operator/upstream-kustomizations/<component>/certmanager/` (and the matching
`mount-metrics-server-cert.yaml` / monitoring ServiceMonitor). Example leaf:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: metrics-certs
  namespace: build-service
spec:
  dnsNames:
    - SERVICE_NAME.SERVICE_NAMESPACE.svc
    - SERVICE_NAME.SERVICE_NAMESPACE.svc.cluster.local
  issuerRef:
    kind: ClusterIssuer
    group: cert-manager.io
    name: konflux-issuer
  secretName: metrics-server-cert
```

ServiceMonitor fragment:

```yaml
tlsConfig:
  insecureSkipVerify: false
  serverName: build-service-controller-manager-metrics-service.build-service.svc
  ca:
    secret:
      name: metrics-server-cert
      key: ca.crt
```

**Operator manager** (`operator/config/certmanager/`): same Secret / SM shape,
but issued by a **namespace-local SelfSigned Issuer**. It cannot use
`konflux-issuer` at install time (chicken-egg with `optional: false` metrics
mount). Use a short `commonName` (`konflux-operator-metrics`): SelfSigned +
dnsNames-only yields an empty Subject DN, which breaks OpenSSL/`curl --cacert`
probes even when Go/Prometheus succeeds. Do not put the full metrics Service
FQDN in `commonName` (X.509 limit 64 bytes); SANs in `dnsNames` remain the
identity scrapers check via `serverName`.

Scrape **auth** (tokens, RBAC, deferred ServiceMonitor apply) is documented in
[`operator/docs/component-monitoring.md`](../../docs/component-monitoring.md),
not here.

### Adding verified metrics TLS to a new operand

1. Ensure the component already follows the operator scrape-token model in
   `component-monitoring.md` (HTTPS `:8443`, `prometheus-scrape-token`, etc.).
2. Add `certmanager/certificate.yaml` like the build-service example above
   (`konflux-issuer` → `metrics-server-cert`); wire kustomize `dnsNames`
   replacements from the metrics Service.
3. Mount `tls.crt`/`tls.key` only (`optional: false`), at controller-runtime’s
   default CertDir or via `--metrics-cert-path`.
4. Point the ServiceMonitor at `metrics-server-cert` / `ca.crt` with the correct
   `serverName`; drop `insecureSkipVerify`.
5. Do **not** invent a per-namespace metrics CA unless you have a bootstrap
   constraint like the operator manager.

Reference overlays: `build-service`, `image-controller`, `release-service`
under `operator/upstream-kustomizations/`.
## Why the UI uses a self-signed root CA

OpenShift's Ingress-to-Route controller reads `tls.crt` (not `ca.crt`) from the
Secret referenced by the `route.openshift.io/destination-ca-certificate-secret`
annotation. The router uses this as a trust anchor to verify the backend's TLS.

For TLS re-encryption to succeed, the trust anchor must be a **self-signed**
CA:TRUE certificate — HAProxy requires a complete chain ending at a self-signed
root. cert-manager intentionally omits self-signed roots from `tls.crt` of
certificates it issues (per TLS spec), so an intermediate CA issued by
`konflux-issuer` would produce a `tls.crt` containing only the intermediate, causing
chain verification failures (503 errors).

The fix: `ui-ca` is a **self-signed** root CA (issued by
`konflux-bootstrap-issuer`). Its `tls.crt` is inherently self-signed, so the
router can verify: `serving-cert → ui-ca (self-signed root)`.

## Cross-service TLS verification

The UI namespace has two trust domains:

1. **Internal (ui-ca)** — Dex, serving-cert, and oauth2-proxy-cert all chain to
   `ui-ca`. The proxy verifies Dex using `serving-cert`'s `ca.crt` (= `ui-ca`).

2. **Cluster (konflux-issuer)** — namespace-lister and other cluster services chain
   to the cluster root (`konflux-ca-secret`). The proxy verifies namespace-lister
   using `cluster-root-ref`'s `ca.crt` field, which cert-manager populates with
   the cluster root CA (the signing CA of `konflux-issuer`).

## Operator management

The `KonfluxCertManager` controller (reconciling the `KonfluxCertManager` CR)
manages the bootstrap resources in this directory. When
`spec.createClusterIssuer` is true (the default), it applies
`konflux-bootstrap-issuer`, `konflux-ca` Certificate, and `konflux-issuer`
ClusterIssuer. Component controllers then apply their own leaf Certificates
(and any namespace Issuers they need, e.g. UI or webhook self-signed). Operand
**metrics** leaves reference `konflux-issuer` directly — no per-namespace
metrics Issuer. The operator manager metrics Issuer+Certificate are install
manifests in `operator/config/certmanager/`, not reconciled by
`KonfluxCertManager`.
