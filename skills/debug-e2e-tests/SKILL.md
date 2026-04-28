---
name: debug-e2e-tests
description: Debug failed Konflux e2e test runs by downloading logs and artifacts from GitHub Actions, analyzing test failures, and suggesting fixes. Use when the user asks to debug, investigate, or fix a failing e2e test, CI failure, or flaky test.
---

# Debug E2E Tests

Debug failed e2e test runs from GitHub Actions. Downloads test logs and cluster artifacts, identifies root causes, and suggests fixes to this repo or upstream dependencies.

**When this skill is activated, tell the user:** "Using the **debug-e2e-tests** skill to investigate this failure." Then show the checklist from the Workflow section below so the user can track progress.

## Prerequisites

- `gh` CLI authenticated with access to `konflux-ci/konflux-ci`
- `tar` and `unzip` available (standard on Linux/macOS)

## Workflow

Copy this checklist and track progress:

```
- [ ] Step 1: Identify the failed run
- [ ] Step 2: Download artifacts
- [ ] Step 3: Analyze test output (JUnit + Ginkgo logs)
- [ ] Step 4: Analyze cluster logs
- [ ] Step 5: (Optional) Inspect the tested commit
- [ ] Step 6: Determine root cause and suggest fix
```

### Step 1: Identify the Failed Run

If the user provides a PR number, run ID, or branch name, use that. Otherwise, find recent failures:

```bash
# List recent failed e2e runs
gh run list --repo konflux-ci/konflux-ci \
  --workflow "Operator E2E Tests" \
  --status failure --limit 10

# For a specific PR
gh run list --repo konflux-ci/konflux-ci \
  --workflow "Operator E2E Tests" \
  --branch <branch> --limit 5
```

Get the **run ID** from the output (first column).

To see which jobs failed within a run:

```bash
gh run view <run-id> --repo konflux-ci/konflux-ci
```

The e2e workflow has these jobs:
- **Run Operator E2E Tests (AMD64)** — main e2e on amd64
- **Run Operator E2E Tests (ARM64)** — main e2e on arm64

### Step 2: Download Artifacts

Use the helper script to download and extract artifacts:

```bash
bash .cursor/skills/debug-e2e-tests/scripts/download-logs.sh <run-id> [arch]
```

- `arch` defaults to `amd64`. Use `arm64` if that's the failing job.
- Artifacts are extracted to `/tmp/e2e-debug-<run-id>/`

If the script is unavailable, download manually:

```bash
WORKDIR="/tmp/e2e-debug-<run-id>"
mkdir -p "$WORKDIR" && cd "$WORKDIR"

# Download artifacts (name is "logs-amd64" or "logs-arm64")
gh run download <run-id> --repo konflux-ci/konflux-ci \
  --name logs-amd64 --dir "$WORKDIR/logs"
```

Also download the raw job log for the failing job:

```bash
# Get job IDs
gh api "repos/konflux-ci/konflux-ci/actions/runs/<run-id>/jobs" \
  --jq '.jobs[] | select(.conclusion=="failure") | "\(.id) \(.name)"'

# Download the full job log
gh api "repos/konflux-ci/konflux-ci/actions/jobs/<job-id>/logs" > "$WORKDIR/job.log"
```

### Step 3: Analyze Test Output

#### 3a. JUnit Report

Check the JUnit XML first for a quick summary of which tests failed:

```bash
# The JUnit file is at:
# logs/junit/junit-conformance-amd64.xml  (or arm64)
```

Look for `<testcase>` elements with `<failure>` children. Extract test names and failure messages.

#### 3b. Ginkgo Output

The raw job log (`job.log`) or stdout contains full Ginkgo output. Search for:

- `[FAILED]` — Ginkgo failure markers
- `FAIL!` — Go test failure
- `Timed out` or `context deadline exceeded` — timeout failures
- `Expected` / `to equal` / `to succeed` — Gomega assertion failures

The test suite is `Konflux Conformance` under `test/go-tests/tests/conformance/`. Cross-reference the failing test name with the source files there to understand what the test was doing.

### Step 4: Analyze Cluster Logs

The `logs/` directory (from `generate-err-logs.sh`) contains the cluster state at test failure time. Read files in this priority order:

| Priority | File | What to look for |
|----------|------|------------------|
| 1 | `operator-logs.log` | Operator crashes, reconcile errors, failed deployments |
| 2 | `konflux-crs-status.log` | Konflux / sub-CR status conditions showing `Ready=False` |
| 3 | `failed-pods-logs.log` | Container logs from pods with Warning events |
| 4 | `failed-pods-definitions.yaml` | Pod specs showing image pull errors, resource limits, crash loops |
| 5 | `failed-deployment-event-log.log` | Deployment rollout failures |
| 6 | `pipelinerun-res.log` | PipelineRun status, failed tasks, error messages |
| 7 | `taskrun-res.log` | TaskRun status, step container failures |
| 8 | `cluster-resources.log` | Node pressure, pending pods, resource exhaustion |
| 9 | `system-resources.log` | Host OOM, CPU saturation, disk full |

For deeper analysis, check structured artifacts under `logs/artifacts/`:

- `events.json` — all cluster events (search for Warning/error patterns)
- `deployments.json` — deployment availability and conditions
- `pipelineruns.json` / `taskruns.json` — Tekton resource details
- `pods/<namespace>_<pod>.log` — individual pod logs from key namespaces
- `repositories.json` — PipelinesAsCode Repository CRs and their status

For details on log structure, see [reference.md](reference.md).

#### 4a. "No PipelineRun found" failures — PaC webhook chain

When the failure is **"no pipelinerun found for component …"**, read
[pac-webhook-debugging.md](pac-webhook-debugging.md) for detailed guidance
on tracing the PaC webhook delivery chain.

### Step 5: Inspect the Tested Commit (Optional)

This step is not always needed. Use it when the logs from Steps 3–4 suggest a regression in this repo's code (operator changes, kustomization updates, test modifications, deployment scripts) and you want to confirm by looking at the actual diff. Skip it when the root cause is already clear from the logs alone (e.g., an obvious infrastructure issue or upstream service crash).

#### 5a. Get the head commit and base branch from the run

```bash
# Show the head SHA, branch, and associated PR for the run
gh run view <run-id> --repo konflux-ci/konflux-ci --json headSha,headBranch \
  --jq '"branch: \(.headBranch)\ncommit: \(.headSha)"'

# Find the PR's base branch (don't assume main)
gh pr list --repo konflux-ci/konflux-ci --head <branch> --json number,baseRefName \
  --jq '.[0] | "PR #\(.number) → base: \(.baseRefName)"'
```

#### 5b. Check out and inspect the diff

```bash
# Fetch the branch and check it out
git fetch origin <branch> <base-branch>
git checkout <head-sha>

# View the full diff against the base branch
git diff origin/<base-branch>...<head-sha> --stat
git diff origin/<base-branch>...<head-sha>

# If the PR has multiple commits, review them individually
git log --oneline origin/<base-branch>..<head-sha>
```

Scan the diff for changes to files related to the failure (e.g., kustomizations, operator code, test helpers, deployment scripts). Cross-reference with the error signals from Steps 3–4.

#### 5c. Investigate git history for suspected regressions

If a specific file or component looks suspicious, use git history to understand recent changes:

```bash
# Recent commits touching a suspect file
git log --oneline -20 -- <file-path>

# Show each change with diff
git log -p -5 -- <file-path>

# Compare the suspect file between two points
git diff <known-good-sha> <head-sha> -- <file-path>
```

This is especially useful when the failure is new and the logs point to a particular controller, kustomization, or test file — tracing the history can pinpoint exactly which change introduced the regression.

### Step 6: Determine Root Cause and Suggest Fix

Classify the failure into one of these categories:

#### A. Test code issue (fix in this repo)

The test assertion is wrong, flaky, or the test setup is incomplete.
- Fix location: `test/go-tests/tests/conformance/`
- Also check: `test/e2e/run-e2e.sh`, `deploy-test-resources.sh`

#### B. Operator issue (fix in this repo)

The Konflux operator itself is broken — reconciliation failures, bad defaults, missing RBAC.
- Fix location: `operator/` (controllers, API types, RBAC, kustomize overlays)

#### C. Upstream dependency issue (fix in upstream repo)

A Konflux microservice is broken. Identify which service from the logs, then map it to the upstream repo:

| Service | Upstream Repo | Kustomization |
|---------|---------------|---------------|
| Build Service | `konflux-ci/build-service` | `operator/upstream-kustomizations/build-service/` |
| Integration Service | `konflux-ci/integration-service` | `operator/upstream-kustomizations/integration/` |
| Release Service | `konflux-ci/release-service` | `operator/upstream-kustomizations/release/` |
| Image Controller | `konflux-ci/image-controller` | `operator/upstream-kustomizations/image-controller/` |
| Application API (CRDs) | `redhat-appstudio/application-api` | `operator/upstream-kustomizations/application-api/` |
| Enterprise Contract | `conforma/crds` | `operator/upstream-kustomizations/enterprise-contract/` |

Check the pinned commit SHA in the kustomization to identify which upstream version is deployed, then inspect that repo for relevant issues or recent changes.

#### D. Infrastructure / flaky failure

Resource exhaustion, network issues, Kind cluster instability.
- Check `system-resources.log` and `cluster-resources.log`
- If the test passed on one arch but failed on another, likely infra-related
- If the failure is intermittent across runs, likely flaky

#### E. Cross-job interference (shared external state)

When a failure looks flaky, check whether **parallel CI jobs are competing
over shared external resources**. The AMD64 and ARM64 e2e jobs run on separate
clusters but may share the same external services (Quay organization, GitHub
repos, OCI registries). Multiple PRs or retries can also run concurrently.

If two jobs create, modify, or depend on the **same external resource** (e.g.,
same Quay repository path, same OCI artifact tag, same GitHub branch), one job
can silently corrupt the other's state — overwriting images, rotating
credentials, deleting data, or pushing conflicting artifacts.

**How to check:**
1. Compare the `BeforeAll` setup logs from both arches (in their JUnit XML
   `<system-err>` blocks) — look for any external resource identifiers
   (image URLs, repo paths, branch names, artifact names) that are identical.
2. Check if the failure is non-deterministic: one arch passes while the other
   fails, or results flip between re-runs with no code change.
3. Check whether other PRs or workflow runs were active at the same time and
   could have touched the same external resources.

**Typical symptoms:** intermittent failures, "unauthorized" registry errors,
wrong image content, unexpected data in pipeline results, one arch passes
while the other fails, or tests that break only when CI is busy.

**Fix:** ensure every concurrent job uses unique external resource names. See
`operator/hack/setup-release.sh` `--image-name-prefix` (`-I`) for an example.

### Output Format

Present findings as:

```markdown
## E2E Failure Analysis

**Run:** <link to run>
**Failed job:** <job name>
**Failed test(s):** <test name(s)>

### Root Cause
<Concise description of why the test failed>

### Evidence
<Key log excerpts that support the diagnosis>

### Category
<A/B/C/D from above>

### Suggested Fix
<Specific code changes or actions to resolve the issue>
- File(s) to change: ...
- If upstream: repo + what to fix
```
