# Konflux PKI (Public Key Infrastructure)

Konflux uses [cert-manager](https://cert-manager.io/) to manage TLS certificates
for internal service-to-service communication. All services share a single trust
root so any service can verify any other service's certificate using just the
root CA.

## Architecture

```
self-signed-cluster-issuer (SelfSigned ClusterIssuer)
│
│   Only purpose: bootstrap the root CA declaratively.
│   Not used by any service directly.
│
└── selfsigned-ca (Certificate, isCA: true)
        │
        ▼
    Secret "root-secret" (cert-manager namespace)
    Contains the root CA key + cert.
        │
        ▼
    ca-issuer (ClusterIssuer)
    The single issuer all Konflux services use.
        │
        ├── namespace-lister cert (leaf, in namespace-lister ns)
        ├── registry cert (leaf, in kind-registry ns)
        │
        └── ui-ca (sub-CA, isCA: true, in konflux-ui ns)
                │
                ▼
            ui-ca-issuer (namespace Issuer, in konflux-ui ns)
                │
                ├── serving-cert (leaf, proxy TLS on port 9443)
                ├── dex-cert (leaf, Dex TLS)
                └── oauth2-proxy-cert (CA bundle for oauth2-proxy → Dex)
```

## Bootstrap sequence

1. **`self-signed-cluster-issuer`** — A SelfSigned ClusterIssuer. Its only job is
   to issue the root CA certificate. If you could manually create a CA key pair
   and store it in a Secret, you wouldn't need this at all. It's purely a
   cert-manager pattern for declarative root CA generation.

2. **`selfsigned-ca` Certificate** — An `isCA: true` Certificate issued by the
   self-signed issuer. cert-manager generates a key pair and stores it in
   `root-secret` in the `cert-manager` namespace.

3. **`ca-issuer` ClusterIssuer** — A CA issuer backed by `root-secret`. This is
   the "real" issuer that all Konflux services reference.

## How services get their certificates

Services reference `ca-issuer` (or a namespace-scoped sub-CA issued by it) in
their Certificate resources:

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
    name: ca-issuer
  secretName: my-service-tls
```

cert-manager creates a Secret containing:

| Field     | Content                                    |
|-----------|--------------------------------------------|
| `tls.crt` | The leaf certificate (+ chain if intermediate) |
| `tls.key` | The private key                            |
| `ca.crt`  | The CA certificate that signed this cert   |

## Why the UI uses a sub-CA

The UI (`konflux-ui` namespace) has a special constraint: OpenShift's
Ingress-to-Route controller reads `tls.crt` (not `ca.crt`) from the Secret
referenced by the `route.openshift.io/destination-ca-certificate-secret`
annotation. It expects a **CA certificate** (CA:TRUE) in that field.

If the UI's serving-cert were issued directly by `ca-issuer`, the Secret's
`tls.crt` would be a leaf cert (CA:FALSE) — which the OpenShift router rejects,
causing 503 errors.

The fix: create `ui-ca` as a sub-CA (isCA: true) issued by `ca-issuer`. Its
Secret has `tls.crt` = a CA cert, satisfying the Route. A namespace-scoped
`ui-ca-issuer` then issues the actual leaf certs (serving-cert, dex-cert).

## Cross-service TLS verification

Since all certificates chain to the same root (`root-secret`), any service can
verify any other service's TLS certificate using the root CA cert.

For example, the UI proxy verifies the namespace-lister's certificate by mounting
`ui-ca` Secret's `ca.crt` field — which contains the root CA cert (because
cert-manager stores the issuing CA in `ca.crt`, and `ui-ca` is issued by
`ca-issuer` which uses `root-secret`).

If a service has its own intermediate CA (like the UI), the chain is:
`leaf → intermediate → root`. The verifying party only needs the root CA to
validate the entire chain, because the intermediate cert is included in the
`tls.crt` bundle sent during the TLS handshake.

## Operator management

The `KonfluxCertManager` controller (reconciling the `KonfluxCertManager` CR)
manages the bootstrap resources in this directory. When
`spec.createClusterIssuer` is true (the default), it applies the
self-signed-cluster-issuer, selfsigned-ca Certificate, and ca-issuer
ClusterIssuer. Individual component controllers then manage their own
Certificate/Issuer resources in their respective namespaces.
