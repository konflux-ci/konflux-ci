Contributing Guidelines
===

<!-- toc -->

- [Documentation Conventions](#documentation-conventions)
- [Editing Markdown Files](#editing-markdown-files)
- [Using KubeLinter](#using-kubelinter)
- [Operator Development](#operator-development)
- [CI/CD and Testing](#cicd-and-testing)
  * [Automated E2E Tests](#automated-e2e-tests)
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

## Automated E2E Tests

The repository includes automated tests that run in GitHub Actions on both x86_64
and ARM64 architectures. There are **two test suites** in `test/go-tests`:

- **Integration tests** (suite "GoTests"): Quick checks that do not require full E2E
  secrets. Run with `cd test/go-tests && go test .`
- **E2E tests** (suite "Konflux E2E"): Full end-to-end flow (application, component,
  build, integration test, release). Requires cluster and E2E credentials. Copy
  `test/e2e/e2e.env.template` to `test/e2e/e2e.env`, fill in the values, then
  source it and run (from repo root; no cd, repeat as needed): `source test/e2e/e2e.env` then `go -C test/go-tests test ./tests/conformance -v`
  The E2E test code lives in `test/go-tests/tests/conformance/` and is maintained in this repo.
  The release-service-catalog revision is read from `test/e2e/release-service-catalog-revision` when not set in env (so your copy of `e2e.env` does not drift).

Workflow `.github/workflows/operator-test-e2e.yaml` runs both suites when
operator-related changes are detected: first integration (`go test .`), then E2E
(env set from secrets, then the same `go test` command).

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
# Edit test/e2e/e2e.env with GH_ORG, GH_TOKEN, QUAY_DOCKERCONFIGJSON, etc.
```

See `test/e2e/e2e.env.template` for all E2E variables and descriptions. You do not need to set `RELEASE_SERVICE_CATALOG_REVISION` in `e2e.env`; it is read from `test/e2e/release-service-catalog-revision` when unset.

## Running the test

Deploy Konflux and test resources (in one terminal):
```bash
./scripts/deploy-local.sh
./deploy-test-resources.sh
```

Run the E2E tests (source E2E env in the same terminal where you run the test, or in a second terminal). From repo root:
```bash
source test/e2e/e2e.env
go -C test/go-tests test ./tests/conformance -v
```

Note: The deploy step uses `scripts/deploy-local.env` (GitHub App, Quay for image-controller, Smee). The E2E step uses `test/e2e/e2e.env` (GitHub/Quay for E2E flows only). They are separate so you never load deploy secrets into the shell where you only run tests.

The source code of the E2E tests is in this repo under `test/go-tests/tests/conformance/`.
