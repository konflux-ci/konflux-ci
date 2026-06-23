Contributing Guidelines
===

<!-- toc -->

- [Documentation Conventions](#documentation-conventions)
- [Cross-Platform Script Compatibility](#cross-platform-script-compatibility)
- [Editing Markdown Files](#editing-markdown-files)
- [Using KubeLinter](#using-kubelinter)
- [Operator Development](#operator-development)
- [CI/CD and Testing](#cicd-and-testing)
  * [Operator rendered manifests](#operator-rendered-manifests)
  * [Automated E2E Tests](#automated-e2e-tests)
  * [OpenShift CI Periodic Tests](#openshift-ci-periodic-tests)
  * [ARM64 Testing](#arm64-testing)
- [Running E2E test](#running-e2e-test)
  * [Prerequisites](#prerequisites)
  * [Setup](#setup)
  * [Running the test](#running-the-test)

<!-- tocstop -->

# Documentation Conventions

When writing user-facing documentation (README, deployment guides, etc.):

- **Gear icon (:gear:)**: Use `:gear:` to mark steps where the user must take action. This makes action items visually distinct from explanatory text.
- **One command per code block**: Split bash code blocks so each contains a single copyable command. This allows users to use the copy button on each block rather than manually selecting lines.
- **Descriptive text between blocks**: Add a brief description before each code block explaining what the command does.

Example:

```markdown
:gear: Create configuration from template:

\`\`\`bash
cp scripts/deploy-local.env.template scripts/deploy-local.env
\`\`\`

:gear: Deploy Konflux:

\`\`\`bash
./scripts/deploy-local.sh
\`\`\`
```

# Cross-Platform Script Compatibility

Scripts in this repository that are intended to run on the user's machine must
work on both **Linux** and **macOS**. This applies to:

- **Deployment scripts** — e.g. `scripts/deploy-local.sh`, `deploy-deps.sh`,
  `deploy-konflux-on-ocp.sh`
- **CLI helper scripts** — e.g. scripts under `operator/upstream-kustomizations/cli/`
- **ConfigMap-hosted scripts** — scripts stored as ConfigMaps on the cluster
  that users fetch and run locally (e.g. `setup-release.sh`, `create-tenant.sh`)

When writing or modifying these scripts, follow these guidelines:

- **Prefer POSIX-compatible constructs** over Bash-specific or GNU-specific
  extensions.
- **Avoid GNU-only flags** for common utilities. For example, `sed -i`
  requires a backup extension argument on macOS (`sed -i '' ...`), `date`
  flags differ between GNU and BSD, and `readlink -f` is not available on
  macOS without `coreutils`.
- **Test with both GNU and BSD coreutils** — utilities like `sed`, `grep`,
  `date`, `readlink`, and `mktemp` behave differently across platforms.
- **Do not assume `/bin/bash` is Bash 4+** — macOS ships Bash 3.2 by default.
  Avoid associative arrays (`declare -A`), `mapfile`/`readarray`, and other
  Bash 4+ features unless the script explicitly requires and checks for a
  newer version.

# Editing Markdown Files

If the structure of markdown files containing table of contents changes, those
need to be updated as well.

To do that, run the command below and add the produced changes to your PR.

```bash
find . -name "*.md" -not -path "./operator/docs/*" | while read -r file; do
    npx markdown-toc $file -i
done
```

# Using KubeLinter

Run [KubeLinter](https://docs.kubelinter.io/#/?id=usage)
locally before submitting a PR to this repository.

After [installing KubeLinter](https://docs.kubelinter.io/#/?id=installing-kubelinter)
and adding it to the $PATH env variable, create a new folder in the base directory
using `mkdir -p ./.kube-linter/`. Then, run the following Bash script:
```
    find . -name "kustomization.yaml" -o -name "kustomization.yml" | while read -r file; do
    dir=$(dirname "$file")
    dir=${dir#./}
    output_file=$(echo "out-$dir" | tr "/" "-")
    kustomize build "$dir" > "./.kube-linter/$output_file.yaml"
    done
```
Finally, run `kube-linter lint ./.kube-linter` to recursively apply KubeLinter checks on this folder.

Consider creating a configuration file. To do so, check
[KubeLinter config documentation](https://docs.kubelinter.io/#/configuring-kubelinter)
this file will allow you to ignore or include specific KubeLinter checks.

# Operator Development

For building and running the operator from source, see the
[operator README](operator/README.md). To deploy a locally built operator on a
Kind cluster, use `OPERATOR_INSTALL_METHOD=build` with `deploy-local.sh`.

# CI/CD and Testing

## Operator rendered manifests

Pinned upstream components live under `operator/upstream-kustomizations/`. The
operator bundles pre-rendered YAML under `operator/pkg/manifests/<component>/`.
Those files must match `kustomize build` for the same pins; CI enforces that via
`.github/workflows/verify-manifests-in-sync.yaml`.

Helm-rendered **cert-manager** and **trust-manager** manifests under
`dependencies/` (and extracted envtest CRDs) must match the chart versions in
`.github/scripts/export-third-party-chart-env.sh`, which MintMaker/Renovate
updates alongside the scheduled update workflow.

When **MintMaker** or **Renovate** opens a PR that only bumps digests or chart
versions, a **companion PR** may be opened automatically
(`.github/workflows/renovate-manifest-companion.yaml`) that includes the matching
rendered output. **Prefer merging the companion PR** (or a single PR that
includes both pins and regenerated files) so `main` never carries mismatched
pins and manifests.

When verify fails on a **pull request**, CI cancels in-progress or queued
**Operator E2E Tests** runs for the same commit (with retries, so runs queued
after verify starts are still caught). E2E was cancelled because manifests are
out of sync; fix verify or use the manifest companion PR. To keep E2E running
despite a verify failure, add the label `force-run-e2e` to the PR.

Source PRs labeled `superseded-by-companion` fail the Operator E2E workflow gate
(merge the manifest companion PR instead). Add `force-run-e2e` to run full E2E on
the source PR anyway.

Source PRs labeled `pending-upstream-image` are blocked for the same reason when
the manifest companion workflow finds upstream container image(s) missing from
their registries. A companion PR is not opened in that case; the workflow posts a
comment on the source PR with the missing image(s). The label is removed
automatically on the next successful companion run once images are available.
Konflux Tekton build and E2E PipelineRuns are also skipped for this label.
Maintainers can add the `skip-image-verify` label and re-run the manifest
companion workflow to bypass image verification.

**Operator E2E Tests** does not run when labels alone change (only on new
commits, reopen, merge queue, or maintainer `/allow` on fork PRs). After you add
`force-run-e2e`, start CI manually—for example re-run **Operator E2E Tests** from
the PR Checks or Actions UI, or push a new commit to the PR branch.

## Automated E2E Tests

The repository includes automated tests that run in GitHub Actions on both x86_64
and ARM64 architectures. There are **two test suites** in `test/go-tests`:

- **Integration tests** (suite "GoTests"): Quick checks that do not require full E2E
  secrets. Run with `cd test/go-tests && go test .`
- **E2E tests** (suite "Konflux E2E"): Full end-to-end flow (application, component,
  build, integration test, release). Requires cluster and E2E credentials. Copy
  `test/e2e/e2e.env.template` to `test/e2e/e2e.env`, fill in the values, then
  source it and run (from repo root): `source test/e2e/e2e.env` then `./test/e2e/run-e2e.sh`
  The E2E test code lives in `test/go-tests/tests/conformance/` and is maintained in this repo.
  The default release catalog revision is `CATALOG_REVISION` in `operator/upstream-kustomizations/cli/setup-release.sh` (Renovate-tracked). Conformance tests that need a pinned build pipeline bundle read `CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE` from the same `build-service` manifests as CI; the Tekton flow sets that via `scripts/operator-e2e/prepare-conformance-env.sh`.

Workflow `.github/workflows/operator-test-e2e.yaml` runs both suites when
operator-related changes are detected: first integration (`go test .`), then E2E
(env set from secrets, then the same `go test` command).

## OpenShift CI Periodic Tests

In addition to GitHub Actions, the repository has periodic E2E tests running on
OpenShift CI (Prow) infrastructure. These tests run daily and validate Konflux
deployment on OCP 4.20 clusters:

- [x86_64 E2E Tests](https://prow.ci.openshift.org/?job=periodic-ci-konflux-ci-konflux-ci-main-ocp420-konflux-e2e-v420)
- [ARM64 E2E Tests](https://prow.ci.openshift.org/?job=periodic-ci-konflux-ci-konflux-ci-main-ocp420-arm64-konflux-e2e-v420-arm64)

## ARM64 Testing

ARM64 integration tests run on GitHub-hosted ARM runners and validate that:
- The operator builds correctly for ARM64 architecture
- All dependencies and services work on ARM64
- Integration test suite passes on ARM64

The ARM64 workflow uses architecture-specific binaries:
- kind: `kind-linux-arm64`
- kubectl: `bin/linux/arm64/kubectl`

The operator container image is built natively for ARM64 using the multi-arch
Containerfile which automatically detects the build architecture via
`TARGETARCH` and `TARGETOS` build arguments.

# Running E2E test
To validate changes more quickly, run the E2E test, which validates that:
* Application and Component can be created
* Build PipelineRun is triggered and can finish successfully
* Integration test gets triggered and finishes successfully
* Application Snapshot can be released successfully

## Prerequisites
* Fork of https://github.com/konflux-ci/testrepo is created and your GitHub App is installed there
* Konflux is deployed on `kind` cluster (follow the guide in README)
* quay.io organization that has `test-images` repository created, with robot account that has admin access to that repo

## Setup

Create the deploy configuration (for deploying Konflux):
```bash
cp scripts/deploy-local.env.template scripts/deploy-local.env
# Edit scripts/deploy-local.env with your GitHub App credentials
```

Create the E2E test configuration (only needed when running E2E tests):
```bash
cp test/e2e/e2e.env.template test/e2e/e2e.env
# Edit test/e2e/e2e.env with GH_ORG, GH_TOKEN, etc.
```

See `test/e2e/e2e.env.template` for all E2E variables and descriptions. Release infrastructure (managed namespace, ImageRepositories, ReleasePlan, etc.) is set up automatically by `operator/upstream-kustomizations/cli/setup-release.sh`, which the test calls during `BeforeAll`. The release-service-catalog revision is embedded in the script as its default and tracked by Renovate.

## Running the test

Deploy Konflux (in one terminal):
```bash
./scripts/deploy-local.sh
```

Run the E2E tests (source E2E env in the same terminal where you run the test, or in a second terminal). From repo root:
```bash
source test/e2e/e2e.env
./test/e2e/run-e2e.sh
```

The script runs proxy integration tests and the conformance suite. Demo-user fixtures
(`deploy-test-resources.sh`) are skipped unless `E2E_DEPLOY_TEST_RESOURCES=true` is set
(for Kind Dex `proxy-dex` RBAC; see `test/e2e/e2e.env.template`). GHA and Tekton enable
fixtures in CI; OpenShift overlay e2e does not need them.

Note: The deploy step uses `scripts/deploy-local.env` (GitHub App, Quay for image-controller, Smee). The E2E step uses `test/e2e/e2e.env` (GitHub token for PaC flows). They are separate so you never load deploy secrets into the shell where you only run tests.

The source code of the E2E tests is in this repo under `test/go-tests/tests/conformance/`.
