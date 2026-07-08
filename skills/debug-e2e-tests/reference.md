# Reference: E2E Debug Skill (GitHub Actions)

For OpenShift CI (Prow) artifact structure, see [prow-reference.md](prow-reference.md).

## Log Directory Structure

After downloading artifacts from a failed GitHub Actions run, the `logs/` directory contains:

```
logs/
├── junit/
│   └── junit-conformance-{amd64|arm64}.xml   # Ginkgo JUnit report
├── artifacts/
│   ├── namespaces.json
│   ├── nodes.json
│   ├── events.json                            # All cluster events (Warning = key signal)
│   ├── configmaps.json
│   ├── deployments.json
│   ├── services.json
│   ├── applications.json                      # AppStudio Application CRs
│   ├── components.json                        # AppStudio Component CRs
│   ├── snapshots.json
│   ├── integrationtestscenarios.json
│   ├── releaseplans.json
│   ├── releaseplanadmissions.json
│   ├── releases.json
│   ├── enterprisecontractpolicies.json
│   ├── pipelineruns.json                      # Tekton PipelineRuns (key for build/release failures)
│   ├── taskruns.json                          # Tekton TaskRuns
│   ├── pipelines.json
│   ├── repositories.json                      # PipelinesAsCode Repository CRs
│   └── pods/
│       ├── build-service_<pod>.log
│       ├── integration-service_<pod>.log
│       ├── release-service_<pod>.log
│       ├── application-service_<pod>.log
│       ├── pipelines-as-code_<pod>.log
│       ├── kube-system_etcd-<node>.log           # etcd logs (--tail=500)
│       └── kube-system_kube-apiserver-<node>.log  # kube-apiserver logs (--tail=500)
├── system-resources.log          # Host: memory, CPU, disk, load, top processes
├── cluster-resources.log         # Nodes describe, pending pods, all pod status
├── container-resources.log       # Docker/containerd stats
├── operator-logs.log             # Konflux operator deployment + pod logs + events
├── konflux-crs-status.log        # Konflux CR and all sub-CRs (KonfluxBuildService, etc.)
├── kyverno-policy-pods.log       # Pods matching Kyverno policy labels
├── kyverno-policy-pod-definitions.yaml
├── failed-pods-definitions.yaml  # Full YAML of pods with Warning events
├── failed-pods-logs.log          # Container logs from warning-event pods
├── failed-deployment-event-log.log
├── pipelinerun-res.log           # All PipelineRuns YAML
└── taskrun-res.log               # All TaskRuns YAML
```

## Upstream Dependency Details

### Pinned versions in kustomizations

Each upstream service is pinned to a specific commit SHA in its kustomization file. The `ref=` query parameter on the GitHub URL and the `newTag:` in the `images:` block use the same SHA.

Example from `operator/upstream-kustomizations/integration/core/kustomization.yaml`:
```yaml
resources:
- https://github.com/konflux-ci/integration-service/config/default?ref=<SHA>
images:
- name: quay.io/konflux-ci/integration-service
  newTag: <SHA>
```

### Finding the upstream commit

To check what code is deployed when a test fails:

```bash
# Extract the pinned SHA for a service
grep 'ref=' operator/upstream-kustomizations/<service>/core/kustomization.yaml

# Then inspect that commit in the upstream repo
gh browse --repo konflux-ci/<service> <SHA>

# Or see recent commits after that SHA
gh api "repos/konflux-ci/<service>/commits?sha=main&per_page=10" \
  --jq '.[] | "\(.sha[:12]) \(.commit.message | split("\n")[0])"'
```

### Full upstream repo mapping

| Kustomization path | Upstream repo | Container image |
|--------------------|---------------|-----------------|
| `build-service/core/` | `konflux-ci/build-service` | `quay.io/konflux-ci/build-service` |
| `integration/core/` | `konflux-ci/integration-service` | `quay.io/konflux-ci/integration-service` |
| `release/core/` | `konflux-ci/release-service` | `quay.io/konflux-ci/release-service` |
| `image-controller/core/` | `konflux-ci/image-controller` | `quay.io/konflux-ci/image-controller` |
| `application-api/` | `redhat-appstudio/application-api` | (CRDs only) |
| `enterprise-contract/core/` | `conforma/crds` | (CRDs only) |
| `release/internal-services/` | `redhat-appstudio/internal-services` | (CRDs only) |
| `segment-bridge/` | `konflux-ci/segment-bridge` | `quay.io/konflux-ci/segment-bridge` |

### Test code structure

- **Suite**: `test/go-tests/tests/conformance/` — Ginkgo v2 + Gomega
- **Entry point**: `test/e2e/run-e2e.sh` → `go test ./tests/conformance -ginkgo.vv`
- **Framework helpers**: `test/go-tests/pkg/`
- **Config/scenarios**: `test/go-tests/tests/conformance/config/scenarios.go`

## CI Workflow Reference

### Operator E2E Tests (`operator-test-e2e.yaml`)

| Job | Purpose |
|-----|---------|
| `check-prerequisites` | Gate fork PRs |
| `check-changes` | Path-filter to skip if no relevant changes |
| `test` | Kind cluster + deploy + integration tests + e2e (matrix: amd64, arm64) |
| `test-skip` | No-op jobs matching test names for consistent status checks |
| `report-operator-e2e-status` | Update check runs for `/allow` dispatches |

**Artifact names**: `logs-amd64`, `logs-arm64`

### Common Failure Patterns

| Pattern | Likely cause | Where to look |
|---------|-------------|---------------|
| `no pipelinerun found for component` | PaC webhook chain failure — push event not processed | See **PaC webhook chain** below |
| `context deadline exceeded` in test | Test timeout — slow cluster or stuck reconciliation | `operator-logs.log`, `pipelineruns.json` |
| `ImagePullBackOff` in pod logs | Bad image tag or registry issue | `failed-pods-definitions.yaml` |
| `no matches for kind` in operator log | Missing CRDs — upstream kustomization issue | `operator-logs.log`, check CRD installation |
| `admission webhook denied` | Kyverno policy blocking resource creation | `events.json`, `kyverno-policy-pods.log` |
| `OOMKilled` in pod status | Container hit memory limit | `failed-pods-definitions.yaml`, `cluster-resources.log` |
| Reconcile error loop in operator | Controller bug or bad CR spec | `operator-logs.log` |
| PipelineRun stuck `Running` | Tekton task issue or missing resources | `pipelineruns.json`, `taskruns.json` |
| Different result amd64 vs arm64 | Architecture-specific bug or resource limits | Compare both artifact sets |
| `etcdserver: request timed out` or `etcdserver: leader changed` | etcd overwhelmed by CPU starvation or excessive watcher load on Kind cluster | `pods/kube-system_etcd-*.log` (etcd logs), `system-resources.log` (load average), `cluster-resources.log` (node conditions) |

