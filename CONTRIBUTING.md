Contributing Guidelines
===

<!-- toc -->

- [Editing Markdown Files](#editing-markdown-files)
- [Using KubeLinter](#using-kubelinter)
- [Operator Development](#operator-development)
  * [Building the Operator Locally](#building-the-operator-locally)
  * [Operator Development Commands](#operator-development-commands)
- [CI/CD and Testing](#cicd-and-testing)
  * [Automated E2E Tests](#automated-e2e-tests)
  * [ARM64 Testing](#arm64-testing)
- [Running E2E test](#running-e2e-test)
  * [Prerequisites](#prerequisites)
  * [Setup](#setup)
  * [Running the test](#running-the-test)

<!-- tocstop -->

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

Please consider running [KubeLinter](https://docs.kubelinter.io/#/?id=usage)
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
finally, run `kube-linter lint ./.kube-linter` to recursively apply KubeLinter checks on this folder.

It may be also recommended to create a configuration file. To do so please check
[KubeLinter config documentation](https://docs.kubelinter.io/#/configuring-kubelinter)
this file will allow you to ignore or include specific KubeLinter checks.

# Operator Development

If you're working on the Konflux operator itself and need to test your changes:

## Building the Operator Locally

Use the `build` installation method with the local deployment script:

```bash
# Configure environment
cp scripts/deploy-local.env.template scripts/deploy-local.env
# Edit deploy-local.env with your secrets

# Deploy with locally built operator
OPERATOR_INSTALL_METHOD=build ./scripts/deploy-local.sh
```

This will:
1. Build the operator image from your local changes
2. Load the image into the Kind cluster
3. Install CRDs using `make install`
4. Deploy the operator using `make deploy`

## Operator Development Commands

```bash
cd operator

# Run tests
make test

# Build operator binary
make build

# Build operator image
make docker-build IMG=localhost/konflux-operator:dev

# Install CRDs
make install

# Deploy operator to cluster
make deploy IMG=localhost/konflux-operator:dev

# Uninstall CRDs
make uninstall

# Undeploy operator
make undeploy
```

See the [operator README](operator/README.md) for more development commands.

# CI/CD and Testing

## Automated E2E Tests

The repository includes automated tests that run in GitHub Actions on both x86_64
and ARM64 architectures:

- **x86_64 E2E Tests**: Defined in `.github/workflows/operator-test-e2e.yaml`,
  runs on `ubuntu-latest` and executes the full E2E test suite
- **ARM64 Integration Tests**: Defined in
  `.github/workflows/operator-integration-test-arm.yaml`, runs on
  `ubuntu-24.04-arm` and executes integration tests

Both workflows run in parallel when changes to the `operator/` directory are
detected. The x86_64 workflow runs the full E2E test suite, while the ARM64
workflow runs integration tests to validate ARM64 compatibility.

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
In order to validate changes quicker, it is possible to run E2E test, which validates that:
* Application and Component can be created
* Build PipelineRun is triggered and can finish successfully
* Integration test gets triggered and finishes successfully
* Application Snapshot can be released successfully

## Prerequisites
* Fork of https://github.com/konflux-ci/testrepo is created and your GitHub App is installed there
* Konflux is deployed on `kind` cluster (follow the guide in README)
* quay.io organization that has `test-images` repository created, with robot account that has admin access to that repo

## Setup

Create the deploy-local.sh configuration:
```bash
cp scripts/deploy-local.env.template scripts/deploy-local.env
# Edit scripts/deploy-local.env with your GitHub App credentials
```

Or just export the E2E test environment variables:
```bash
# quay.io org where the built and released image will be pushed to
export QUAY_ORGANIZATION=""
# quay.io org OAuth access token
export QUAY_TOKEN=""
# Content of quay.io credentials config for the robot account with access to $QUAY_ORGANIZATION/test-images
export QUAY_DOCKERCONFIGJSON="$(< /path/to/docker/config.json)"
# URL of the smee.io channel you created
export SMEE_CHANNEL=""
# Name of the GitHub org/username where https://github.com/konflux-ci/testrepo is forked
export GH_ORG=""
# GitHub token with permissions to merge PRs in your GH_ORG
export GH_TOKEN=""
```

## Running the test

Run (from the root of the repository directory):
```bash
./scripts/deploy-local.sh
./deploy-test-resources.sh
./test/e2e/run-e2e.sh
```

Note: The `deploy-local.sh` script reads `SMEE_CHANNEL`, `QUAY_TOKEN`, and
`QUAY_ORGANIZATION` from the environment or from `scripts/deploy-local.env` to
configure E2E prerequisites (secrets, Smee, image-controller).

The source code of the test is located [.](https://github.com/konflux-ci/e2e-tests/tree/main/tests/konflux-demo)
