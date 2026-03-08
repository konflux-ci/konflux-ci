package gitlab

import (
	"net/http"

	gitlabClient "github.com/xanzy/go-gitlab"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

const (
	HEADS = "refs/heads/%s"
)

type GitlabClient struct {
	client  *gitlabClient.Client
	groupID string
}

func NewGitlabClient(accessToken, baseUrl, groupID string) (*GitlabClient, error) {
	var err error
	var glc = &GitlabClient{groupID: groupID}

	httpClient := &http.Client{
		Transport: utils.NewRetryTransport(http.DefaultTransport),
	}

	glc.client, err = gitlabClient.NewClient(accessToken,
		gitlabClient.WithBaseURL(baseUrl),
		gitlabClient.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, err
	}
	return glc, nil
}

// GetClient returns the underlying gitlab client
func (gc *GitlabClient) GetClient() *gitlabClient.Client {
	return gc.client
}
