package ociregistry

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// This client is meant for direct interactions with the OCI Registry HTTP V2 API.
// Docs: https://github.com/opencontainers/distribution-spec/blob/main/spec.md#api
type OciRegistryV2Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewOciRegistryV2Client(baseURL string) *OciRegistryV2Client {
	if !strings.HasPrefix(baseURL, "http") {
		baseURL = "https://" + baseURL
	}

	return &OciRegistryV2Client{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

func (c *OciRegistryV2Client) makeRequest(url, method string, body io.Reader) ([]byte, error) {
	requestURL := fmt.Sprintf("%s/v2/%s", c.baseURL, url)

	req, err := http.NewRequest(method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", response.StatusCode, string(responseBody))
	}

	return responseBody, nil
}

// Fetches a blob using the GET /v2/<name>/blobs/<digest> endpoint
func (c *OciRegistryV2Client) FetchBlob(organization, repository, digest string) ([]byte, error) {
	blobURL := fmt.Sprintf("%s/%s/blobs/%s", organization, repository, digest)

	responseBody, err := c.makeRequest(blobURL, "GET", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blob: %w", err)
	}

	return responseBody, nil
}
