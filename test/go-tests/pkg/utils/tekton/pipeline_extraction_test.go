package tekton

import (
	"testing"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestPipelineExtraction(t *testing.T) {
	var defaultBundleRef string
	var err error
	if defaultBundleRef, err = GetDefaultPipelineBundleRef(constants.BuildPipelineConfigConfigMapYamlURL, "docker-build"); err != nil {
		assert.Error(t, err, "failed to parse bundle ref")
		panic(err)
	}
	assert.Contains(t, defaultBundleRef, "pipeline-docker-build", "failed to retrieve bundle ref")
}
