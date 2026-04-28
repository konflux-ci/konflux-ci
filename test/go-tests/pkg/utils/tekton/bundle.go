package tekton

import (
	"fmt"
	"io"
	"net/http"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

type BuildPipelineConfig struct {
	DefaultPipelineName string        `json:"default-pipeline-name"`
	Pipelines           []PipelineRef `json:"pipelines"`
}

type PipelineRef struct {
	Name   string `json:"name"`
	Bundle string `json:"bundle"`
}

// GetBundleRef returns the bundle reference from a pipelineRef
func GetBundleRef(pipelineRef *pipeline.PipelineRef) string {
	_, bundleRef := GetPipelineNameAndBundleRef(pipelineRef)
	return bundleRef
}

// GetDefaultPipelineBundleRef gets the specific Tekton pipeline bundle reference from a Build pipeline config
// ConfigMap (in a YAML format) from a URL specified in the parameter
func GetDefaultPipelineBundleRef(buildPipelineConfigConfigMapYamlURL string, name constants.BuildPipelineType) (string, error) {
	request, err := http.NewRequest("GET", buildPipelineConfigConfigMapYamlURL, nil)
	if err != nil {
		return "", fmt.Errorf("error creating GET request: %s", err)
	}

	client := &http.Client{}
	res, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("failed to get a build pipeline selector from url %s: %v", buildPipelineConfigConfigMapYamlURL, err)
	}

	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read the body response of a build pipeline selector: %v", err)
	}

	configMap := &corev1.ConfigMap{}
	if err = yaml.Unmarshal(body, configMap); err != nil {
		return "", fmt.Errorf("failed to unmarshal build pipeline config config map: %v", err)
	}
	bpc := &BuildPipelineConfig{}
	if err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), bpc); err != nil {
		return "", fmt.Errorf("failed to unmarshal build pipeline config: %v", err)
	}

	for i := range bpc.Pipelines {
		pipeline := bpc.Pipelines[i]
		if pipeline.Name == string(name) {
			return pipeline.Bundle, nil
		}
	}

	return "", fmt.Errorf("could not find %s pipeline bundle in build pipeline config fetched from %s", name, buildPipelineConfigConfigMapYamlURL)
}
