# Reference: OpenShift CI (Prow) Artifact Structure

## GCS URL Pattern

All Prow job artifacts are stored in public GCS buckets accessible via HTTP
through gcsweb (no auth required).

**Presubmit (PR) jobs:**
```
gs://test-platform-results/pr-logs/pull/<org>_<repo>/<PR-number>/<job-name>/<build-id>/
```

**Periodic jobs:**
```
gs://test-platform-results/logs/<job-name>/<build-id>/
```

**HTTP access via gcsweb:**
```
https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/<bucket-relative-path>
```

## Top-Level Files

| File | Description |
|------|-------------|
| `build-log.txt` | ci-operator top-level log (step orchestration, image builds) |
| `started.json` | Job start time, PR number, repo refs, commit SHAs |
| `finished.json` | Job result (`passed`/`failed`), completion timestamp |
| `prowjob.json` | Full ProwJob CR: job name, refs, labels, spec |
| `prowjob_junit.xml` | Single JUnit test: "Job run should complete before timeout" |
| `sidecar-logs.json` | Prow sidecar upload/censoring logs |

## Artifacts Directory

```
artifacts/
├── ci-operator.log              # Detailed ci-operator execution log
├── ci-operator-step-graph.json  # Full step dependency graph with timing
├── ci-operator-metrics.json     # Step durations and success/failure
├── junit_operator.xml           # JUnit for ALL multi-stage steps (key file!)
├── metadata.json                # Repo, PR, commit, pod name, namespace
├── build-logs/                  # Source compilation logs
├── build-resources/             # K8s objects from build namespace
│   ├── builds.json
│   ├── events.json
│   ├── pods.json
│   └── imagestreams.json
├── release/                     # Release ImageStream
└── <workflow-name>/             # Test workflow steps (see below)
```

## Workflow Steps Directory

For Konflux e2e jobs, the workflow directory is named after the test
(e.g., `konflux-e2e-v420-optional`). Each step directory contains:

```
<workflow-name>/
├── <step-name>/
│   ├── build-log.txt       # Step's stdout/stderr
│   ├── finished.json       # Step result
│   ├── sidecar-logs.json   # Upload logs
│   └── artifacts/          # Files written to $ARTIFACT_DIR by the step
└── ...
```

### Konflux-specific Steps

| Step | Purpose | Key artifacts |
|------|---------|---------------|
| `ipi-install-rbac` | Cluster RBAC setup | (rarely relevant) |
| `redhat-appstudio-health-check` | Pre-test cluster health validation | `build-log.txt`, `artifacts/` |
| `konflux-ci-install-operator` | Deploys Konflux operator + CRs | `build-log.txt` (deployment log) |
| `konflux-ci-e2e-tests` | Runs the Ginkgo test suite | `build-log.txt` (test output) |
| `redhat-appstudio-gather` | Collects cluster state post-test | `artifacts/` (main debug data) |
| `redhat-appstudio-report` | Generates JUnit reports | `artifacts/junit.xml`, `junit-rp.xml` |
| `gather-extra` | Additional cluster diagnostics | `artifacts/` (pod logs under `artifacts/pods/`) |
| `gather-audit-logs` | Kubernetes audit logs | `artifacts/` |
| `gather-must-gather` | OpenShift must-gather | `artifacts/` (large tarball) |
| `konflux-ci-unregister-sprayproxy` | Cleanup sprayproxy registration | `build-log.txt` |

## redhat-appstudio-gather Artifacts

The gather step dumps all Konflux-related cluster resources as JSON files.
These are the primary debugging data source:

```
redhat-appstudio-gather/artifacts/
├── must-gather-appstudio/           # oc adm must-gather output (tarball)
├── must-gather-network-appstudio/   # Network diagnostics must-gather
├── applications_appstudio.json      # HAS Application CRs
├── components.json                  # HAS Component CRs
├── componentdetectionqueries.json   # CDQ CRs
├── snapshots.json                   # Snapshot CRs
├── integrationtestscenarios.json    # ITS CRs
├── pipelineruns.json                # Tekton PipelineRuns
├── taskruns.json                    # Tekton TaskRuns
├── pipelines.json                   # Tekton Pipeline definitions
├── tasks.json                       # Tekton Task definitions
├── repositories.json                # PipelinesAsCode Repository CRs
├── releases.json                    # Release CRs
├── releaseplans.json                # ReleasePlan CRs
├── releaseplanadmissions.json       # ReleasePlanAdmission CRs
├── enterprisecontractpolicies.json  # EC policy CRs
├── environments.json                # Environment CRs
├── promotionruns.json               # PromotionRun CRs
├── tektonconfigs.json               # TektonConfig CRs
├── tektonpipelines.json             # TektonPipeline CRs
├── tektonchains.json                # TektonChain CRs
├── clusterinterceptors.json         # Tekton ClusterInterceptors
├── eventlisteners.json              # Tekton EventListeners
├── triggerbindings.json             # Tekton TriggerBindings
├── triggertemplates.json            # Tekton TriggerTemplates
├── triggers.json                    # Tekton Triggers
├── resolutionrequests.json          # Tekton ResolutionRequests
└── ...                              # Many more CR types (see actual listing)
```

## gather-extra Artifacts

The `gather-extra` step collects additional cluster diagnostics not covered by
`redhat-appstudio-gather`. Its `artifacts/` directory contains useful cluster
state, including pod logs from the test cluster:

```
gather-extra/artifacts/
├── pods/                   # Pod logs from the test cluster
│   ├── <namespace>_<pod>.log
│   └── ...
└── ...                     # Other cluster diagnostic files
```

The `pods/` directory is especially useful when you need container-level logs
that are not included in the `redhat-appstudio-gather` JSON dumps.

## Mapping Prow Artifacts to GitHub Actions Artifacts

| Analysis need | GitHub Actions file | Prow equivalent |
|---------------|--------------------|-----------------| 
| Test JUnit results | `logs/junit/junit-conformance-*.xml` | `junit/junit.xml` |
| Test Ginkgo output | `job.log` | `steps/konflux-ci-e2e-tests/build-log.txt` |
| Operator deploy log | `logs/operator-logs.log` | `steps/konflux-ci-install-operator/build-log.txt` |
| PipelineRun details | `logs/artifacts/pipelineruns.json` | `gather/pipelineruns.json` |
| TaskRun details | `logs/artifacts/taskruns.json` | `gather/taskruns.json` |
| Component CRs | `logs/artifacts/components.json` | `gather/components.json` |
| Repository CRs | `logs/artifacts/repositories.json` | `gather/repositories.json` |
| Cluster events | `logs/artifacts/events.json` | (in must-gather tarball) |
| Pod logs | `logs/artifacts/pods/*.log` | `gather-extra/artifacts/pods/` |
| Node/resource pressure | `logs/cluster-resources.log` | (in must-gather or gather-extra) |

## Accessing Additional Files

The download script fetches the most commonly needed files. For deeper analysis,
fetch additional files directly:

```bash
GCSWEB="https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs"
BASE="test-platform-results/pr-logs/pull/konflux-ci_konflux-ci/<PR>/<job>/<build-id>"

# Browse available files (returns HTML directory listing):
curl -s "${GCSWEB}/${BASE}/artifacts/<workflow>/redhat-appstudio-gather/artifacts/"

# Download a specific file:
curl -sfL "${GCSWEB}/${BASE}/artifacts/<workflow>/redhat-appstudio-gather/artifacts/<file>.json" -o <local-path>
```

## Common Prow-Specific Failure Patterns

| Pattern | Likely cause | Where to look |
|---------|-------------|---------------|
| `konflux-ci-install-operator` step failed | Operator deploy issue, OCP incompatibility | `steps/konflux-ci-install-operator/build-log.txt` |
| `redhat-appstudio-health-check` step failed | Cluster not ready, missing prerequisites | `steps/redhat-appstudio-health-check/build-log.txt` |
| Step timeout in `junit_operator.xml` | Step exceeded its time limit | `artifacts/ci-operator-step-graph.json` for timing |
| `ipi-install-*` failure | Cluster provisioning failed (infra) | Not a Konflux code issue |
| All steps pass but `konflux-ci-e2e-tests` fails | Test-level failure (same as GHA) | `steps/konflux-ci-e2e-tests/build-log.txt` |
| `gather` step failed | Cluster in bad state, gather script error | Usually ignorable for root cause |
