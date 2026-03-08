package oras

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/openshift/library-go/pkg/image/reference"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// PullArtifacts pulls artifacts from the given imagePullSpec.
// Pulled artifacts will be stored in a local directory, whose path is returned.
func PullArtifacts(imagePullSpec string) (string, error) {
	storePath, err := os.MkdirTemp("", "pulled-artifacts")
	if err != nil {
		return "", err
	}
	fs, err := file.New(storePath)
	if err != nil {
		return "", err
	}
	defer fs.Close()

	imageRef, err := reference.Parse(imagePullSpec)
	if err != nil {
		return "", fmt.Errorf("cannot parse %s: %w", imagePullSpec, err)
	}

	repo, err := remote.NewRepository(imagePullSpec)
	if err != nil {
		return "", fmt.Errorf("cannot get repository from %s: %w", imagePullSpec, err)
	}
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: auth.StaticCredential(imageRef.Registry, auth.Credential{
			AccessToken: os.Getenv("QUAY_TOKEN"),
		}),
	}

	ctx := context.Background()
	srcRef := imageRef.ID
	if srcRef == "" {
		srcRef = imageRef.Tag
	}
	dstRef := srcRef

	opts := oras.DefaultCopyOptions
	// Fetch only the artifacts directly referenced in the Image Manifest.
	opts.FindSuccessors = noSuccessors

	if _, err := oras.Copy(ctx, repo, srcRef, fs, dstRef, opts); err != nil {
		return "", fmt.Errorf("copying %s: %w", imagePullSpec, err)
	}

	return storePath, nil
}

// noSuccessors returns the nodes directly pointed by the current node. By default oras will follow
// the "subject" of an Image Manifest. For artifacts that are attached to an image, this causes the
// image itself to also be pulled. Since oras doesn't provide a public function for fetching only
// the direct nodes we must roll our own. Ironically, the oras CLI also uses a custom behavior:
// https://github.com/oras-project/oras/blob/00a19d20644fe57d051d3b871579167dc2ff98e5/internal/graph/graph.go#L61
// This function only supports a very small set of subsets which is sufficient for e2e-tests.
func noSuccessors(ctx context.Context, fetcher content.Fetcher, node ocispec.Descriptor) ([]ocispec.Descriptor, error) {

	switch node.MediaType {
	case ocispec.MediaTypeImageManifest:
		content, err := content.FetchAll(ctx, fetcher, node)
		if err != nil {
			return nil, err
		}
		var manifest ocispec.Manifest
		if err := json.Unmarshal(content, &manifest); err != nil {
			return nil, err
		}
		return manifest.Layers, nil
	default:
		return nil, nil
	}
}
