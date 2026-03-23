# Konflux Conformance Tests

### Description
These tests validate the core functionality of Konflux CI -- application creation, component build, integration testing, and release. They must always pass and serve as the baseline for any Konflux deployment.

They run against an upstream Konflux instance deployed via scripts in the [konflux-ci repository](https://github.com/konflux-ci/konflux-ci).

### Prerequisites

1. Fork https://github.com/konflux-ci/testrepo to your GitHub org (specified in `MY_GITHUB_ORG` env var)
2. Make sure the cluster you are about to run this test against is public (i.e. hosted on a public cloud provider)

### What the conformance test covers

1. Setup
   1. Create a user namespace
   1. Create a managed namespace used by release service for validating and releasing the built image
   1. Create required resources in managed-namespace
      1. Secret with container image registry credentials
      1. Service account with required roles and rolebindings
      1. PVC for the release pipeline
      1. Secret with cosign-public-key (used for validating the built image by EC)
      1. Release plan for the targeted application and namespace
      1. Release plan admission
      1. Conforma policy
1. Test scenario
   1. The application and component are created successfully
   1. Verify that the initial PaC pull request was created (triggers the default build PipelineRun)
   1. After merging the PR, another build PipelineRun is triggered
   1. The PipelineRun completes successfully
   1. The integration test PipelineRun is created and completes successfully
   1. The release pipeline succeeds and the release is marked as successful

### How to run

1) Follow the instructions in the [konflux-ci repository](https://github.com/konflux-ci/konflux-ci/blob/main/CONTRIBUTING.md#running-e2e-test) to install Konflux
2) From the repo root, run:
   ```bash
   ./test/e2e/run-e2e.sh
   ```
   This deploys test resources and runs the conformance suite.

   You can also run `go test . -v` directly from this directory, but that
   skips test resource deployment — you must ensure resources are already
   deployed (e.g. via `deploy-test-resources.sh`).

### Configuration

The test scenarios are defined in [scenarios.go](./config/scenarios.go). Update `UpstreamAppSpecs` to test your own component/repository.
