package gitlab

import (
	"net/http"

	gitlabClient "github.com/xanzy/go-gitlab"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

const (
	HEADS = "refs/heads/%s"
)

type Client struct {
	client  *gitlabClient.Client
	groupID string
}

func NewClient(accessToken, baseUrl, groupID string) (*Client, error) {
	var err error
	var glc = &Client{groupID: groupID}

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
func (c *Client) GetClient() *gitlabClient.Client {
	return c.client
}
