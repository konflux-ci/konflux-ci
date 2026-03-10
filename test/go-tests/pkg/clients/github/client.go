package github

import (
	"context"
	"time"

	"github.com/gofri/go-github-ratelimit/github_ratelimit"
	"github.com/google/go-github/v44/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	"golang.org/x/oauth2"
)

const (
	HEADS = "heads/%s"
)

type Client struct {
	client       *github.Client
	organization string
}

func NewClient(token, organization string) (*Client, error) {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	// https://docs.github.com/en/rest/guides/best-practices-for-integrators?apiVersion=2022-11-28#dealing-with-secondary-rate-limits
	rateLimiter, err := github_ratelimit.NewRateLimitWaiterClient(tc.Transport, github_ratelimit.WithSingleSleepLimit(time.Minute, nil))
	if err != nil {
		return &Client{}, err
	}

	// Wrap with retry transport so all API calls automatically retry on 5xx
	rateLimiter.Transport = utils.NewRetryTransport(rateLimiter.Transport)

	client := github.NewClient(rateLimiter)
	githubClient := &Client{
		client:       client,
		organization: organization,
	}

	return githubClient, nil
}
