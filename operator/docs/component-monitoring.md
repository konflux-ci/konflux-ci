# Component monitoring

How Konflux operator deploys Prometheus metrics scraping for controller components.

For auth options and Kind/OCP validation notes, see
[monitoring-authentication.md](./monitoring-authentication.md).

## Scope

**Metrics-enabled components** — operands whose reconcilers honor
`spec.componentMetrics`: **build-service**, **image-controller**, and
**integration-service**.

When `componentMetrics.enabled` is false, those reconcilers skip `monitoring/`
resources and delete previously applied scrape objects. **Operator self-metrics**
(Konflux operator Deployment ServiceMonitor) are not controlled by this field.

Within metrics-enabled components, scrape models differ today:

| Model | Components | Summary |
|-------|------------|---------|
| Operator scrape token | build-service, image-controller | HTTPS + `prometheus-scrape-token` (TokenRequest, rotated) |
| Legacy interim | integration-service | HTTP `:8080` + static `bearerTokenSecret` (`*-metrics-reader` Secret) |
| Not wired yet | release-service | `monitoring/` overlay exists; not tied to `componentMetrics` |

## Cluster knob: `spec.componentMetrics`

The Konflux CR exposes one switch for all metrics-enabled components:

```yaml
spec:
  componentMetrics:
    enabled: false   # omit or true to deploy ServiceMonitor + scrape RBAC
```

- **Default:** `enabled` is treated as **true** when unset.

On Kind, `deploy-deps.sh` installs the ServiceMonitor CRD by default. Set
`componentMetrics.enabled: false` on the Konflux CR if you skip CRD install.

## Third-party CRD pin (Kind + envtest)

The ServiceMonitor CRD is vendored like cert-manager envtest CRDs:

| Pin | `.github/scripts/export-third-party-chart-env.sh` → `PROMETHEUS_OPERATOR_VERSION` |
| Generate | `.github/scripts/update-third-party-manifests.sh` |
| envtest | `operator/test/crds/prometheus/` |
| Kind deps | `dependencies/prometheus-operator-crds/` (applied in `deploy-deps.sh`) |

Renovate bumps the pin; `verify-manifests-in-sync` fails if generated files drift.
Override at deploy time with `SKIP_PROMETHEUS_OPERATOR_CRDS=true` when the CRD is
already present (e.g. OpenShift UWM).

## Repo layout

Each component that exposes controller metrics follows this split:

```
operator/upstream-kustomizations/<component>/
├── kustomization.yaml      # includes core + monitoring (+ certmanager where needed)
├── core/                   # operand: Deployment, Service, RBAC, webhooks, …
│   └── …                   # patches that *remove* upstream ServiceMonitor + scrape SA/Secret
└── monitoring/             # operator-owned scrape contract (ServiceMonitor + metrics-reader RBAC)
    └── kustomization.yaml
```

Built manifests land in `operator/pkg/manifests/<component>/manifests.yaml` via
`operator/pkg/manifests/process-component.sh`.

**Rule:** upstream remote kustomizations may ship monitoring scaffolding; `core/` strips
those so they are not duplicated. `monitoring/` is the single source of truth for what
the operator reconciles.

## Target architecture (unified)

Aligns with [kubebuilder v4 metrics](https://book.kubebuilder.io/reference/metrics) and
the Konflux operator’s own metrics server.

| Piece | Target |
|-------|--------|
| Metrics server | HTTPS `:8443`, controller-runtime `WithAuthenticationAndAuthorization` (no kube-rbac-proxy) |
| Server TLS | cert-manager `Certificate` → `metrics-server-cert` (or OpenShift serving CA) |
| ServiceMonitor | `scheme: https`, `port: https`, `bearerTokenFile` (Prometheus pod token) **where the Prometheus Operator allows it** |
| TLS verify | `tlsConfig.ca` + `serverName` — **not** `insecureSkipVerify: true` |
| Authorization | `<component>-metrics-reader` ClusterRole bound to the **Prometheus scraper SA** (e.g. `prometheus-user-workload` on OpenShift UWM) |
| Scrape credentials | **No** long-lived `kubernetes.io/service-account-token` Secrets |

**OpenShift UWM caveat:** User-workload Prometheus rejects ServiceMonitors with
`bearerTokenFile` (`ArbitraryFSAccessThroughSMs`). On OCP, the unified kubebuilder
`bearerTokenFile` model may not be achievable until platform policy changes; see
**Shipped today** below for the operator-managed `bearerTokenSecret` workaround.

Operator self-metrics reference: `operator/config/prometheus/` (ServiceMonitor deployed
via `config/default/operator-rbac/`; cert-manager patches optional — see
[monitoring-authentication.md](./monitoring-authentication.md#validation-phases)).

## Shipped today (operator scrape token)

Applies to metrics-enabled components on the **operator scrape token** model (see
[Scope](#scope)).

| Piece | Shipped |
|-------|---------|
| Metrics server | HTTPS `:8443` with auth filters (`insecureSkipVerify: true` on ServiceMonitor for now) |
| ServiceMonitor | `bearerTokenSecret` → `prometheus-scrape-token` in the operand namespace |
| Scrape Secret | **Not** in kustomize — reconciler mints a bound token via TokenRequest for the cluster Prometheus SA and writes `prometheus-scrape-token`; refreshes before expiry |
| Authorization | `<component>-metrics-reader` ClusterRole; CRB subjects include `prometheus-user-workload` (OCP UWM), `prometheus-k8s` (legacy UWM name), and `prometheus` in `konflux-monitoring-test` (Kind integration tests) |
| Legacy SA/Secret | Removed from `monitoring/` overlays for these components |

Implementation: `operator/pkg/kubernetes/scrape_token.go`, wired from operand reconcilers
when `spec.componentMetrics` is enabled.

Example manifests: `operator/upstream-kustomizations/build-service/monitoring/`.

### Operator self-metrics

The Konflux operator manager uses the same **operator scrape token** model in
`konflux-operator` (not gated by `spec.componentMetrics`):

| Piece | Shipped |
|-------|---------|
| Metrics server | HTTPS `:8443` with auth filters (`cmd/main.go`) |
| ServiceMonitor | `config/prometheus/monitor.yaml` — `bearerTokenSecret` → `prometheus-scrape-token` |
| Scrape Secret | `ScrapeTokenRotator` in `cmd/main.go` mints and rotates `prometheus-scrape-token` |
| Scraper CRB | `config/prometheus/metrics_reader_binding.yaml` (OCP UWM + Kind test scraper SAs) |
| Server TLS (cert-manager) | Optional — `config/certmanager/` exists; not enabled in default `operator-rbac` kustomization yet |

Cluster integration tests scrape the operator target via TokenRequest for the Kind
test scraper SA (`test/fixtures/metrics-scraper/rbac.yaml`), not the operand Secret.

## Legacy interim

Applies to metrics-enabled components still on the **legacy interim** model (see
[Scope](#scope)), and to release-service when its overlay is wired.

| Piece | Legacy interim |
|-------|----------------|
| Metrics server | HTTP `:8080` (no auth on metrics yet) |
| ServiceMonitor | `scheme: http` and/or `bearerTokenSecret` → static `*-metrics-reader` Secret |
| Authorization | `<component>-metrics-reader` ClusterRole bound to a dedicated **`metrics-reader` ServiceAccount** in the component namespace |
| Scrape credentials | Legacy SA token Secret (`type: kubernetes.io/service-account-token`) |

Example: integration-service ServiceMonitor uses `scheme: http`, `port: http`, and
`bearerTokenSecret` → `integration-service-metrics-reader` (static legacy Secret).

**Why legacy interim remains:** Those controllers do not expose HTTPS authenticated metrics
yet; Prometheus can scrape without waiting for upstream `--metrics-secure` and cert-manager.

## Migrate a component: legacy interim → unified

Components on the **operator scrape token** model have already dropped static-token interim;
remaining work is mostly cert-manager server TLS and verified scrape TLS (see unified target).

Components on the **legacy interim** model (and release-service when wired) should migrate
in order below. Skip steps that already apply to operator scrape token components.

### 1. Upstream controller (service repository)

- [ ] Bind metrics on `:8443` with `--metrics-secure=true`
- [ ] Remove kube-rbac-proxy sidecar if present
- [ ] Keep `metrics_auth_role*` and `metrics_reader_role` in upstream RBAC
- [ ] Stop shipping upstream ServiceMonitor, scrape SA, and static token Secret in `config/default`

**Check:** `kubectl create token …` + `curl -k -H "Authorization: Bearer …" https://…:8443/metrics` → 200; no token → 401.

### 2. Operator `core/` + cert-manager

- [ ] Add or extend `certmanager/` with a metrics `Certificate` (secret `metrics-server-cert`)
- [ ] Patch Deployment: mount cert volume, `--metrics-cert-path=…`
- [ ] Add kustomize `replacements` for ServiceMonitor `serverName` (see operator deploy kustomization)
- [ ] Keep `core/` patches that delete upstream monitoring resources

### 3. Operator `monitoring/` overlay

**Remove (legacy interim only):**

- [ ] `v1_secret_*-metrics-reader.yaml`
- [ ] `v1_serviceaccount_*-metrics-reader.yaml`

**Keep:**

- [ ] `<component>-metrics-reader` ClusterRole
- [ ] `prometheus-*-metrics-reader` ClusterRoleBinding with Prometheus scraper SA subjects

**ServiceMonitor (HTTPS components):**

- [ ] `scheme: https`, `port: https`
- [ ] `bearerTokenSecret` → `prometheus-scrape-token` (operand namespace) — **required on OpenShift UWM**; do not use `bearerTokenFile` there
- [ ] On clusters that allow `bearerTokenFile`, optional future simplification: Prometheus pod projected token instead of operand Secret
- [ ] Replace `insecureSkipVerify: true` with `tlsConfig.ca` from `metrics-server-cert` and correct `serverName`

**Operator reconciler (HTTPS components on OCP / Kind):**

- [ ] Wire `EnsurePrometheusScrapeToken` (mint TokenRequest for Prometheus SA, rotate Secret)
- [ ] Do **not** embed `prometheus-scrape-token` in kustomize — reconciler creates it

**ClusterRoleBinding:**

- [ ] Subjects: Prometheus scraper SA (`prometheus-user-workload` on OCP UWM; `prometheus` in `konflux-monitoring-test` for Kind integration tests) — **not** a component `metrics-reader` SA

### 4. Operator controller RBAC

- [ ] Ensure kubebuilder markers allow binding `<component>-metrics-reader` and the scraper CRB
- [ ] Run `make manifests` in `operator/`

### 5. Rebuild and verify

```bash
cd operator/pkg/manifests
bash process-component.sh <component> /path/to/konflux-ci
```

- [ ] Prometheus target **up** (verified TLS when cert-manager enabled)
- [ ] No legacy `kubernetes.io/service-account-token` scrape Secret in the namespace
- [ ] `prometheus-scrape-token` present for HTTPS components using operator-managed auth
- [ ] Grafana / UWM dashboards still resolve metrics

## Component status (manual — update when migrating)

| Component | Scrape model | Remaining for unified |
|-----------|--------------|------------------------|
| build-service | Operator scrape token + HTTPS SM | cert-manager metrics cert; verified TLS on SM |
| image-controller | Operator scrape token + HTTPS SM | cert-manager metrics cert; verified TLS on SM |
| integration-service | Legacy interim (HTTP SM + static `bearerTokenSecret`) | Upstream `--metrics-secure`; then operator scrape token pattern |
| release-service | Overlay not wired | Upstream HTTP `:8080` + wire `monitoring/` |

## Related paths

| Topic | Location |
|-------|----------|
| Detailed auth comparison & Kind/OCP phases | [monitoring-authentication.md](./monitoring-authentication.md) |
| Operator self-metrics | `operator/config/prometheus/`, `internal/operatormetrics/scrape_token_rotator.go` |
| Embedded manifests | `operator/pkg/manifests/<component>/manifests.yaml` |
| Legacy infra reference | `infra-deployments/components/<component>/base/monitoring.yaml` |
| Cluster integration tests | `test/go-tests/metricsintegration/` + `test/fixtures/metrics-targets.yaml` (via `scripts/operator-e2e/run-metrics-integration-tests.sh`, hooked in `test/e2e/run-e2e.sh`) |
