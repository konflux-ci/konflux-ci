package config

import (
	"fmt"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

var UpstreamAppSpecs = []ApplicationSpec{
	{
		Name:            "Test local instance of konflux-ci - docker-build-oci-ta-min pipeline",
		ApplicationName: "konflux-ci-upstream-docker-build-oci-ta-min",
		Skip:            false,
		ComponentSpec: ComponentSpec{
			Name:                       "konflux-ci-upstream",
			GitSourceUrl:               fmt.Sprintf("https://github.com/%s/%s", utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe"), "testrepo"),
			GitSourceRevision:          "47517b7ad6a09ada952f3de7eb8da729ffbf3d6d",
			GitSourceDefaultBranchName: "main",
			DockerFilePath:             "Dockerfile",
			BuildPipelineType:          constants.DockerBuildOciTAMin,
			IntegrationTestScenario: IntegrationTestScenarioSpec{
				GitURL:      fmt.Sprintf("https://github.com/%s/%s", utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe"), "testrepo"),
				GitRevision: "47517b7ad6a09ada952f3de7eb8da729ffbf3d6d",
				TestPath:    "integration-tests/testrepo-integration.yaml",
			},
		},
	},
}
