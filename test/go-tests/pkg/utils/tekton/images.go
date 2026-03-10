package tekton

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const quayBaseUrl = "https://quay.io/api/v1"

type ManifestResponse struct {
	Layers []any `json:"layers"`
}

type QuayImageInfo struct {
	ImageRef string
	Layers   []any
}

type Tag struct {
	Digest string `json:"manifest_digest"`
}

type TagResponse struct {
	Tags []Tag `json:"tags"`
}

// getImageInfoFromQuay returns QuayImageInfo for a given image.
func getImageInfoFromQuay(imageRepo, imageTag string) (*QuayImageInfo, error) {

	res, err := http.Get(fmt.Sprintf("%s/repository/%s/tag/?specificTag=%s", quayBaseUrl, imageRepo, imageTag))
	if err != nil {
		return nil, fmt.Errorf("cannot get quay.io/%s:%s image from container registry: %+v", imageRepo, imageTag, err)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read body of a response from quay.io regarding quay.io/%s:%s image %+v", imageRepo, imageTag, err)
	}

	tagResponse := &TagResponse{}
	if err = json.Unmarshal(body, tagResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response from quay.io regarding quay.io/%s:%s image %+v", imageRepo, imageTag, err)
	}

	if len(tagResponse.Tags) < 1 {
		return nil, fmt.Errorf("cannot get manifest digest from quay.io/%s:%s image. response body: %+v", imageRepo, imageTag, string(body))
	}

	quayImageInfo := &QuayImageInfo{}
	quayImageInfo.ImageRef = fmt.Sprintf("quay.io/%s@%s", imageRepo, tagResponse.Tags[0].Digest)

	if strings.Contains(imageTag, ".att") {
		res, err = http.Get(fmt.Sprintf("%s/repository/%s/manifest/%s", quayBaseUrl, imageRepo, tagResponse.Tags[0].Digest))
		if err != nil {
			return nil, fmt.Errorf("cannot get quay.io/%s@%s image from container registry: %+v", imageRepo, quayImageInfo.ImageRef, err)
		}
		body, err = io.ReadAll(res.Body)
		if err != nil {
			return nil, fmt.Errorf("cannot read body of a response from quay.io regarding %s image: %+v", quayImageInfo.ImageRef, err)
		}
		manifestResponse := &ManifestResponse{}
		if err := json.Unmarshal(body, manifestResponse); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response from quay.io regarding %s image: %+v", quayImageInfo.ImageRef, err)
		}

		if len(manifestResponse.Layers) < 1 {
			return nil, fmt.Errorf("cannot get layers from %s image. response body: %+v", quayImageInfo.ImageRef, string(body))
		}
		quayImageInfo.Layers = manifestResponse.Layers
	}

	return quayImageInfo, nil
}
