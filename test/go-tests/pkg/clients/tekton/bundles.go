package tekton

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/types"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils/tekton"
)

type Bundles struct {
	DockerBuildBundle                   string
	DockerBuildMultiPlatformOCITABundle string
	DockerBuildOCITABundle              string
	DockerBuildOCITAMinBundle           string
	FBCBuilderBundle                    string
}

// NewBundles returns new Bundles.
func (t *TektonController) NewBundles() (*Bundles, error) {
	namespacedName := types.NamespacedName{
		Name:      "build-pipeline-config",
		Namespace: "build-service",
	}
	bundles := &Bundles{}
	configMap := &corev1.ConfigMap{}
	err := t.KubeRest().Get(context.Background(), namespacedName, configMap)
	if err != nil {
		return nil, err
	}

	bpc := &tekton.BuildPipelineConfig{}
	if err = yaml.Unmarshal([]byte(configMap.Data["config.yaml"]), bpc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal build pipeline config: %v", err)
	}

	for i := range bpc.Pipelines {
		pipeline := bpc.Pipelines[i]
		switch pipeline.Name {
		case "docker-build":
			bundles.DockerBuildBundle = pipeline.Bundle
		case "docker-build-multi-platform-oci-ta":
			bundles.DockerBuildMultiPlatformOCITABundle = pipeline.Bundle
		case "docker-build-oci-ta":
			bundles.DockerBuildOCITABundle = pipeline.Bundle
		case "docker-build-oci-ta-min":
			bundles.DockerBuildOCITAMinBundle = pipeline.Bundle
		case "fbc-builder":
			bundles.FBCBuilderBundle = pipeline.Bundle
		}
	}
	return bundles, nil
}
