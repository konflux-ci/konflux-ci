package tekton

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	remoteimg "github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/tektoncd/cli/pkg/bundle"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"github.com/tektoncd/pipeline/pkg/remote/oci"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
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

// ExtractTektonObjectFromBundle extracts specified Tekton object from specified bundle reference
func ExtractTektonObjectFromBundle(bundleRef, kind string, name constants.BuildPipelineType) (runtime.Object, error) {
	var obj runtime.Object
	var err error

	resolver := oci.NewResolver(bundleRef, authn.DefaultKeychain)
	if obj, _, err = resolver.Get(context.Background(), kind, string(name)); err != nil {
		return nil, fmt.Errorf("failed to fetch the tekton object %s with name %s: %v", kind, name, err)
	}
	return obj, nil
}

// BuildAndPushTektonBundle builds a Tekton bundle from YAML and pushes to remote container registry
func BuildAndPushTektonBundle(YamlContent []byte, ref name.Reference, remoteOption remoteimg.Option) error {
	img, err := bundle.BuildTektonBundle([]string{string(YamlContent)}, map[string]string{}, map[string]string{}, time.Now(), os.Stdout)
	if err != nil {
		return fmt.Errorf("error when building a bundle %s: %v", ref.String(), err)
	}

	outDigest, err := bundle.Write(img, ref, remoteOption)
	if err != nil {
		return fmt.Errorf("error when pushing a bundle %s to a container image registry repo: %v", ref.String(), err)
	}
	klog.Infof("image digest for a new tekton bundle %s: %+v", ref.String(), outDigest)

	return nil
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
