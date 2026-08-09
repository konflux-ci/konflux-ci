package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v89/github"
)

type Webhook struct {
	github.Hook
}

func (c *Client) ListRepoWebhooks(ctx context.Context, repository string) ([]*github.Hook, error) {
	hooks, _, err := c.client.Repositories.ListHooks(ctx, c.organization, repository, &github.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error when listing webhooks: %v", err)
	}
	return hooks, err
}

func (c *Client) DeleteWebhook(ctx context.Context, repository string, ID int64) error {
	_, err := c.client.Repositories.DeleteHook(ctx, c.organization, repository, ID)
	if err != nil {
		return fmt.Errorf("error when deleting webhook: %v", err)
	}
	return nil
}
