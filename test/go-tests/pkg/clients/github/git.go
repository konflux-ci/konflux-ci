package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v84/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

func (c *Client) DeleteRef(repository, branchName string) error {
	_, err := c.client.Git.DeleteRef(context.Background(), c.organization, repository, fmt.Sprintf(HEADS, branchName))
	if err != nil {
		return err
	}
	return nil
}

// CreateRef creates a new ref (GitHub branch) in a specified GitHub repository,
// that will be based on the commit specified with sha. If sha is not specified
// the latest commit from base branch will be used.
func (c *Client) CreateRef(repository, baseBranchName, sha, newBranchName string) error {
	ctx := context.Background()
	ref, _, err := c.client.Git.GetRef(ctx, c.organization, repository, fmt.Sprintf(HEADS, baseBranchName))
	if err != nil {
		return fmt.Errorf("error when getting the base branch name '%s' for the repo '%s': %+v", baseBranchName, repository, err)
	}

	ref.Ref = github.String(fmt.Sprintf(HEADS, newBranchName))

	if sha != "" {
		ref.Object.SHA = &sha
	}

	_, _, err = c.client.Git.CreateRef(ctx, c.organization, repository, ref)
	if err != nil {
		return fmt.Errorf("error when creating a new branch '%s' for the repo '%s': %+v", newBranchName, repository, err)
	}
	err = utils.WaitUntilWithInterval(func() (done bool, err error) {
		exist, err := c.ExistsRef(repository, newBranchName)
		if err != nil {
			return false, err
		}
		if exist && err == nil {
			return exist, err
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

// EnsureBranchExists checks if a branch exists in the repository and creates one from
// fallback branch if not.
func (c *Client) EnsureBranchExists(repository, branchName, fallbackBranch string) error {
	exists, err := c.ExistsRef(repository, branchName)
	if err != nil {
		return fmt.Errorf("error checking if branch '%s' exists: %w", branchName, err)
	}
	if exists {
		return nil
	}
	// Branch doesn't exist, create it from fallback branch
	return c.CreateRef(repository, fallbackBranch, "", branchName)
}
