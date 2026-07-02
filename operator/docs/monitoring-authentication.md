# Monitoring authentication options

> **Operational guide:** For component monitoring layout (scope, scrape models, migration
> checklist), see [component-monitoring.md](./component-monitoring.md). **Metrics-enabled
> components** are defined there (build-service, image-controller, integration-service).

Design comparison for enabling Prometheus metrics collection via the Konflux
operator. This document covers approaches that **do not use long-lived
credentials** (legacy `kubernetes.io/service-account-token` secrets).

## Background

### What infra-deployments does today

Legacy Konflux overlays authenticate metric scrapes with:

1. **kube-rbac-proxy** — a sidecar in front of the controller's `/metrics`
   endpoint. It terminates HTTPS and validates a bearer token via
   TokenReview / SubjectAccessReview against a `*-metrics-reader` ClusterRole.
2. **A static scrape token** — a Secret of type
   `kubernetes.io/service-account-token`, referenced from the ServiceMonitor
   as `bearerTokenSecret` (or `authorization.credentials`).

That pattern provides RBAC-protected metrics, but the scrape credential never
rotates. Kubernetes has discouraged this secret type since 1.24, and Konflux is
moving away from it elsewhere (for example, short-lived bound ServiceAccount
tokens for Dex in [konflux-ci#6962](https://github.com/konflux-ci/konflux-ci/pull/6962)).

### What the operator has today

- **Operator self-metrics** — deployed via `config/default/operator-rbac/` (`../../prometheus`):
  - HTTPS `:8443` with `filters.WithAuthenticationAndAuthorization` (`cmd/main.go`); no
    kube-rbac-proxy sidecar.
  - `ServiceMonitor` (`config/prometheus/monitor.yaml`) with `bearerTokenSecret` →
    `prometheus-scrape-token` (not `bearerTokenFile` — required for OpenShift UWM parity).
  - `ScrapeTokenRotator` (`internal/operatormetrics/`) mints and rotates
    `prometheus-scrape-token` in `konflux-operator`.
  - Scraper `ClusterRoleBinding` (`config/prometheus/metrics_reader_binding.yaml`) binds
    OCP UWM and Kind integration-test Prometheus SAs to `konflux-operator-metrics-reader`.
  - cert-manager metrics certs (`config/certmanager/`) are scaffolded but **not** enabled in
    the default `operator-rbac` kustomization yet (`insecureSkipVerify: true` on the
    ServiceMonitor until then).
- Metrics-enabled **operands** on the **operator scrape token** model (see
  [component-monitoring.md](./component-monitoring.md#scope)) — build-service,
  image-controller — use the same `bearerTokenSecret` + TokenRequest rotation pattern via
  operand reconcilers (`operator/pkg/kubernetes/scrape_token.go`). Legacy static
  `metrics-reader` SA/Secret manifests are removed from those `monitoring/` overlays.
- Metrics-enabled operands on the **legacy interim** model (integration-service) still use
  HTTP metrics on `:8080` plus a static `bearerTokenSecret` referencing a legacy
  `kubernetes.io/service-account-token` Secret.

### OpenShift UWM constraint

OpenShift user-workload Prometheus **rejects** ServiceMonitors that set
`bearerTokenFile` (`ArbitraryFSAccessThroughSMs`). Targets never appear in UWM.
Operand namespaces therefore use `bearerTokenSecret` referencing an operator-managed
Secret, even though the long-term kubebuilder default is `bearerTokenFile` on
clusters where the Prometheus Operator allows it.

### Constraints for the new solution

- **No long-lived scrape tokens** — no `kubernetes.io/service-account-token` Secrets;
  bound TokenRequest tokens with operator-driven rotation are acceptable.
- **No kube-rbac-proxy** — align with current kubebuilder / controller-runtime
  and prior team guidance to drop the sidecar.
- **Parity with legacy monitoring** — ServiceMonitors picked up by OpenShift
  user-workload Prometheus (UWM), Grafana dashboards continue to work.
- **Operator-managed** — auth resources created and owned by the operator
  when monitoring is enabled on a component.

### Kubebuilder guidance (does it mean mTLS?)

The [Kubebuilder metrics documentation](https://book.kubebuilder.io/reference/metrics#recommended-enabling-certificates-for-production-disabled-by-default)
does **not** drive towards mTLS as the authentication model. It describes two
**separate layers**:

| Layer | Kubebuilder mechanism | Purpose |
|-------|----------------------|---------|
| **Transport** | cert-manager `Certificate` for the metrics server (replaces dev self-signed certs) | Encrypt traffic; Prometheus verifies the **server** via `tlsConfig.ca` + `serverName` |
| **Application** | `filters.WithAuthenticationAndAuthorization` + scrape bearer token on the ServiceMonitor | Authenticate the scraper via Kubernetes TokenReview; authorize via RBAC (`metrics-reader` ClusterRole) |

On **OpenShift UWM**, the scrape bearer token is supplied via `bearerTokenSecret`
(operator-managed `prometheus-scrape-token`), not `bearerTokenFile`, because UWM blocks
arbitrary filesystem paths on ServiceMonitors.

Kubebuilder is explicit: *"You use those certificates to secure the transport
layer (TLS). The token authentication using authn/authz … serves as the
application-level credential."*

That matches **option 1** in this document (bearer token + verified server
TLS), not **option 2** (mTLS). The scaffolded `monitor_tls_patch.yaml`
references `cert` / `keySecret` from the same `metrics-server-cert` secret
as the server CA — Prometheus may present those fields, but controller-runtime
does **not** require or verify client certificates by default. They are not a
substitute for bearer-token auth in the kubebuilder model.

Other relevant kubebuilder points:

- **kube-rbac-proxy is deprecated** — projects on v4.1.0+ should use
  controller-runtime auth filters instead ([discussion](https://github.com/kubernetes-sigs/kubebuilder/discussions/3907)).
- **NetworkPolicy** is an optional extra layer; kubebuilder notes it does not
  handle authn/authz.
- **ClusterRoleBinding for scrapers** — kubebuilder scaffolds `metrics-reader` but does not
  bind it to a scraper SA by default; Konflux ships `metrics_reader_binding.yaml` for the
  operator and wires operand CRBs in each `monitoring/` overlay.

---

## Options

All options below avoid legacy SA-token secrets. They differ in *how* the
scrape is authenticated and what rotates automatically.

### 1. Kubernetes RBAC bearer token + server TLS (kubebuilder default)

This is the model described in the
[Kubebuilder metrics guide](https://book.kubebuilder.io/reference/metrics):
cert-manager (or OpenShift serving CA) for **transport**, projected SA token
for **application auth**.

**How it works**

| Layer | Mechanism |
|-------|-----------|
| Metrics server | Controller serves HTTPS on `:8443` with `filters.WithAuthenticationAndAuthorization` (built into controller-runtime, replaces kube-rbac-proxy). |
| Server TLS | cert-manager `Certificate` ([recommended for production](https://book.kubebuilder.io/reference/metrics#recommended-enabling-certificates-for-production-disabled-by-default)) or OpenShift service serving certificate. |
| Scrape auth | Prometheus presents a bearer token validated via TokenReview. **Kubebuilder default:** Prometheus pod projected SA token via `bearerTokenFile` (kubelet rotates it). **Konflux operands on OCP UWM:** `bearerTokenSecret` → operator-managed `prometheus-scrape-token` (TokenRequest for `prometheus-user-workload`, rotated by reconciler). |
| Authorization | `ClusterRole` `*-metrics-reader` (`nonResourceURLs: [/metrics], verbs: [get]`) bound to the **Prometheus scraper** ServiceAccount (e.g. `prometheus-user-workload` in UWM). |

**ServiceMonitor sketch**

```yaml
spec:
  endpoints:
  - path: /metrics
    port: https
    scheme: https
    bearerTokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
    tlsConfig:
      ca:
        secret:
          name: <server-ca-secret>
          key: ca.crt
      serverName: <service>.<namespace>.svc
```

**Pros**

- Native kubebuilder / controller-runtime path; operator `main.go` already uses it.
- Scrape credential rotates without operator intervention.
- Well-understood on OpenShift UWM when using `bearerTokenSecret` + operator-rotated tokens (see UWM constraint above).
- Minimal new infrastructure — RBAC + ServiceMonitor + server TLS certs.

**Cons**

- Still bearer-token based (short-lived, but a token nonetheless).
- Each controller must expose metrics with controller-runtime auth filters (upstream services on `:8080` HTTP need migration).
- Server TLS must be managed (cert-manager or OpenShift serving CA).

---

### 1b. Operator-managed scrape token (`bearerTokenSecret` on OCP)

Shipped for metrics-enabled components on the **operator scrape token** model when UWM
(or policy) blocks `bearerTokenFile`.

**How it works**

| Layer | Mechanism |
|-------|-----------|
| ServiceMonitor | `bearerTokenSecret: { name: prometheus-scrape-token, key: token }` in the operand namespace |
| Secret | Operand reconciler calls TokenRequest for the cluster Prometheus SA (`prometheus-user-workload` on OCP; `prometheus` in `konflux-monitoring-test` on Kind tests), writes an opaque Secret, requeues before expiry |
| Authorization | Same `*-metrics-reader` ClusterRole; CRB binds the **Prometheus scraper SA**, not a component-local `metrics-reader` SA |
| Rotation | Operator refresh at ~50% of token TTL remaining (not a static `kubernetes.io/service-account-token` Secret) |

**Pros**

- Works on OpenShift UWM (no `ArbitraryFSAccessThroughSMs` rejection).
- Short-lived credentials; no legacy SA-token Secret type.
- Self-contained in the operand namespace (SM + Secret + CRB).

**Cons**

- Operator must hold `serviceaccounts/token` create on Prometheus scraper SAs.
- Extra reconciler logic vs pure `bearerTokenFile`.
- Still a Secret in etcd (rotating opaque data, not a platform-projected mount).

---

### 2. Mutual TLS (mTLS)

**Not the kubebuilder default.** Kubebuilder uses certs for server TLS and
keeps bearer-token RBAC as the application credential. mTLS would **replace**
that application layer with client-certificate identity.

**How it works**

| Layer | Mechanism |
|-------|-----------|
| Metrics server | HTTPS with **client certificate required** (`tls.RequireAndVerifyClientCert`). Only holders of an approved client cert can connect. |
| Server TLS | cert-manager or OpenShift serving certificate. |
| Scrape auth | Prometheus presents a **client certificate** via ServiceMonitor `tlsConfig.cert` / `tlsConfig.keySecret`, issued by cert-manager to a dedicated scrape identity (ServiceAccount or per-cluster CA). |
| Authorization | Identity is the client cert's CN/URI/SAN — no bearer token, no Kubernetes RBAC check at scrape time (unless combined with option 1). |

**ServiceMonitor sketch**

```yaml
spec:
  endpoints:
  - path: /metrics
    port: https
    scheme: https
    tlsConfig:
      ca:
        secret: { name: metrics-server-ca, key: ca.crt }
      cert:
        secret: { name: prometheus-scrape-client-cert, key: tls.crt }
      keySecret: { name: prometheus-scrape-client-cert, key: tls.key }
      serverName: <service>.<namespace>.svc
```

**Pros**

- No bearer tokens at all; aligns with the high-level mTLS discussion.
- Strong cryptographic identity for scraper and server.
- cert-manager can issue short-lived certificates and renew them automatically.

**Cons**

- **Not the kubebuilder default** — controller-runtime metrics server would need custom TLS configuration to verify client certificates and maintain a client CA.
- More moving parts: server cert, client cert, client CA distribution, renewal, and potentially **per-scraper** or **per-service** cert management across all Konflux components.
- Upstream Konflux services do not implement this today; operator would own the full stack.
- Debugging scrape failures is harder (cert expiry, SAN mismatch, CA trust).

---

### 3. TLS encryption + NetworkPolicy (network segmentation only)

**How it works**

| Layer | Mechanism |
|-------|-----------|
| Metrics server | HTTPS (or HTTP on a cluster-internal port) without application-level authentication. |
| Access control | `NetworkPolicy` allows ingress to the metrics port **only** from the Prometheus namespace (or pods labeled as scrapers). The operator scaffold already has an example (`config/network-policy/allow-metrics-traffic.yaml`). |
| Scrape | Plain ServiceMonitor — no `bearerTokenFile`, no client cert. |

**Pros**

- Simplest to implement; no token or client-cert lifecycle.
- No long-lived credentials by definition.
- Composes well as a **baseline** alongside another option.

**Cons**

- **Not authentication** — any compromised pod in an allowed namespace can scrape metrics.
- Does not meet defense-in-depth expectations for multi-tenant clusters.
- Unlikely to satisfy security review on its own for production Konflux environments.

---

### 4. OpenShift service serving certificate (server TLS only)

**How it works**

OpenShift's service CA signs the Service's serving certificate. Prometheus
verifies the server using `service-ca.crt` (ConfigMap or projected secret).
This is how etcd-shield configures **server** TLS in infra-deployments, though
that ServiceMonitor still pairs it with a static bearer token (which we want
to drop).

Can be combined with option 1 (bearer + verified server TLS) or option 3
(network policy only).

**Pros**

- No cert-manager required on OpenShift; serving certs rotate with the
  service.
- Eliminates `insecureSkipVerify: true` common in legacy ServiceMonitors.

**Cons**

- OpenShift-specific; vanilla Kubernetes needs cert-manager instead.
- Server TLS alone does not authenticate the scraper.

---

### 5. Projected scrape token (dedicated identity)

A refinement of option 1. Instead of reusing the Prometheus pod's default
projected token, mount a **dedicated** projected ServiceAccount token (with
`expirationSeconds` and optional `audience`) into the Prometheus pod and
point `bearerTokenFile` at that mount path.

This is the same mechanism Konflux uses for UI backend tokens and the
direction of [konflux-ci#6962](https://github.com/konflux-ci/konflux-ci/pull/6962)
for Dex — short-lived, bound, audience-scoped credentials.

**Pros**

- Principle of least privilege: scrape token can be bound to a dedicated SA
  with only `*-metrics-reader`, separate from Prometheus's API-access token.
- Explicit TTL and audience control.

**Cons**

- Requires UWM / Prometheus Operator support for custom volume mounts or
  accepting `bearerTokenFile` paths beyond the default SA token (verify for
  `appstudio-workload-monitoring` Prometheus).
- More configuration than option 1 with marginal benefit if Prometheus SA is
  already minimally privileged.

---

## Comparison summary

| Option | Scrape credential | Rotates automatically | kube-rbac-proxy | Upstream fit | Operational complexity |
|--------|-------------------|----------------------|-----------------|--------------|------------------------|
| **1. Bearer token + server TLS** | Projected SA token (`bearerTokenFile`) or operator `prometheus-scrape-token` on OCP | Yes | No — controller-runtime filters | Strong — [kubebuilder default](https://book.kubebuilder.io/reference/metrics); OCP uses 1b | Low–medium |
| **2. mTLS** | Client certificate | Yes (cert-manager) | No | Not kubebuilder default — greenfield | High |
| **3. NetworkPolicy only** | None | N/A | No | Partial — NP scaffold exists | Very low |
| **4. OpenShift serving TLS** | None (server TLS only) | Yes (service CA) | No | Partial — used for server side | Low (OpenShift only) |
| **5. Projected scrape token** | Bound projected SA token | Yes (kubelet) | No | Strong — matches #6962 direction | Medium |

---

## Recommended direction

**Primary: option 1 (bearer token + verified server TLS)** — kubebuilder’s documented
production path. On **OpenShift UWM**, use **option 1b** (`bearerTokenSecret` +
operator-managed `prometheus-scrape-token`) because `bearerTokenFile` is rejected.

1. Enable cert-manager metrics certificates on each controller (or OpenShift
   serving CA — option 4).
2. ServiceMonitor with verified `tlsConfig.ca` / `serverName`
   (not `insecureSkipVerify: true`).
3. Scrape auth: `bearerTokenFile` where allowed; else `bearerTokenSecret` →
   `prometheus-scrape-token` with reconciler rotation.
4. `ClusterRoleBinding` from the Prometheus scraper SA to `*-metrics-reader`.

This gives:

- No kube-rbac-proxy ([deprecated by kubebuilder](https://book.kubebuilder.io/reference/metrics)).
- No legacy `kubernetes.io/service-account-token` scrape Secrets.
- Direct alignment with kubebuilder v4+ and the operator's `main.go` (for operator self-metrics).
- Migration from legacy infra-deployments: keep `*-metrics-reader` ClusterRole semantics,
  bind the **Prometheus scraper SA**, drop static token Secrets.

**mTLS (option 2)** is a distinct, stronger model that goes **beyond** what
Kubebuilder recommends. It is viable if security requirements explicitly
mandate client-certificate authentication *instead of* bearer tokens, but
expect custom controller-runtime TLS configuration across every Konflux
component. Treat it as optional hardening, not what the kubebuilder cert-manager
section implies.

**NetworkPolicy (option 3)** — kubebuilder lists this as an optional
supplement that does not replace authn/authz; use alongside option 1.

---

## Migration notes

| Legacy artifact | New approach |
|-----------------|--------------|
| kube-rbac-proxy sidecar | Remove; enable `metrics-secure` + controller-runtime auth filters on upstream controllers |
| `Secret` type `kubernetes.io/service-account-token` | Remove; use TokenRequest + `prometheus-scrape-token` (HTTPS operands) or `bearerTokenFile` where UWM allows |
| `bearerTokenSecret: metrics-reader` (static) | `bearerTokenSecret: prometheus-scrape-token` (operator-rotated) on OCP; or `bearerTokenFile` on Prometheus SA elsewhere |
| `ClusterRoleBinding` → `metrics-reader` SA | Rebind to Prometheus scraper SA (`prometheus-user-workload` on OCP UWM) |
| ServiceMonitor in `appstudio-workload-monitoring` with `namespaceSelector` | Keep if UWM Prometheus requires it; auth model changes regardless of placement |
| `config/prometheus/monitor.yaml` | Deployed with `bearerTokenSecret` + optional cert-manager TLS patches; not `bearerTokenFile` on OCP |

## Validation phases

Incremental plan to prove the recommended approach on the **operator's own
kubebuilder metrics** before rolling it out to Konflux components.

### What is already deployed (default operator install)

Via `config/default/operator-rbac/kustomization.yaml` (`make deploy` / CI operator install):

| Piece | Config | Deployed? |
|-------|--------|-----------|
| HTTPS metrics on `:8443` | `manager_metrics_patch.yaml` | Yes |
| `filters.WithAuthenticationAndAuthorization` | `cmd/main.go` | Yes |
| Metrics `Service` | `metrics_service.yaml` | Yes |
| `metrics-auth-role` on controller SA | `metrics_auth_role_binding.yaml` | Yes |
| `metrics-reader` ClusterRole | `metrics_reader_role.yaml` | Yes (as `konflux-operator-metrics-reader`) |
| Scraper `ClusterRoleBinding` | `config/prometheus/metrics_reader_binding.yaml` | **Yes** |
| `ServiceMonitor` | `config/prometheus/monitor.yaml` | **Yes** (`bearerTokenSecret` → `prometheus-scrape-token`) |
| `prometheus-scrape-token` rotation | `ScrapeTokenRotator` in `cmd/main.go` | **Yes** (when metrics enabled) |
| cert-manager metrics certs | `config/certmanager/` | **No** (directory exists; `#- ../certmanager` still commented in `operator-rbac`) |
| Verified TLS on ServiceMonitor | `monitor_tls_patch.yaml` | **No** (`insecureSkipVerify: true` until cert-manager enabled) |

Phase 2 below describes an **optional Kind/dev overlay** (`config/prometheus-stack/` +
uncommented cert-manager) used to validate the full kubebuilder + Prometheus path — it is
**not** the default production operator install today.

### Phase 1 — Auth only (manual)

**Goal:** Verify bearer-token RBAC auth on `/metrics` without Prometheus or
cert-manager. Uses the dev self-signed server certificate (`curl -k`).

**Prerequisites:** Operator running (e.g. `make deploy` on Kind).

**Steps:**

1. Confirm metrics endpoint is enabled:

   ```bash
   kubectl get svc -n konflux-operator \
     konflux-operator-controller-manager-metrics-service
   ```

2. Create the scraper binding **only if testing without the shipped operator deploy**
   (production and `make deploy` already include `metrics_reader_binding.yaml`):

   ```bash
   kubectl create clusterrolebinding konflux-operator-metrics-scraper \
     --clusterrole=konflux-operator-metrics-reader \
     --serviceaccount=konflux-operator:konflux-operator-controller-manager
   ```

   In production the subject is the **Prometheus scraper SA**, not the controller SA.
   Binding the controller SA is sufficient for a manual curl smoke test.

3. **Positive test** — token from a SA with `metrics-reader`:

   ```bash
   TOKEN=$(kubectl create token konflux-operator-controller-manager -n konflux-operator)
   kubectl run curl-metrics --rm -it --restart=Never \
     --image=curlimages/curl:8.5.0 -n konflux-operator -- \
     curl -sk -H "Authorization: Bearer ${TOKEN}" \
     https://konflux-operator-controller-manager-metrics-service.konflux-operator.svc:8443/metrics
   ```

   Expect **HTTP 200** and Prometheus text exposition (e.g. `workqueue_*`,
   `controller_runtime_*`).

4. **Negative test — no token:**

   ```bash
   kubectl run curl-metrics --rm -it --restart=Never \
     --image=curlimages/curl:8.5.0 -n konflux-operator -- \
     curl -sk -o /dev/null -w "HTTP %{http_code}\n" \
     https://konflux-operator-controller-manager-metrics-service.konflux-operator.svc:8443/metrics
   ```

   Expect **HTTP 401**.

5. **Negative test — token without permission:**

   ```bash
   TOKEN=$(kubectl create token default -n konflux-operator)
   kubectl run curl-metrics --rm -it --restart=Never \
     --image=curlimages/curl:8.5.0 -n konflux-operator -- \
     curl -sk -o /dev/null -w "HTTP %{http_code}\n" \
     -H "Authorization: Bearer ${TOKEN}" \
     https://konflux-operator-controller-manager-metrics-service.konflux-operator.svc:8443/metrics
   ```

   Expect **HTTP 403**.

**Phase 1 results (Kind cluster `kind-konflux`, 2026-06-23):**

| Test | Expected | Actual |
|------|----------|--------|
| Authorized bearer token | 200 | **200** |
| No `Authorization` header | 401 | **401** |
| `default` SA token (no binding) | 403 | **403** |

Binding used for the test: `konflux-operator-metrics-scraper-test` →
`konflux-operator-metrics-reader` → `konflux-operator-controller-manager`.

### Phase 2 — Kind/dev overlay (cert-manager + local Prometheus)

**Goal:** Validate the full kubebuilder production stack on a **local Kind cluster** —
cert-manager server TLS, ServiceMonitor with verified CA, and a standalone Prometheus
instance. This is **not** part of the default operator install; it uses extra kustomize
overlays documented here for manual experimentation.

**Prerequisites:** Phase 1 passing; cert-manager already installed on cluster.

#### What the dev overlay adds

1. **`config/certmanager/`** — metrics `Issuer` + `Certificate` (secret
   `metrics-server-cert`) and `kustomizeconfig.yaml` so `namePrefix` rewrites
   `issuerRef.name` correctly (`konflux-operator-selfsigned-issuer`).

2. **Uncomment / enable in `config/default/operator-rbac/kustomization.yaml`** (not
   enabled in the default branch today):
   - `../../certmanager` alongside `../../prometheus`
   - `cert_metrics_manager_patch.yaml` (mounts certs, `--metrics-cert-path`)
   - `replacements` for Certificate DNS names and ServiceMonitor `serverName`
   - `monitor_tls_patch.yaml` in `config/prometheus/kustomization.yaml`

3. **`config/prometheus-stack/`** — Kind-only Prometheus instance (**not** part of
   production operator deploy):
   - `Prometheus` CR in `monitoring` namespace
   - `prometheus` ServiceAccount + discovery RBAC
   - `ClusterRoleBinding` `prometheus-konflux-operator-metrics-reader`

4. **Cluster setup (manual):**
   - Prometheus Operator v0.77.1 (`kubectl apply --server-side` — required on
     Kind to avoid CRD annotation size errors)
   - Restart `prometheus-operator` after CRDs are installed (operator logs
     `prometheuses not installed` if started too early)
   - `kubectl apply` of `kustomize build config/default` (with cert-manager enabled)
   - `kubectl apply` of `kustomize build config/prometheus-stack`

#### Gotcha fixed during testing

The initial `Certificate` stayed in `Issuing` because `namePrefix` renamed the
`Issuer` but not `spec.issuerRef.name` until `kustomizeconfig.yaml` was added.

#### Phase 2 results (Kind cluster `kind-konflux`, 2026-06-23 — dev overlay)

| Check | Result |
|-------|--------|
| `Certificate` `konflux-operator-metrics-certs` Ready | **Yes** |
| Secret `metrics-server-cert` created | **Yes** |
| Operator pod uses `--metrics-cert-path` | **Yes** |
| `ServiceMonitor` created with CA from `metrics-server-cert` | **Yes** |
| Prometheus target health | **up** |
| `workqueue_depth` series scraped | **Yes** (15 series) |

Prometheus scrape URL:
`https://<operator-pod-ip>:8443/metrics` via ServiceMonitor
`konflux-operator-controller-manager-metrics-monitor`.

Scraper auth in this experiment: `prometheus` SA in `monitoring` namespace (projected
token via `bearerTokenFile` in the dev overlay) bound to `konflux-operator-metrics-reader`.
**Production / default deploy** uses `bearerTokenSecret` → `prometheus-scrape-token`
instead (same as operands on OCP).

#### Reproduce locally (dev overlay — enable cert-manager in kustomization first)

```bash
# Prometheus Operator (server-side apply for Kind)
curl -fsSL -o /tmp/po-bundle.yaml \
  https://github.com/prometheus-operator/prometheus-operator/releases/download/v0.77.1/bundle.yaml
kubectl apply --server-side --force-conflicts -f /tmp/po-bundle.yaml
kubectl rollout restart deployment/prometheus-operator -n default

# Operator with metrics certs + ServiceMonitor (uncomment certmanager in operator-rbac first)
cd operator && bin/kustomize build config/default | kubectl apply -f -
kubectl wait certificate/konflux-operator-metrics-certs -n konflux-operator \
  --for=condition=Ready --timeout=120s
kubectl rollout status deployment/konflux-operator-controller-manager -n konflux-operator

# Kind-only Prometheus stack (not part of default deploy)
bin/kustomize build config/prometheus-stack | kubectl apply -f -

# Verify (after ~30s scrape interval)
kubectl exec -n monitoring prometheus-prometheus-0 -c prometheus -- \
  wget -qO- 'http://localhost:9090/api/v1/targets' | jq '.data.activeTargets[] | select(.labels.namespace=="konflux-operator") | {health,lastError}'
```

### Phase 3 — Automated e2e

**Goal:** CI-durable test covering metrics scrape auth (partially done — see
`test/go-tests/metricsintegration/` and `scripts/operator-e2e/run-metrics-integration-tests.sh`).

**Remaining work items:**

1. Create `operator/test/e2e/` if full Prometheus-operator-in-the-loop coverage is
   needed (Makefile references it but directory is empty).
2. Optional: Kind overlay enabling cert-manager metrics certs + verified TLS on
   ServiceMonitor (extends Phase 2 dev overlay).
3. Assert: `Certificate` Ready, Prometheus target up, metrics query returns data,
   unauthenticated scrape fails.

### Phase 4 — OpenShift UWM integration

**Goal:** Parity with legacy infra-deployments monitoring on
`development-operator`.

**Work items:**

1. Reconcile `ServiceMonitor` with label `prometheus: appstudio-workload` if
   required by UWM Prometheus in `appstudio-workload-monitoring`.
2. Bind Prometheus scraper SA to `metrics-reader` — on current OCP UWM use
   `prometheus-user-workload` in `openshift-user-workload-monitoring` (not only
   legacy `prometheus-k8s` in `appstudio-workload-monitoring`).
3. Use OpenShift serving CA or cert-manager for server TLS verification.
4. Confirm Grafana dashboards still resolve operator metrics.

---

## Open questions

1. **Upstream controllers** — metrics-enabled components on the operator scrape token
   model bind metrics on `:8443` without kube-rbac-proxy in operator manifests; legacy
   interim components still use `:8080` HTTP. Does monitoring enablement include
   patching all metrics-enabled components to the secure metrics model?
2. **UWM Prometheus SA name** — OCP user-workload monitoring uses
   `prometheus-user-workload` in `openshift-user-workload-monitoring`; legacy docs
   referenced `prometheus-k8s` in `appstudio-workload-monitoring`.
3. **mTLS vs kubebuilder model** — confirm stakeholders understand kubebuilder
   cert-manager integration is **server TLS + bearer token**, not mTLS. Is
   option 2 a separate mandate beyond that?
4. **cert-manager dependency** — on non-OpenShift clusters, is cert-manager
   already a Konflux operator dependency for webhooks (reuse for metrics
   server certs)?
