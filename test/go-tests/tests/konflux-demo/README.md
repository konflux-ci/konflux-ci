# KONFLUX Demo test

### Description
This test simulates typical user scenario (creation of Application, Component, build of a container image, testing it and releasing it).

It is possible to run this test against "downstream" (deployed via scripts in [infra-deployments](https://github.com/redhat-appstudio/infra-deployments)) and "upstream" (deployed via scripts in [konflux-ci repository](https://github.com/konflux-ci/konflux-ci)) version of Konflux.

### Prerequisites for running the build scenario against your own cluster


#### For downstream version of Konflux
1. Fork https://github.com/redhat-appstudio-qe/hacbs-test-project to your GitHub org (specified in `MY_GITHUB_ORG` env var)
2. Make sure the cluster you are about to run this test against is public (i.e. hosted on a public cloud provider)

#### For upstream version of Konflux
1. Fork https://github.com/konflux-ci/testrepo to your GitHub org (specified in `MY_GITHUB_ORG` env var)
2. Make sure the cluster you are about to run this test against is public (i.e. hosted on a public cloud provider)

#### Test Steps
1. Setup
   
2. Test Scenario
   1. The application was created successfully
   2. The Component (default) Build finished successfully
   3. Snapshot was created and integration test finished successfully

### Default build with Integration test
1. Setup
   1. Create a user namespace
   1. Create a managed namespace used by release service for validating and releasing the built image
   1. Create required resources in managed-namespace
      1. Secret with container image registry credentials (used for pushing the image to container registry)
      1. Service account that mounts the secret, including required roles and rolebindings
      1. PVC for the release pipeline
      1. Secret with cosign-public-key (used for validating the built image by EC)
      1. Release plan for the targeted application and (user) namespace
      1. Release strategy, Release plan admission
      1. Conforma policy
1. Test scenario
   1. The application and component were created successfully
   1. Verify that the initial PaC pull request was created in the component's repo (this will also trigger the default build pipelinerun)
   1. After merging the PR, there should be another build pipelinerun triggered in user namespace
   1. Make sure the pipelinerun completes successfully
   1. Make sure that the resulting SBOM file can be pulled from the container registry (where also the image was pushed) and it is saved in expected format
   1. Make sure that integration test pipelinerun is created and completes successfully
   1. The release pipeline should succeed and the release should be marked as successful

Steps to run Konflux demo test:

#### For downstream version of Konflux
1) Follow the instructions from the [Readme](../../docs/Installation.md) scripts to install Konflux in E2E mode
2) Run the E2E test: `ginkgo -v --label-filter="konflux"`

#### For upstream version of Konflux
1) Follow the instructions in the [konflux-ci repository](https://github.com/konflux-ci/konflux-ci/blob/main/CONTRIBUTING.md#running-e2e-test) to install Konflux and run tests in your Kubernetes/OpenShift cluster.
2) To run the E2E test from this repository: `ginkgo -v` (or `go test . -v` from this directory)

**Note** 

## Test Generator

The test specs in konflux-demo-suite are generated dynamically using ginkgo specs.

If you want to test your own Component (repository), all you need to do is to update the `TestScenarios` variable in [scenarios.go](./config/scenarios.go)

#### Setup configuration for private repositories

1. Define in your configuration for the application and the component
Example of a test scenario for GitHub private repository:

```go
var ApplicationSpecs = []ApplicationSpec{
    {
        Name:            "nodejs private component test",
        ApplicationName: "nodejs-private-app",
        ComponentSpec: ComponentSpec{
            {
                Name:              "nodejs-private-comp",
                Private:           true,
                Language:          "JavaScript",
                GitSourceUrl:      "https://github.com/redhat-appstudio-qe-bot/nodejs-health-check.git",
            },
        },
    },
}
```

2. Run the e2e tests
