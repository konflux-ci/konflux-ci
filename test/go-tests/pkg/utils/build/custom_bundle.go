package build

import (
	"fmt"
	"os"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
)

func GetBuildPipelineBundleAnnotation(buildPipelineName constants.BuildPipelineType) map[string]string {
	var bundleVersion string

	switch buildPipelineName {
	case constants.DockerBuild:
		bundleVersion = os.Getenv(constants.CUSTOM_DOCKER_BUILD_PIPELINE_BUNDLE_ENV)
	case constants.DockerBuildOciTA:
		bundleVersion = os.Getenv(constants.CUSTOM_DOCKER_BUILD_OCI_TA_PIPELINE_BUNDLE_ENV)
	case constants.DockerBuildOciTAMin:
		bundleVersion = os.Getenv(constants.CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE_ENV)
	case constants.DockerBuildMultiPlatformOciTa:
		bundleVersion = os.Getenv(constants.CUSTOM_DOCKER_BUILD_OCI_MULTI_PLATFORM_TA_PIPELINE_BUNDLE_ENV)
	case constants.FbcBuilder:
		bundleVersion = os.Getenv(constants.CUSTOM_FBC_BUILDER_PIPELINE_BUNDLE_ENV)
	}
	if bundleVersion == "" {
		bundleVersion = "latest"
	}

	return map[string]string{
		"build.appstudio.openshift.io/pipeline": fmt.Sprintf(`{"name":"%s", "bundle": "%s"}`, buildPipelineName, bundleVersion),
	}
}
