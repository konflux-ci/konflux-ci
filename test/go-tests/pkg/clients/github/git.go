package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v89/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

func (c *Client) DeleteRef(ctx context.Context, repository, branchName string) error {
	_, err := c.client.Git.DeleteRef(ctx, c.organization, repository, fmt.Sprintf(HEADS, branchName))
	if err != nil {
		return err
	}
	return nil
}

// CreateBranchAtSHA creates a new branch pointing at the given commit.
func (c *Client) CreateBranchAtSHA(repository, sha, newBranchName string) error {
	if sha == "" {
		return fmt.Errorf("sha is required to create branch '%s' in repo '%s'", newBranchName, repository)
	}
	ctx := context.Background()
	newRef := github.CreateRef{
		Ref: fmt.Sprintf(HEADS, newBranchName),
		SHA: sha,
	}
	_, _, err := c.client.Git.CreateRef(ctx, c.organization, repository, newRef)
	if err != nil {
		return fmt.Errorf("error when creating branch '%s' at %s for repo '%s': %+v", newBranchName, sha, repository, err)
	}
	return c.waitForRef(repository, newBranchName)
}

func (c *Client) waitForRef(repository, branchName string) error {
	err := utils.WaitUntilWithInterval(func() (done bool, err error) {
		exist, err := c.ExistsRef(repository, branchName)
		if err != nil {
			return false, err
		}
		if exist {
			return true, nil
		}
		return false, nil
	}, 2*time.Second, 2*time.Minute) //Wait for the branch to actually exist
	if err != nil {
		return fmt.Errorf("error when waiting for ref: %+v", err)
	}
	return nil
}

func (c *Client) ExistsRef(repository, branchName string) (bool, error) {
	_, _, err := c.client.Git.GetRef(context.Background(), c.organization, repository, fmt.Sprintf(HEADS, branchName))
	if err != nil {
		if strings.Contains(err.Error(), "404 Not Found") {
			return false, nil
		} else {
			return false, fmt.Errorf("error when getting the branch '%s' for the repo '%s': %+v", branchName, repository, err)
		}
	}
	return true, nil
}

func (c *Client) UpdateGithubOrg(githubOrg string) {
	c.organization = githubOrg
}
