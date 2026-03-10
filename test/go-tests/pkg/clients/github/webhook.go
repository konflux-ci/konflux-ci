package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v44/github"
)

type Webhook struct {
	github.Hook
}

func (c *Client) ListRepoWebhooks(repository string) ([]*github.Hook, error) {
	hooks, _, err := c.client.Repositories.ListHooks(context.Background(), c.organization, repository, &github.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error when listing webhooks: %v", err)
	}
	return hooks, err
}

func (c *Client) CreateWebhook(repository, url string) (int64, error) {
	newWebhook := &github.Hook{
		Events: []string{"push"},
		Config: map[string]interface{}{
			"content_type": "json",
			"insecure_ssl": 0,
			"url":          url,
		},
	}

	hook, _, err := c.client.Repositories.CreateHook(context.Background(), c.organization, repository, newWebhook)
	if err != nil {
		return 0, fmt.Errorf("error when creating a webhook: %v", err)
	}
	return hook.GetID(), err
}

func (c *Client) DeleteWebhook(repository string, ID int64) error {
	_, err := c.client.Repositories.DeleteHook(context.Background(), c.organization, repository, ID)
	if err != nil {
		return fmt.Errorf("error when deleting webhook: %v", err)
	}
	return nil
}
