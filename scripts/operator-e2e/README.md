# Operator E2E helper scripts

Current shell entry points used by the Tekton operator E2E flow.

All scripts take the **konflux-ci repository root** as the first argument unless noted (absolute or relative path is fine).

## Tekton: operator lifecycle

With `OPERATOR_INSTALL_METHOD=none`, `tekton-deploy-operator-and-wait.sh` starts **`bin/manager` in the background** inside the **`deploy-konflux` Task** pod. When that Task completes, the pod is torn down and **that operator process ends**; it does not keep running on the cluster.

**`konflux-e2e-tests`** runs later, in **another pod**, and only uses **`kubectl` / `go test`** against the Kind cluster. **No Konflux operator** is running during that phase. In-cluster services the operator already installed (for example build-service) continue to run; tests should not assume the Konflux **operator** will reconcile or repair arbitrary API changes mid-suite.

For the full pipeline narrative, see **Scope** in `.tekton/pipelines/operator-e2e/README.md`.

## Metrics integration tests

`run-metrics-integration-tests.sh` runs `test/go-tests/metricsintegration` against a live cluster (port-forward + bearer token, Prometheus-style). HTTPS targets read the operator-managed `prometheus-scrape-token` Secret; HTTP targets mint a scraper SA token via TokenRequest.

Architecture and scrape models: [operator/docs/component-monitoring.md](../../operator/docs/component-monitoring.md).

| Entry point | Label filter | What runs |
|-------------|--------------|-----------|
| `test/e2e/run-e2e.sh` (GHA Kind) | `metrics` (default) | Operator + component targets |
| `tekton-run-e2e-tests.sh` | `metrics && component` | Component targets from `metricsauth.DefaultCatalog()` (operator pod not running during e2e Task) |

Tekton skips operator metrics because the deploy Task runs `bin/manager` out-of-cluster; the operator metrics Service has no endpoints during the e2e Task.

Override with `METRICS_GINKGO_LABEL_FILTER` (see `run-metrics-integration-tests.sh`).

Catalog: `test/go-tests/pkg/metricsauth.DefaultCatalog()` (`group: operator` or `component` per target; `scrapeTokenSecret` for HTTPS operands).

## OpenShift UWM metrics tests

`openshift/run-metrics-openshift-tests.sh` runs `test/go-tests/metricsopenshift` against **user-workload Prometheus** (not port-forward direct scrape). Proves ServiceMonitors are picked up by UWM (`up{namespace,service}==1`) plus HTTPS scrape contract (ServiceMonitor, `prometheus-scrape-token`, metrics-reader CRB). UWM readiness (enable flag, Prometheus pods, optional canary) runs in the suite `BeforeSuite` (`pkg/metricsopenshift/wait.go`).

| Entry point | UWM enable | Canary wait | Tests |
|-------------|------------|-------------|-------|
| `deploy-konflux-on-ocp.sh` (Prow install) | `openshift/enable-uwm.sh` after `deploy-deps.sh` | — | — |
| `test/e2e/run-e2e.sh` (Prow e2e, infra overlay) | (install / preview) | `dummy-service` when that namespace exists | `run-metrics-openshift-tests.sh` |
| Tekton / Kind | — | — | **not invoked** |

Label filter: `METRICS_OPENSHIFT_GINKGO_LABEL_FILTER` (default `openshift`). Sub-labels: `metrics-contract`, `metrics-uwm`.

Override canary: `UWM_CANARY_QUERY` or `UWM_SKIP_CANARY=true` (see `pkg/metricsopenshift/wait.go`).

On UWM target failure, the suite logs to the CI build log (no live cluster login required):

| Env | Default | Effect |
|-----|---------|--------|
| `UWM_DEBUG_LOG_INTERVAL` | `60` | Seconds between progress logs while waiting for `up==1`; `0` logs first poll and label/value changes only |
| `UWM_DEBUG_DIRECT_SCRAPE` | off | When `true`, failure snapshot also port-forwards the operand metrics Service and logs HTTP status |
| `UWM_DEBUG_OPERATOR_LOG_LINES` | `500` | Tail lines from user-workload `prometheus-operator` pod; filtered to failed namespace and ServiceMonitor messages |

Every run (pass or fail) emits `[UWM resync]` lines after UWM readiness: for `build-service` and `image-controller`, logs ServiceMonitor `resync_at` (`konflux.konflux-ci.dev/metrics-scrape-resync`), scrape-token presence, and UWM `active_targets` / strict `up` at that moment. The metrics-contract specs also require `resync_at` to be set on those operands.

Failure snapshot includes strict/broad `up` queries, a **peer comparison** row per UWM target (active + dropped target counts + `up` for operator/build-service/image-controller), active/dropped target details for the failed namespace, namespace monitoring labels, ServiceMonitor vs metrics Service selector match, ServiceMonitor/secret metadata (creation time, resourceVersion, labels) for all peers, endpoints/pods for the failed target, filtered **prometheus-operator** log tail from `openshift-user-workload-monitoring`, and optional direct scrape.

## Extra `go test` arguments (integration / conformance)

The Tekton Task sets optional env vars from pipeline params (empty by default). Scripts **omit quotes** around those expansions on purpose: the shell must **split on spaces** so each flag becomes its own argument to `go test`. (Shellcheck rule SC2086 is disabled next to those lines because unquoted expansion is usually risky; here it is required for the same reason as forwarding `"$@"`.)

| Env var | Set by pipeline param | Appended to |
|--------|------------------------|-------------|
| `E2E_INTEGRATION_GO_TEST_EXTRA_ARGS` | `integration-go-test-extra-args` | `go test . ./pkg/...` |
| `E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS` | `conformance-go-test-extra-args` | `go test ./tests/conformance ...` (after fixed Ginkgo flags) |

**Local / ad hoc** (from repo root, same semantics as the Task):

```bash
export E2E_INTEGRATION_GO_TEST_EXTRA_ARGS='-run=TestKonfluxIntegration -count=1'
export E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS='-ginkgo.skip=Flaky'
bash scripts/operator-e2e/tekton-run-e2e-tests.sh "$(pwd)"
```

Use **one token per flag** where possible (e.g. `-ginkgo.skip=Flaky`). Values with spaces inside one flag are awkward in a flat env string; prefer Ginkgo’s `-name=value` form, or run `./test/e2e/run-e2e.sh` for full `"$@"` forwarding from your shell.

**Tekton** examples: see `.tekton/pipelines/operator-e2e/README.md` (pipeline params) and `.tekton/tasks/konflux-e2e-tests/README.md` (Task params).

## Scripts

| Script | Purpose |
|--------|---------|
| `quiet-cluster-for-conformance.sh` | After proxy/metrics: patch Konflux CR to scale UI proxy/Dex/namespace-lister to 0 and disable Tekton Results (+ Postgres). Opt out: `E2E_QUIET_CLUSTER=false`. |
| `prepare-conformance-env.sh` | Exports `CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE` from `operator/pkg/manifests/build-service/manifests.yaml` (same pin as GitHub Actions E2E). |
| `run-conformance-tests.sh` | Runs conformance tests. Requires `GH_ORG`, `GH_TOKEN` (conformance clears `QUAY_TOKEN` for the test run). |
| `tekton-fetch-kubeconfig.sh` | **Tekton:** decodes kubeconfig from Secret into `/mnt/e2e-shared/kubeconfig`. Args: `SECRET_NAME` `[KEY]`. Env: `POD_NAMESPACE`. |
| `tekton-copy-shared-tools.sh` | **Tekton:** copies `kubectl`, `yq`, `jq` into `/mnt/e2e-shared/bin` and **`jq`’s shared libraries** (`libjq`, `libonig`) into `/mnt/e2e-shared/lib` for `ubi10/go-toolset` steps. |
| `tekton-resolve-konflux-cr.sh` | **Tekton:** resolves optional ConfigMap env (`E2E_KONFLUX_CR_YAML`) or `E2E_KONFLUX_CR_PATH` to an absolute CR file path (stdout). |
| `tekton-deploy-prep.sh` | **Tekton (go-toolset):** optional overrides via `operator/cmd/overrides`, then `deploy-local.sh` with `OPERATOR_INSTALL_METHOD=none`. |
| `tekton-push-operator-pkg-manifests-oci.sh` | **Tekton (task-runner):** after prep, push `operator/pkg/manifests` to OCI as `${oci-container-repo}:${pipelineRun}.pkg-manifests` (skips if `E2E_OCI_CONTAINER_REPO` blank). |
| `tekton-deploy-operator-and-wait.sh` | **Tekton (go-toolset):** `make install`/build, `bin/manager`, apply CR, wait Ready. |
| `tekton-run-e2e-tests.sh` | **Tekton:** `deploy-test-resources.sh` (`SKIP_SAMPLE_COMPONENTS=true`), optional `kubectl port-forward` + `KONFLUX_PROXY_URL` for remote Kind, integration + conformance `go test`. Proxy tests wait for Konflux Ready and read `status.uiURL` in Go (`BeforeSuite` in `test/go-tests/proxy_setup.go`). Optional env: `KONFLUX_PROXY_URL`, `E2E_INTEGRATION_GO_TEST_EXTRA_ARGS`, `E2E_CONFORMANCE_GO_TEST_EXTRA_ARGS`. |
| `run-proxy-integration-tests.sh` | Select proxy auth mode and run `go test` in `test/go-tests`. OpenShift OAuth runs in test `BeforeSuite`. Called from `test/e2e/run-e2e.sh`. |
| `run-metrics-integration-tests.sh` | Runs `test/go-tests/metricsintegration` (port-forward + bearer token scrape). |
| `openshift/enable-uwm.sh` | **OpenShift CI only:** idempotent `enableUserWorkload: true` via `cluster-monitoring-config`. |
| `openshift/run-metrics-openshift-tests.sh` | **OpenShift CI only:** `go test ./metricsopenshift` (UWM readiness in BeforeSuite, ServiceMonitor contract + UWM `up`). |

## Proxy integration tests

`test/e2e/run-e2e.sh` runs `run-proxy-integration-tests.sh` before conformance. Demo-user fixtures (`deploy-test-resources.sh`) run only when `E2E_DEPLOY_TEST_RESOURCES=true` (set in GHA; Tekton calls `deploy-test-resources.sh` directly). OpenShift OAuth (kubeadmin → Dex `id_token`) is implemented in Go (`test/go-tests/proxy_oauth_openshift.go`) and runs in proxy test `BeforeSuite` when `KONFLUX_PROXY_AUTH=openshift`. Useful env vars:

| Variable | Purpose |
|----------|---------|
| `E2E_DEPLOY_TEST_RESOURCES` | When `true`, run `deploy-test-resources.sh` before tests (Kind Dex `proxy-dex` RBAC; off by default in `run-e2e.sh`) |
| `KONFLUX_PROXY_AUTH` | `openshift` or `dex` (default: infer — openshift when `OPENSHIFT_PASSWORD`, `KUBEADMIN_PASSWORD_FILE`, or `SHARED_DIR/kubeadmin-password` is set and `TEST_ENVIRONMENT!=upstream`, else dex) |
| `KONFLUX_PROXY_AUTH_METHOD` | Set by the runner: `openshift-oauth` or `dex-password-grant` |
| `OPENSHIFT_PASSWORD` / `KUBEADMIN_PASSWORD_FILE` / `SHARED_DIR/kubeadmin-password` | Kubeadmin password for OpenShift OAuth (file or env; see runner script) |
| `KONFLUX_PROXY_ID_TOKEN` | Optional override: skip OAuth and use a pre-obtained Dex `id_token` |
| `KONFLUX_PROXY_ID_TOKEN_USER1` / `KONFLUX_PROXY_ID_TOKEN_USER2` | Optional per-user tokens for Dex-only specs |
| `KONFLUX_PROXY_URL` | Proxy base URL (default: `konflux/konflux` `status.uiURL`) |
| `KONFLUX_PROXY_WAIT_UI_ONLY` | When `true`, `BeforeSuite` waits for `ui.Ready` instead of full Konflux Ready |
| `KONFLUX_PROXY_TEST_NAMESPACE` | Tenant namespace for proxied API paths (default: `default-tenant`) |

On OpenShift CI, Dex-only specs are skipped via `-ginkgo.label-filter='!proxy-dex'`.

## Override YAML schema

Each list item supports:

- `name` (component under `operator/upstream-kustomizations/`)
- `git` (array of rules; may be empty if only image overrides)
- `images` (array of `{ orig, replacement }`; may be empty if only git overrides)

At least one of `git` or `images` must be non-empty per item.

Each `git` rule:

- `sourceRepo`: `org/repo` or `https://github.com/org/repo`
- plus either:
  - `remote: { repo, ref }`
  - or `localPath`

`remote.ref` can be branch, tag, or SHA. First matching `sourceRepo` per resource URL wins.
