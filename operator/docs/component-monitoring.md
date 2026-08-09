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
verified server TLS (**cert-manager required** for the migrated HTTPS components), and **`bearerTokenSecret` →
operator-managed `prometheus-scrape-token`** (TokenRequest for the operand `metrics-scraper`
ServiceAccount, rotated by the operator).

| Piece | Target |
|-------|--------|
| Metrics server | HTTPS `:8443`, controller-runtime auth filters (no kube-rbac-proxy) |
| Server TLS | Single Secret `metrics-server-cert` (`tls.crt`/`tls.key` + scrape trust `ca.crt`). Operands: Certificate via `ClusterIssuer/konflux-issuer`. Operator manager: namespace-local SelfSigned Issuer (`config/certmanager/`) so the Secret exists before the manager starts. **cert-manager is required** for verified scrape installs. |
| ServiceMonitor | `scheme: https`, `port: https`, `bearerTokenSecret` → `prometheus-scrape-token` |
| TLS verify | `tlsConfig.ca` from `metrics-server-cert` / `ca.crt` + `serverName` — **not** `insecureSkipVerify: true` |
| Authorization | `<component>-metrics-reader` ClusterRole bound to the operator-owned `metrics-scraper` ServiceAccount in the operand namespace |
| Scrape credentials | Short-lived bound tokens in `prometheus-scrape-token`; **not** `bearerTokenFile` or legacy `kubernetes.io/service-account-token` Secrets |

**Why not `bearerTokenFile`:** OpenShift user-workload Prometheus rejects ServiceMonitors
that set `bearerTokenFile` (`ArbitraryFSAccessThroughSMs`). Konflux uses
`bearerTokenSecret` everywhere so Kind, vanilla Kubernetes, and OCP share one architecture.

Operator self-metrics reference: `internal/operatormetrics/scrape_wiring.go` and
`ScrapeTokenRotator` in `cmd/main.go` (ServiceMonitor created at runtime when the
ServiceMonitor CRD is installed). Metrics TLS certificates are issued via
`operator/config/certmanager/` and mounted with `cert_metrics_manager_patch.yaml`.

## Shipped today (operator scrape token)

Implements the [unified target](#target-architecture-unified). Applies to metrics-enabled
components on the **operator scrape token** model (see [Scope](#scope)).

| Piece | Shipped |
|-------|---------|
| Metrics server | HTTPS `:8443` with auth filters. **konflux-operator**, **build-service**, **image-controller**, **release-service**, and **integration-service** use a single `metrics-server-cert` Secret (leaf + `ca.crt`) with verified scrape TLS (`tlsConfig.ca` → `metrics-server-cert`/`ca.crt`, plus `serverName`). Pods mount `tls.crt`/`tls.key` only. Operands are issued by `konflux-issuer`; the operator uses a namespace-local SelfSigned Issuer at install time. Operand controllers mount at controller-runtime’s default CertDir (`/tmp/k8s-metrics-server/serving-certs`) with no `--metrics-cert-path`. |
| ServiceMonitor | `bearerTokenSecret` → `prometheus-scrape-token` in the operand namespace |
| Scrape Secret | **Not** in kustomize — reconciler mints a bound token via TokenRequest for the operand `metrics-scraper` SA and writes `prometheus-scrape-token`; refreshes before expiry |
| Authorization | `<component>-metrics-reader` ClusterRole; CRB subjects bind the operator-owned `metrics-scraper` ServiceAccount in the operand namespace |
| Legacy SA/Secret | Removed from `monitoring/` overlays for these components |

Implementation: `operator/internal/common/scrape_token.go` (high-level
`ReconcilePrometheusScrapeToken`) and `operator/pkg/kubernetes/scrape_token.go`
(lower-level token helpers), wired from operand reconcilers when
`spec.componentMetrics` is enabled.

Operand controllers use complementary mechanisms:

- **Secret watch (scrape token)** — `Owns` on `prometheus-scrape-token` (name-filtered) reconciles
  immediately when the owned Secret is deleted or replaced.
- **Secret watch (metrics TLS)** — `Watches` on `metrics-server-cert` by name
  (cert-manager creates this Secret without CR ownerRefs) so metrics TLS changes are detected without
  waiting for the rotation ticker.
- **ServiceMonitor watch** — CRD-gated `Owns` on the operand ServiceMonitor (via
  `common.OperandServiceMonitorWatchObjectIfInstalled`) so out-of-band delete/mutate
  triggers immediate reconcile. When the ServiceMonitor CRD is absent, controllers
  fall back to the rotation broadcaster.
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

Operand reconcilers on the operator scrape-token model address this by
**deferred apply**: when `componentMetrics` is enabled, the operand ServiceMonitor
is skipped in `applyManifests` and applied only from
`ReconcilePrometheusScrapeToken` after the scrape token Secret is readable **and**
`metrics-server-cert` has verifying `tls.crt` + `ca.crt`. The SM is
re-applied on every reconcile (idempotent SSA) so tracking-client orphan cleanup
retains ownership.

**Conditional retain during TLS wait:** while waiting for TLS readiness, an
already-existing ServiceMonitor is normally re-applied (retained) so
tracking-client orphan cleanup does not delete it. However, when
`metrics-server-cert` is absent (`MetricsTLSReasonCertMissing`), retain is
**skipped** — the SM's `tlsConfig.ca` references the missing Secret, so
prometheus-operator can reject it (`InvalidConfiguration`). Skipping retain
lets orphan cleanup remove the stale SM; deferred apply recreates it once
the Secret verifies again. For other not-ready reasons (`metrics-ca-empty`,
`metrics-server-cert-empty`, `leaf-ca-mismatch`) the Secret object exists and
CA references resolve, so retain runs normally.

Annotation-only "resync" patches on the ServiceMonitor nudge OpenShift UWM
prometheus-operator to re-evaluate scrape config after the scrape token is
reminted or metrics TLS CA material changes. Deferred apply still prevents
SM-before-Secret `InvalidConfiguration` rejection; identical SM SSA alone does
**not** enqueue UWM when only Secret data changes, so
`ResyncOperandServiceMonitor` merge-patches `metrics-scrape-resync*` annotations
(`token-minted` / `token-refreshed` / `secret-sync` / `ca-sync` / `settle-retry`).
After token mint/refresh, `metrics-scrape-resync-settle` holds an RFC3339 deadline
(`now` + 15s); early Secret-watch reconciles requeue the remaining delay and only
issue `settle-retry` once that deadline elapses.

Implementation: `operator/internal/common/scrape_token.go`,
`operator/pkg/kubernetes/servicemonitor_resync.go`,
`operator/pkg/kubernetes/metrics_tls.go`, wired from build-service,
image-controller, release-service, and integration-service reconcilers and the
operator `ScrapeTokenRotator`.

`EnsurePrometheusScrapeToken` returns `EnsureScrapeTokenResult` (token bytes,
`SecretExisted`, post-write `ResourceVersion`) from the write path.
`EvaluateMetricsScrapeTLS` / `ReconcileMetricsScrapeTLS` return metrics TLS
`ResourceVersion` from the verification Get. Metrics TLS Secret Gets prefer
`SecretReader` (`mgr.GetAPIReader()`) so cert-manager updates are not missed due to
cache lag.

Operator logs:

- `Deferring operand ServiceMonitor apply` — V(2); logged each reconcile
  when deferred apply is active. Silent at default verbosity.
- `metrics scrape deferred ServiceMonitor apply` — first SM create at Info;
  steady-state re-apply (SM already exists) at V(2). Silent at default
  verbosity once the SM is created.
- `metrics scrape deferred ServiceMonitor waiting for TLS chain` — V(1)
  while metrics TLS are missing or not yet verifying (`tls.crt`/`ca.crt`
  empty or mismatched). Visible at `-v=1` (debug). Leaf/CA rotation is
  owned by cert-manager; the operator only waits and re-applies the SM
  when the Secret verifies again.
- `retaining existing ServiceMonitor while waiting for metrics TLS chain`
  — V(2); logged when an SM is re-applied during TLS wait to prevent
  orphan cleanup from deleting it. Silent at default verbosity.
- `skipping ServiceMonitor retain while metrics-server-cert is absent` —
  Info; logged when metrics-server-cert is missing and retain is skipped
  so orphan cleanup removes the stale SM.
- `metrics scrape resync` — Info when annotation-only SM nudge runs
  (`token-minted`, `token-refreshed`, `secret-sync`, `ca-sync`,
  `settle-retry`). Emitted on remint / Secret or CA RV change, not on
  steady-state reconciles.

Steady-state reconciles at default verbosity produce no metrics scrape log
lines when the ServiceMonitor exists and TLS is ready (no remint/resync).
Use `-v=1` to see TLS-wait messages, and `-v=2` to see per-reconcile
deferral and re-apply details.

### OpenShift UWM integration tests

On OpenShift optional e2e (`konflux-e2e-v420-optional`, `konflux-e2e-v420-arm64-optional`),
`scripts/operator-e2e/openshift/enable-uwm.sh` enables user-workload monitoring, then
`run-metrics-openshift-tests.sh` runs `test/go-tests/metricsopenshift/`.

The suite verifies:

- UWM Prometheus is ready in `openshift-user-workload-monitoring`
- Operand scrape contract (ServiceMonitor spec, token Secret, presence of
  `metrics-scrape-resync` annotations after token mint) for scrape-token targets
  (`konflux-operator`, `build-service`, `image-controller`, `release-service`,
  `integration-service`)
- `up==1` in UWM Prometheus for scrape-token targets (`metrics-uwm`) and legacy interim
  HTTP operands with `UWMUpCheck` (`konflux-ui-proxy`; label
  `metrics-uwm-up-only`, no scrape-token contract)

Before specs, tests emit `[UWM scrape]` evidence lines with secret/SM resource
versions, `uwm_active_targets`, and `sm_after_secret` (SM `creationTimestamp` after
scrape token). Use `sm_after_secret=true` and `uwm_active_targets=1` / `uwm_up=1` as
pass fingerprints.

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
| Rotation | `ScrapeTokenRotator` adaptive timer (`DefaultScrapeTokenRotationInterval`, same as operand broadcaster) plus early wake on scrape-wiring Secret events (`metrics-server-cert`, scrape token); freshness check skips mint when token is still valid |
| Server TLS (cert-manager) | **Required** for verified scrape — `config/certmanager/` is included from default `operator-rbac` kustomization; metrics TLS Secrets gate ServiceMonitor apply |

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

- [ ] Add or extend `certmanager/` with a Certificate that issues `metrics-server-cert` via `konflux-issuer` (operands) or a namespace-local SelfSigned Issuer (operator manager only)
- [ ] Patch Deployment: mount leaf cert volume (`tls.crt`/`tls.key` from `metrics-server-cert`), `--metrics-cert-path=…` (or default CertDir)
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
- [ ] Replace `insecureSkipVerify: true` with `tlsConfig.ca` from `metrics-server-cert` / `ca.crt` and correct `serverName`

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

**Controller watches:**

8. In `SetupWithManager`, register a CRD-gated ServiceMonitor `Owns` watch
   with `common.OperandServiceMonitorWatchObjectIfInstalled(mgr.GetRESTMapper())`
   so out-of-band ServiceMonitor delete/mutate triggers immediate reconcile.
   Follow the pattern in build-service, image-controller, release-service,
   integration-service, and UI. When the CRD is absent, skip the watch and
   rely on the rotation broadcaster.

**Orphan cleanup:**

9. Extend orphan cleanup GVKs with
   `kubernetes.ComponentMetricsOrphanCleanupGVKs` and add
   ClusterRole/ClusterRoleBinding names to the cluster-scoped resource
   allowlist in the component reconciler

**Tests:**

10. Add unit tests for both gating paths: `ComponentMetrics: nil` (enabled by
    default) and `ComponentMetrics: &ComponentMetricsConfig{Enabled:
    ptr.To(false)}` (disabled, scrape resources skipped/deleted). Follow the
    test style established in the target package; if the package uses
    Ginkgo/Gomega, apply [ginkgo-testing](../../skills/ginkgo-testing/SKILL.md)
    conventions.
11. Register the new scrape target in the metrics integration test catalog
    (`test/go-tests/pkg/metricsauth/default_catalog.go`)

**Documentation:**

12. Update this document to list the new component under the appropriate
    scrape model in the [Scope](#scope) tables

## Related paths

| Topic | Location |
|-------|----------|
| Operator self-metrics | `internal/operatormetrics/` (`scrape_token_rotator.go`, `scrape_wiring.go`, `health.go`) |
| Embedded manifests | `operator/pkg/manifests/<component>/manifests.yaml` |
| Cluster integration tests | `test/go-tests/metricsintegration/` + `test/go-tests/pkg/metricsauth.DefaultCatalog()` (via `scripts/operator-e2e/run-metrics-integration-tests.sh`, hooked in `test/e2e/run-e2e.sh`) |
| OpenShift UWM tests | `test/go-tests/metricsopenshift/` + `test/go-tests/pkg/metricsopenshift/` (via `scripts/operator-e2e/openshift/run-metrics-openshift-tests.sh`, optional OCP e2e in `test/e2e/run-e2e.sh`) |
| Deferred SM apply + scrape token | `operator/internal/common/scrape_token.go`, `operand_servicemonitor.go` |
| Metrics TLS readiness | `operator/pkg/kubernetes/metrics_tls.go` |
| SM annotation resync | `operator/pkg/kubernetes/servicemonitor_resync.go` (UWM nudge on token/CA change) |

## `konflux_up` ecosystem labels

The `konflux_up` metric is a standardized binary gauge (0 = down, 1 = up) used
across all Konflux services to report availability. It enables unified alerting
and dashboards across the fleet.

**Required labels** for any `konflux_up` metric:

| Label | Description | Example |
|-------|-------------|---------|
| `service` | Component name (must be unique across all `konflux_up` emitters) | `konflux-operator`|
| `check` | What aspect of availability is being verified | `konflux-ready`|

**Implementation in the operator:**

The operator emits `konflux_up` with `ConstLabels` in `internal/operatormetrics/health.go`.
Because `ConstLabels` produce a metric-side `service` label that collides with the
Prometheus target label of the same name, the operator's ServiceMonitor sets
`honorLabels: true` (in `scrape_wiring.go`) to preserve the metric's labels.

**Adding new `konflux_up` signals:**

When adding a new availability metric to the operator or any component:

1. Use the metric name `konflux_up` with unique `service` + `check` label values
2. If using `ConstLabels` that collide with target labels, set `honorLabels: true`
   on the ServiceMonitor endpoint
