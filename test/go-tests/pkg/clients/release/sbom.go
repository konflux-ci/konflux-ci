/* SBOM of type structures matches Pyxis structure

When SBOM components are uploaded to Pyxis, key names have to be modified
to conform to GraphQL naming conventions.
1. Use _ instead of camel case, e.g. camelCase -> camel_case
2. Use _ instead of -, e.g. key-with-dash -> key_with_dash
See https://github.com/konflux-ci/release-service-utils/blob/main/pyxis/upload_sbom.py

*/

package release

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

// Defines a struct Links with fields for various types of links including artifacts, requests, RPM manifests,
// test results, and vulnerabilities. Each field is represented by a corresponding struct type.
type Links struct {
	Artifacts       ArtifactLinks        `json:"artifacts"`
	Requests        RequestLinks         `json:"requests"`
	RpmManifest     RpmManifestLinks     `json:"rpm_manifest"`
	TestResults     TestResultsLinks     `json:"test_results"`
	Vulnerabilities VulnerabilitiesLinks `json:"vulnerabilities"`
}

// Defines a struct ArtifactLinks with a single field Href for storing a link related to an artifact.
type ArtifactLinks struct {
	Href string `json:"href"`
}

// Defines a struct RequestLinks with a single field Href for storing a link related to a request.
type RequestLinks struct {
	Href string `json:"href"`
}

// Defines a struct RpmManifestLinks with a single field Href for storing a link to an RPM manifest.
type RpmManifestLinks struct {
	Href string `json:"href"`
}

// Defines a struct TestResultsLinks with a single field Href for storing a link to test results.
type TestResultsLinks struct {
	Href string `json:"href"`
}

// Defines a struct VulnerabilitiesLinks with a single field Href for storing a link.
type VulnerabilitiesLinks struct {
	Href string `json:"href"`
}

// ContentManifest id of content manifest
type ContentManifest struct {
	ID string `json:"_id"`
}

// Defines a struct FreshnessGrade with fields for creation date, grade, and start date.
type FreshnessGrade struct {
	CreationDate string `json:"creation_date"`
	Grade        string `json:"grade"`
	StartDate    string `json:"start_date"`
}

// ParsedData general details of env
type ParsedData struct {
	Architecture  string   `json:"architecture"`
	DockerVersion string   `json:"docker_version"`
	EnvVariables  []string `json:"env_variables"`
}

// Defines a struct Image with various fields representing image properties and metadata.
// It includes fields for ID, links, architecture, certification status, content manifest,
// content manifest components, creator information, creation date, Docker image digest,
// freshness grades, image ID, last update date, last updated by, object type, and parsed data.
type Image struct {
	ID                string           `json:"_id"`
	Links             Links            `json:"_links"`
	Architecture      string           `json:"architecture"`
	Certified         bool             `json:"certified"`
	ContentManifest   ContentManifest  `json:"content_manifest"`
	CreatedBy         string           `json:"created_by"`
	CreatedOnBehalfOf interface{}      `json:"created_on_behalf_of"`
	CreationDate      string           `json:"creation_date"`
	DockerImageDigest string           `json:"docker_image_digest"`
	FreshnessGrades   []FreshnessGrade `json:"freshness_grades"`
	ImageID           string           `json:"image_id"`
	LastUpdateDate    string           `json:"last_update_date"`
	LastUpdatedBy     string           `json:"last_updated_by"`
	ObjectType        string           `json:"object_type"`
	ParsedData        ParsedData       `json:"parsed_data"`
}

// GetPyxisImageByImageID makes a GET request to stage Pyxis to get an image
// and returns it.
func (r *ReleaseController) GetPyxisImageByImageID(pyxisStageImagesApiEndpoint, imageID string,
	pyxisCertDecoded, pyxisKeyDecoded []byte) ([]byte, error) {

	url := fmt.Sprintf("%s%s", pyxisStageImagesApiEndpoint, imageID)

	// Create a TLS configuration with the key and certificate
	cert, err := tls.X509KeyPair(pyxisCertDecoded, pyxisKeyDecoded)
	if err != nil {
		return nil, fmt.Errorf("error creating TLS certificate and key: %s", err)
	}

	// Create a client with the custom TLS configuration
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
			},
		},
	}

	// Send GET request
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating GET request: %s", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("error sending GET request: %s", err)
	}

	defer response.Body.Close()

	// Read the response body
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %s", err)
	}
	return body, nil
}

// GetPyxisImageIDsFromCreatePyxisImageTaskLogs takes a slice of task logs (as this is what
// TektonController.GetTaskRunLogs returns) and parses them for the imageIDs, returning them
// as a slice.
func (r *ReleaseController) GetPyxisImageIDsFromCreatePyxisImageTaskLogs(logs map[string]string) ([]string, error) {
	var imageIDs []string

	re := regexp.MustCompile(`(?:The image id is: )(.+)`)

	for _, tasklog := range logs {
		for _, matchingString := range re.FindAllString(tasklog, -1) {
			imageIDs = append(imageIDs, re.FindStringSubmatch(matchingString)[1])
		}
	}

	return imageIDs, nil
}
