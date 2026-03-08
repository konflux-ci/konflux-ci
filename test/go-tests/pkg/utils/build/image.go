package build

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/oc/pkg/cli/image/extract"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	imageInfo "github.com/openshift/oc/pkg/cli/image/info"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func ExtractImage(image string) (string, error) {
	dockerImageRef, err := reference.Parse(image)
	if err != nil {
		return "", fmt.Errorf("cannot parse docker pull spec (image) %s, error: %+v", image, err)
	}
	tmpDir, err := os.MkdirTemp(os.TempDir(), "eimage")
	if err != nil {
		return "", fmt.Errorf("error when creating a temp directory for extracting files: %+v", err)
	}
	ginkgo.GinkgoWriter.Printf("extracting contents of container image %s to dir: %s\n", image, tmpDir)
	eMapping := extract.Mapping{
		ImageRef: imagesource.TypedImageReference{Type: "docker", Ref: dockerImageRef},
		To:       tmpDir,
	}
	e := extract.NewExtractOptions(genericclioptions.IOStreams{Out: os.Stdout, ErrOut: os.Stderr})
	e.Mappings = []extract.Mapping{eMapping}

	if err := e.Run(); err != nil {
		return "", fmt.Errorf("error: %+v", err)
	}
	return tmpDir, nil
}

func ImageFromPipelineRun(pipelineRun *pipeline.PipelineRun) (*imageInfo.Image, error) {
	var outputImage string
	for _, parameter := range pipelineRun.Spec.Params {
		if parameter.Name == "output-image" {
			outputImage = parameter.Value.StringVal
		}
	}
	if outputImage == "" {
		return nil, fmt.Errorf("output-image in PipelineRun not found")
	}

	dockerImageRef, err := reference.Parse(outputImage)
	if err != nil {
		return nil, fmt.Errorf("error parsing outputImage to dockerImageRef, %w", err)
	}

	imageRetriever := imageInfo.ImageRetriever{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	image, err := imageRetriever.Image(ctx, imagesource.TypedImageReference{Type: "docker", Ref: dockerImageRef})
	if err != nil {
		return nil, fmt.Errorf("error getting image from imageRetriver, %w", err)
	}
	return image, nil
}

// FetchImageConfig fetches image config from remote registry.
// It uses the registry authentication credentials stored in default place ~/.docker/config.json
func FetchImageConfig(imagePullspec string) (*v1.ConfigFile, error) {
	wrapErr := func(err error) error {
		return fmt.Errorf("error while fetching image config %s: %w", imagePullspec, err)
	}
	ref, err := name.ParseReference(imagePullspec)
	if err != nil {
		return nil, wrapErr(err)
	}
	// Fetch the manifest using default credentials.
	descriptor, err := remote.Get(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return nil, wrapErr(err)
	}

	image, err := descriptor.Image()
	if err != nil {
		return nil, wrapErr(err)
	}
	configFile, err := image.ConfigFile()
	if err != nil {
		return nil, wrapErr(err)
	}
	return configFile, nil
}

// FetchImageDigest fetches image manifest digest.
// Digest is returned as a hex string without sha256: prefix.
func FetchImageDigest(imagePullspec string) (string, error) {
	ref, err := name.ParseReference(imagePullspec)
	if err != nil {
		return "", err
	}
	// Fetch the manifest using default credentials.
	descriptor, err := remote.Get(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return "", err
	}
	return descriptor.Digest.Hex, nil
}
