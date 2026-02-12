---
title: "Custom URL without Operator-Managed Ingress"
linkTitle: "Custom URL"
weight: 1
description: "How to configure a custom URL for Konflux UI when managing your own ingress or external routing."
---

The Konflux operator can manage an Ingress resource for the UI automatically.
When ingress is enabled (the default on OpenShift), the operator creates and
maintains the Ingress. When ingress is disabled (the default on plain
Kubernetes), the UI is accessible via port-forwarding on `localhost:9443`.

In both environments, you may want to manage your own routing — for example,
using a standard Kubernetes Ingress with your own ingress controller, a hardware
load balancer, or any other external routing mechanism.

This guide shows how to configure the operator so that all internal components
(oauth2-proxy, dex, etc.) use your custom hostname, **without** the operator
creating or managing an Ingress resource.

## Configuration

Set `ingress.enabled: false` and `ingress.host` to your desired hostname in the
Konflux CR:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  ui:
    spec:
      ingress:
        enabled: false
        host: konflux.example.com
```

With this configuration:

- **`ingress.enabled: false`** — the operator will **not** create an Ingress
  resource. You are responsible for routing traffic to the `proxy` service in the
  `konflux-ui` namespace.
- **`ingress.host`** — the operator configures oauth2-proxy, dex, and all
  related components to use this hostname for OIDC redirect URLs, issuer URLs,
  and allowed redirect domains. This ensures authentication flows work correctly
  with your custom URL.

## Routing to the Backend

Regardless of which routing method you choose, traffic must reach the **`proxy`**
service in the `konflux-ui` namespace on port **`web-tls`** (port 9443). The
proxy service terminates TLS using a certificate signed by the `ui-ca` CA
(managed by cert-manager).

### Kubernetes Ingress

On plain Kubernetes, create an Ingress resource with your ingress controller. The
exact annotations depend on your ingress controller (nginx, traefik, etc.):

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: konflux-ui
  namespace: konflux-ui
  annotations:
    # Add annotations specific to your ingress controller here.
    # The backend (proxy service) uses TLS, so you may need to configure
    # your ingress controller for backend TLS / SSL passthrough.
spec:
  ingressClassName: nginx  # Adjust to your ingress controller
  rules:
    - host: konflux.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: proxy
                port:
                  name: web-tls
  tls:
    - hosts:
        - konflux.example.com
      secretName: my-tls-secret  # Your TLS certificate for the edge
```

> **Note:** Since the backend proxy service uses TLS (port `web-tls` / 9443),
> you may need to configure your ingress controller to trust the backend CA
> certificate. The CA certificate is stored in the `ui-ca` Secret in the
> `konflux-ui` namespace.

### OpenShift Ingress (Route)

On OpenShift, the Ingress-to-Route controller automatically converts Ingress
resources into Routes. Use the OpenShift-specific annotations for TLS
re-encryption:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: konflux-ui
  namespace: konflux-ui
  annotations:
    route.openshift.io/destination-ca-certificate-secret: ui-ca
    route.openshift.io/termination: reencrypt
spec:
  rules:
    - host: konflux.apps.my-cluster.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: proxy
                port:
                  name: web-tls
```

Key points:

- **`route.openshift.io/termination: reencrypt`** — the router terminates the
  external TLS connection and creates a new TLS connection to the backend.
- **`route.openshift.io/destination-ca-certificate-secret: ui-ca`** — tells the
  router to trust the CA certificate from the `ui-ca` Secret when connecting to
  the backend proxy service.

### Other Routing Methods

The same approach works with any routing mechanism (Gateway API, hardware load
balancer, service mesh, etc.) as long as traffic reaches the `proxy` service on
port 9443 with TLS re-encryption. The backend proxy serves TLS using a
certificate signed by the `ui-ca` CA — your routing layer must be configured to
trust this CA for the backend connection.
