package config

import (
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
)

// testrepoURL is the canonical URL for the test repository. It must always
// point to konflux-ci/testrepo so that the pinned SHA resolves against the
// correct fork (where pipeline timeout fixes are applied).
const testrepoURL = "https://github.com/konflux-ci/testrepo"

var UpstreamAppSpecs = []ApplicationSpec{
	{
		Name:            "Test local instance of konflux-ci - docker-build-oci-ta-min pipeline",
		ApplicationName: "konflux-ci-upstream-docker-build-oci-ta-min",
		Skip:            false,
		ComponentSpec: ComponentSpec{
			Name:                       "konflux-ci-upstream",
			GitSourceUrl:               testrepoURL,
			GitSourceRevision:          "878eb2976b97946f577a8dbb0cc391d5370efbbb",
			GitSourceDefaultBranchName: "main",
			DockerFilePath:             "Dockerfile",
			BuildPipelineType:          constants.DockerBuildOciTAMin,
			IntegrationTestScenario: IntegrationTestScenarioSpec{
				GitURL:      testrepoURL,
				GitRevision: "878eb2976b97946f577a8dbb0cc391d5370efbbb",
				TestPath:    "integration-tests/testrepo-integration.yaml",
			},
		},
	},
}
