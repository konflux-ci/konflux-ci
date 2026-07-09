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

Implementation: `operator/pkg/kubernetes/scrape_token.go`, wired from operand reconcilers
when `spec.componentMetrics` is enabled.

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

### ServiceMonitor apply ordering (OpenShift UWM)

On clusters where prometheus-operator evaluates ServiceMonitors at apply time, a
ServiceMonitor that references `bearerTokenSecret: prometheus-scrape-token` before the
Secret exists can be rejected (`InvalidConfiguration: secret not found`) and may not
recover when the Secret appears later.

Operand reconcilers on the operator scrape-token model address this in two layers:

1. **Deferred apply** — when `componentMetrics` is enabled, the operand ServiceMonitor
   is skipped in `applyManifests` and applied only from
   `ReconcilePrometheusScrapeToken` after the scrape token Secret is readable. The SM is
   re-applied on every reconcile (idempotent SSA) so tracking-client orphan cleanup
   retains ownership.
2. **Resync nudges** — after apply, `ResyncOperandServiceMonitor` patches SM annotations
   so prometheus-operator re-evaluates the SM when:
   - the token is minted (`token-minted`) or refreshed (`token-refreshed`)
   - a settle requeue fires (~15s, `settle-retry`)
   - the secret resource version drifts (`secret-sync`, blocked while settle is pending)

Annotations (on the operand ServiceMonitor):

| Annotation | Purpose |
|------------|---------|
| `konflux.konflux-ci.dev/metrics-scrape-resync` | RFC3339 timestamp of last nudge |
| `konflux.konflux-ci.dev/metrics-scrape-resync-reason` | `token-minted`, `token-refreshed`, `settle-retry`, or `secret-sync` |
| `konflux.konflux-ci.dev/metrics-scrape-resync-secret-rv` | Last seen `prometheus-scrape-token` resourceVersion |
| `konflux.konflux-ci.dev/metrics-scrape-resync-settle` | `pending` while waiting for settle requeue |

Implementation: `operator/internal/common/scrape_token.go`,
`operator/pkg/kubernetes/servicemonitor_resync.go`, wired from build-service and
image-controller reconcilers.

`EnsurePrometheusScrapeToken` returns `EnsureScrapeTokenResult` (token bytes,
`SecretExisted`, post-write `ResourceVersion`) from the write path. Operand
reconciliation uses that result for resync decisions instead of re-reading the Secret
or ServiceMonitor from the informer cache immediately after apply, when the cache may
not have caught up yet.

Operator logs (verbosity 1 unless noted):

- `metrics scrape deferred ServiceMonitor apply` — first SM create at Info;
  steady-state re-apply at V(1)
- `metrics scrape resync` — Info, one line per annotation patch
- `metrics scrape resync secret-sync deferred` — Info when settle blocks secret-sync

### OpenShift UWM integration tests

On OpenShift optional e2e (`konflux-e2e-v420-optional`, `konflux-e2e-v420-arm64-optional`),
`scripts/operator-e2e/openshift/enable-uwm.sh` enables user-workload monitoring, then
`run-metrics-openshift-tests.sh` runs `test/go-tests/metricsopenshift/`.

The suite verifies:

- UWM Prometheus is ready in `openshift-user-workload-monitoring`
- Operand scrape contract (ServiceMonitor spec, resync annotations, token Secret) for
  scrape-token targets (`konflux-operator`, `build-service`, `image-controller`)
- `up==1` in UWM Prometheus for scrape-token targets (`metrics-uwm`) and legacy interim
  HTTP operands with `UWMUpCheck` (`integration-service`, `konflux-ui-proxy`; label
  `metrics-uwm-up-only`, no scrape-token contract)

Before specs, tests emit `[UWM resync]` lines with `resync_reason`, secret/SM resource
versions, `uwm_active_targets`, and `sm_after_secret` (SM `creationTimestamp` after
scrape token). Use `sm_after_secret=true` and `uwm_active_targets=1` as pass fingerprints;
`resync_reason` alone is not a reliable flake indicator.

On failure, `[UWM debug]` dumps SM/secret metadata, prometheus-operator log tail, and
peer target comparison. See `test/go-tests/pkg/metricsopenshift/`.

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

## Related paths

| Topic | Location |
|-------|----------|
| Operator self-metrics | `internal/operatormetrics/` (`scrape_token_rotator.go`, `scrape_wiring.go`) |
| Embedded manifests | `operator/pkg/manifests/<component>/manifests.yaml` |
| Cluster integration tests | `test/go-tests/metricsintegration/` + `test/go-tests/pkg/metricsauth.DefaultCatalog()` (via `scripts/operator-e2e/run-metrics-integration-tests.sh`, hooked in `test/e2e/run-e2e.sh`) |
| OpenShift UWM tests | `test/go-tests/metricsopenshift/` + `test/go-tests/pkg/metricsopenshift/` (via `scripts/operator-e2e/openshift/run-metrics-openshift-tests.sh`, optional OCP e2e in `test/e2e/run-e2e.sh`) |
| ServiceMonitor resync | `operator/pkg/kubernetes/servicemonitor_resync.go` |
| Deferred SM apply | `operator/internal/common/scrape_token.go`, `operand_servicemonitor.go` |
