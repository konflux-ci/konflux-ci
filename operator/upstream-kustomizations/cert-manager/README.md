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
   `konflux-ca-secret`. This is the "real" issuer that all Konflux services
   reference.

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
ClusterIssuer. Individual component controllers then manage their own
Certificate/Issuer resources in their respective namespaces.
