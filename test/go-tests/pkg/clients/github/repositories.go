package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v90/github"
)

func (c *Client) CreateFile(repository, pathToFile, fileContent, branchName string) (*github.RepositoryContentResponse, error) {
	opts := &github.RepositoryContentFileOptions{
		Message: github.String("e2e test commit message"),
		Content: []byte(fileContent),
		Branch:  github.String(branchName),
	}

	file, _, err := c.client.Repositories.CreateFile(context.Background(), c.organization, repository, pathToFile, opts)
	if err != nil {
		return nil, fmt.Errorf("error when creating file contents: %v", err)
	}

	return file, nil
}
