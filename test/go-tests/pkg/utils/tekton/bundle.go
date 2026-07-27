package tekton

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	remoteimg "github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/tektoncd/cli/pkg/bundle"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"github.com/tektoncd/pipeline/pkg/remote/oci"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

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
