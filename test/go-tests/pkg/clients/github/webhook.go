package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v44/github"
)

type Webhook struct {
	github.Hook
}

func (g *Github) ListRepoWebhooks(repository string) ([]*github.Hook, error) {
	hooks, _, err := g.client.Repositories.ListHooks(context.Background(), g.organization, repository, &github.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error when listing webhooks: %v", err)
	}
	return hooks, err
}

func (g *Github) CreateWebhook(repository, url string) (int64, error) {
	newWebhook := &github.Hook{
		Events: []string{"push"},
		Config: map[string]interface{}{
			"content_type": "json",
			"insecure_ssl": 0,
			"url":          url,
		},
	}

	hook, _, err := g.client.Repositories.CreateHook(context.Background(), g.organization, repository, newWebhook)
	if err != nil {
		return 0, fmt.Errorf("error when creating a webhook: %v", err)
	}
	return hook.GetID(), err
}

func (g *Github) DeleteWebhook(repository string, ID int64) error {
	_, err := g.client.Repositories.DeleteHook(context.Background(), g.organization, repository, ID)
	if err != nil {
		return fmt.Errorf("error when deleting webhook: %v", err)
	}
	return nil
}
