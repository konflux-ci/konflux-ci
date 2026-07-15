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
			Name:              "konflux-ci-upstream",
			GitSourceUrl:      fmt.Sprintf("https://github.com/%s/%s", utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, constants.DefaultGitHubE2EOrganization), "testrepo"),
			GitSourceRevision: "878eb2976b97946f577a8dbb0cc391d5370efbbb",
			DockerFilePath:    "Dockerfile",
			BuildPipelineType: constants.DockerBuildOciTAMin,
			IntegrationTestScenario: IntegrationTestScenarioSpec{
				GitURL:      fmt.Sprintf("https://github.com/%s/%s", utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, constants.DefaultGitHubE2EOrganization), "testrepo"),
				GitRevision: "7ab8dd0157209308324be243d98301d8be3ae295",
				TestPath:    "integration-tests/testrepo-integration.yaml",
			},
		},
	},
}
