package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

// testrepoRevision returns the git revision for https://github.com/<org>/testrepo in conformance.
// When TESTREPO_REVISION is set (e.g. from test/e2e/testrepo-revision in CI), that value is used;
// otherwise developers default to branch main.
func testrepoRevision() string {
	if v := strings.TrimSpace(os.Getenv(constants.TESTREPO_REVISION_ENV)); v != "" {
		return v
	}
	return "main"
}

// UpstreamAppSpecs returns the conformance application scenarios for the upstream Konflux test.
func UpstreamAppSpecs() []ApplicationSpec {
	rev := testrepoRevision()
	return []ApplicationSpec{
		{
			Name:            "Test local instance of konflux-ci - docker-build-oci-ta-min pipeline",
			ApplicationName: "konflux-ci-upstream-docker-build-oci-ta-min",
			Skip:            false,
			ComponentSpec: ComponentSpec{
				Name:                       "konflux-ci-upstream",
				GitSourceUrl:               fmt.Sprintf("https://github.com/%s/%s", utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe"), "testrepo"),
				GitSourceRevision:          rev,
				GitSourceDefaultBranchName: "main",
				DockerFilePath:             "Dockerfile",
				BuildPipelineType:          constants.DockerBuildOciTAMin,
				IntegrationTestScenario: IntegrationTestScenarioSpec{
					GitURL:      fmt.Sprintf("https://github.com/%s/%s", utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe"), "testrepo"),
					GitRevision: rev,
					TestPath:    "integration-tests/testrepo-integration.yaml",
				},
			},
		},
	}
}
