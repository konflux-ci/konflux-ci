# Component monitoring

How Konflux operator deploys Prometheus metrics scraping for controller components.

## Scope

**Metrics-enabled components** — Konflux operands for which the operator deploys and
reconciles Prometheus scrape resources when `spec.componentMetrics` is enabled. The
current set is defined by operand reconcilers and `operator/upstream-kustomizations/*/monitoring/`,
not by a fixed list in this document.

When `componentMetrics.enabled` is false, those reconcilers skip `monitoring/`
resources and delete previously applied scrape objects. **Operator self-metrics**
(Konflux operator Deployment ServiceMonitor) are not controlled by this field.

Scrape models in use today (see overlays and reconcilers to see which operands use each):

| Model | How to recognize | Summary |
|-------|------------------|---------|
| Operator scrape token | Operand reconciler rotates `prometheus-scrape-token`; scraper CRB has `konflux.konflux-ci.dev/metrics-scraper-binding` | HTTPS + `prometheus-scrape-token` (TokenRequest, rotated) |
| Legacy interim | Static `*-metrics-reader` ServiceAccount and token Secret in `monitoring/` overlay | HTTP `:8080` + static `bearerTokenSecret` (`*-metrics-reader` Secret) |
| Pending | `monitoring/` overlay exists but is not in the operand `kustomization.yaml` and/or the reconciler does not honor `componentMetrics` | Overlay not deployed or controlled by the cluster knob yet |

## Cluster knob: `spec.componentMetrics`

The Konflux CR exposes metrics controls for all metrics-enabled components:

```yaml
spec:
  componentMetrics:
    enabled: true
```

- **`enabled`:** treated as **true** when unset. The Konflux reconciler forwards this
  value to metrics-enabled operand CRs (`KonfluxBuildService`, `KonfluxImageController`,
  `KonfluxIntegrationService`, `KonfluxUI`, etc.).
- **Scraper identity:** not configurable on the CR. For HTTPS operands on the operator
  scrape-token model, the operator creates a `metrics-scraper` ServiceAccount in each
  operand namespace, binds it in the metrics-reader ClusterRoleBinding, and mints
  `prometheus-scrape-token` via TokenRequest.

On Kind, `deploy-deps.sh` installs the ServiceMonitor CRD by default. Set
`componentMetrics.enabled: false` on the Konflux CR if you skip CRD install.

## Third-party CRD pin (Kind + envtest)

The ServiceMonitor CRD is vendored like cert-manager envtest CRDs:

| Role | Path |
|------|------|
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

Same model on OpenShift UWM, Kind, and other Kubernetes clusters: HTTPS metrics
with controller-runtime `WithAuthenticationAndAuthorization` (no kube-rbac-proxy),
verified server TLS when cert-manager is enabled, and **`bearerTokenSecret` →
operator-managed `prometheus-scrape-token`** (TokenRequest for the operand `metrics-scraper`
ServiceAccount, rotated by the operator).

| Piece | Target |
|-------|--------|
| Metrics server | HTTPS `:8443`, controller-runtime auth filters (no kube-rbac-proxy) |
| Server TLS | cert-manager `Certificate` → `metrics-server-cert` (or OpenShift serving CA) |
| ServiceMonitor | `scheme: https`, `port: https`, `bearerTokenSecret` → `prometheus-scrape-token` |
| TLS verify | `tlsConfig.ca` + `serverName` — **not** `insecureSkipVerify: true` |
| Authorization | `<component>-metrics-reader` ClusterRole bound to the operator-owned `metrics-scraper` ServiceAccount in the operand namespace |
| Scrape credentials | Short-lived bound tokens in `prometheus-scrape-token`; **not** `bearerTokenFile` or legacy `kubernetes.io/service-account-token` Secrets |

**Why not `bearerTokenFile`:** OpenShift user-workload Prometheus rejects ServiceMonitors
that set `bearerTokenFile` (`ArbitraryFSAccessThroughSMs`). Konflux uses
`bearerTokenSecret` everywhere so Kind, vanilla Kubernetes, and OCP share one architecture.

Operator self-metrics reference: `internal/operatormetrics/scrape_wiring.go` and
`ScrapeTokenRotator` in `cmd/main.go` (ServiceMonitor created at runtime when the
ServiceMonitor CRD is installed; cert-manager TLS patches remain under
`operator/config/prometheus/` for future use).

## Shipped today (operator scrape token)

Implements the [unified target](#target-architecture-unified). Applies to metrics-enabled
components on the **operator scrape token** model (see [Scope](#scope)).

| Piece | Shipped |
|-------|---------|
| Metrics server | HTTPS `:8443` with auth filters (`insecureSkipVerify: true` on ServiceMonitor for now) |
| ServiceMonitor | `bearerTokenSecret` → `prometheus-scrape-token` in the operand namespace |
| Scrape Secret | **Not** in kustomize — reconciler mints a bound token via TokenRequest for the operand `metrics-scraper` SA and writes `prometheus-scrape-token`; refreshes before expiry |
| Authorization | `<component>-metrics-reader` ClusterRole; CRB subjects bind the operator-owned `metrics-scraper` ServiceAccount in the operand namespace |
| Legacy SA/Secret | Removed from `monitoring/` overlays for these components |

Implementation: `operator/internal/common/scrape_token.go` (high-level
`ReconcilePrometheusScrapeToken`) and `operator/pkg/kubernetes/scrape_token.go`
(lower-level token helpers), wired from operand reconcilers when
`spec.componentMetrics` is enabled.

Operand controllers use two complementary mechanisms:

- **Secret watch** — `Owns` on `prometheus-scrape-token` (name-filtered) reconciles
  immediately when the owned Secret is deleted or replaced.
- **Rotation broadcaster** — a leader-elected ticker (default every 15 minutes) nudges
  subscribed controllers to reconcile so tokens refresh before expiry and rotation still
  runs if a reconcile was skipped.

Timing constants and trade-offs: `DefaultScrapeTokenTTL`,
`DefaultScrapeTokenRotationInterval`, `DefaultScrapeTokenMinRequeue`, and
`DefaultScrapeTokenRefreshRemaining` in `scrape_token.go`.

Example manifests: `operator/upstream-kustomizations/<component>/monitoring/`.

### Operator self-metrics

The Konflux operator manager uses the same **operator scrape token** model in
`konflux-operator`. Operator self-metrics are **not** gated by
`spec.componentMetrics` on the Konflux CR; `main.go` registers `ScrapeTokenRotator`
only when `--metrics-bind-address` is not `0` (metrics server enabled).

| Piece | Shipped |
|-------|---------|
| Metrics server | HTTPS `:8443` with auth filters (`cmd/main.go`) |
| ServiceMonitor | `ScrapeTokenRotator` ensures `controller-manager-metrics-monitor` in `konflux-operator` — `bearerTokenSecret` → `prometheus-scrape-token` |
| Scrape Secret | `ScrapeTokenRotator` in `cmd/main.go` mints and rotates `prometheus-scrape-token` |
| Scraper CRB | Operand reconciler binds `metrics-scraper` in the operand namespace; operator rotator does the same in `konflux-operator` |
| Rotation | `ScrapeTokenRotator` fixed-interval ticker (`DefaultScrapeTokenRotationInterval`, same as operand broadcaster); freshness check skips mint when token is still valid |
| Server TLS (cert-manager) | Optional — `config/certmanager/` exists; not enabled in default `operator-rbac` kustomization yet |

Cluster integration tests scrape via the operator-managed `prometheus-scrape-token` Secret.

## Legacy interim

Applies to metrics-enabled components still on the **legacy interim** model (see
[Scope](#scope)), and to operands whose `monitoring/` overlay is wired later.

| Piece | Legacy interim |
|-------|----------------|
| Metrics server | HTTP `:8080` (no auth on metrics yet) |
| ServiceMonitor | `scheme: http` and/or `bearerTokenSecret` → static `*-metrics-reader` Secret |
| Authorization | `<component>-metrics-reader` ClusterRole bound to a dedicated **`metrics-reader` ServiceAccount** in the component namespace |
| Scrape credentials | Legacy SA token Secret (`type: kubernetes.io/service-account-token`) |

Example: a legacy interim ServiceMonitor uses `scheme: http`, `port: http`, and
`bearerTokenSecret` → `<component>-metrics-reader` (static legacy Secret).

**Components on legacy interim today:**

- **konflux-ui-proxy** — Caddy reverse-proxy, HTTP `:2112` on port `metrics`,
  `konflux-ui-proxy-metrics-reader` ClusterRole, gated by `KonfluxUISpec.componentMetrics`
  in the UI reconciler. No bearer token (plain HTTP scrape).

**Why legacy interim remains:** Those controllers do not expose HTTPS authenticated metrics
yet; Prometheus can scrape without waiting for upstream `--metrics-secure` and cert-manager.

## Migrate a component: legacy interim → unified

Components on the **operator scrape token** model have already dropped static-token interim;
remaining work is mostly cert-manager server TLS and verified scrape TLS (see unified target).

Components on the **legacy interim** model (and operands whose `monitoring/` overlay
is not yet wired to `componentMetrics`) should migrate in order below. Skip steps
that already apply to operator scrape token components.

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
- [ ] `prometheus-*-metrics-reader` ClusterRoleBinding with empty `subjects` and
  `konflux.konflux-ci.dev/metrics-scraper-binding: "true"` (subjects reconciled at runtime)

**ServiceMonitor (HTTPS components):**

- [ ] `scheme: https`, `port: https`
- [ ] `bearerTokenSecret` → `prometheus-scrape-token` (operand namespace) on all clusters
- [ ] Replace `insecureSkipVerify: true` with `tlsConfig.ca` from `metrics-server-cert` and correct `serverName`

**Operator reconciler (HTTPS components on OCP / Kind):**

- [ ] Wire `EnsurePrometheusScrapeToken` (mint TokenRequest for `metrics-scraper`, rotate Secret)
- [ ] Do **not** embed `prometheus-scrape-token` in kustomize — reconciler creates it

**ClusterRoleBinding:**

- [ ] Subjects: operator-owned `metrics-scraper` ServiceAccount in the operand namespace

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

## Determining scrape model per operand

Use the repo and reconcilers rather than a maintained component list:

| Check | Operator scrape token | Legacy interim | Pending |
|-------|----------------------|----------------|---------|
| `monitoring/` in operand `kustomization.yaml` | Included | Included | Often present on disk, not included |
| ServiceMonitor | `scheme: https`, `bearerTokenSecret` → `prometheus-scrape-token` | `scheme: http` and/or static `*-metrics-reader` Secret | N/A until wired |
| Operand reconciler | `TokenCreator`, `ReconcilePrometheusScrapeToken`, CRB `metrics-scraper-binding` annotation | Skips token rotation; static SA/Secret in overlay | No `componentMetrics` gating / token wiring |

Paths: `operator/upstream-kustomizations/<component>/`, matching controller under
`operator/internal/controller/`, embedded output in `operator/pkg/manifests/<component>/`.

## Controller wiring checklist

When adding Prometheus metrics scraping for a new operator component, complete every
item below. Steps follow the established pattern across build-service,
image-controller, integration-service, and UI. Cross-reference the scrape model
tables and migration guide above for architectural context. For overlay and manifest
steps (creating `monitoring/` kustomization, rebuilding embedded manifests via
`process-component.sh`), see
[Migrate a component](#migrate-a-component-legacy-interim--unified).

**API and code generation:**

1. Add `ComponentMetrics *ComponentMetricsConfig` field (with JSON tag
   `componentMetrics,omitempty`) to the component's spec type in
   `operator/api/v1alpha1/`
2. Where a `NewKonflux<Component>Spec` constructor exists in
   `operator/api/v1alpha1/operand_specs.go`, extend it to accept
   `*ComponentMetricsConfig` and wire it into the spec. Not all components
   have constructors — for example, the UI reconciler sets `ComponentMetrics`
   inline in the Konflux reconciler instead.
3. Run `make manifests generate` from `operator/` to regenerate deepcopy and
   CRD schemas

**Konflux reconciler (top-down config flow):**

4. In the Konflux reconciler
   (`operator/internal/controller/konflux/konflux_controller.go`), forward
   `componentMetrics` from the parent Konflux CR to the sub-CR spec using
   `common.ForwardedComponentMetrics(owner)`

**Component reconciler (gating logic):**

5. In the component reconciler, add conditional skip logic for monitoring
   resources using `kubernetes.IsComponentMetricsScrapeResource` — when
   `spec.ComponentMetrics.IsEnabled()` is false, skip apply and delete
   existing scrape objects. For HTTPS operands on the operator scrape-token
   model, also wire `TokenCreator` for scrape-token minting and
   `ReconcilePrometheusScrapeToken` for token rotation (see
   [Shipped today](#shipped-today-operator-scrape-token) and
   `operator/internal/common/scrape_token.go`)

**RBAC:**

6. Add the new `<component>-metrics-reader` ClusterRole to the
   `bind;escalate` kubebuilder RBAC annotation on the component controller.
   For HTTPS operands on the scrape-token model, also ensure
   `prometheus-<component>-metrics-reader` ClusterRoleBinding has `bind`
   verb so the operator can bind the `metrics-scraper` ServiceAccount
7. Add a ServiceMonitor RBAC marker with all required verbs:
   `get;list;watch;create;patch` — omitting `create` will prevent the
   controller from creating ServiceMonitors

**Orphan cleanup:**

8. Extend orphan cleanup GVKs with
   `kubernetes.ComponentMetricsOrphanCleanupGVKs` and add
   ClusterRole/ClusterRoleBinding names to the cluster-scoped resource
   allowlist in the component reconciler

**Tests:**

9. Add unit tests for both gating paths: `ComponentMetrics: nil` (enabled by
   default) and `ComponentMetrics: &ComponentMetricsConfig{Enabled:
   ptr.To(false)}` (disabled, scrape resources skipped/deleted). Follow
   [ginkgo-testing](../../skills/ginkgo-testing/SKILL.md) conventions.
10. Register the new scrape target in the metrics integration test catalog
    (`test/go-tests/pkg/metricsauth/default_catalog.go`)

**Documentation:**

11. Update this document to list the new component under the appropriate
    scrape model in the [Scope](#scope) tables

## Related paths

| Topic | Location |
|-------|----------|
| Operator self-metrics | `internal/operatormetrics/` (`scrape_token_rotator.go`, `scrape_wiring.go`) |
| Embedded manifests | `operator/pkg/manifests/<component>/manifests.yaml` |
| Cluster integration tests | `test/go-tests/metricsintegration/` + `test/go-tests/pkg/metricsauth.DefaultCatalog()` (via `scripts/operator-e2e/run-metrics-integration-tests.sh`, hooked in `test/e2e/run-e2e.sh`) |
