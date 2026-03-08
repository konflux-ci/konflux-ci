package forgejo

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v2"
	"github.com/onsi/gomega"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

// CreateBranch creates a new branch in a Forgejo repository
// projectID should be in format "owner/repo"
func (fc *ForgejoClient) CreateBranch(projectID, newBranchName, baseBranch string) error {
	owner, repo := splitProjectID(projectID)

	opts := forgejo.CreateBranchOption{
		BranchName:    newBranchName,
		OldBranchName: baseBranch,
	}

	_, resp, err := fc.client.CreateBranch(owner, repo, opts)
	if err != nil {
		return fmt.Errorf("failed to create branch %s in project %s: %w", newBranchName, projectID, err)
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected status code when creating branch %s: %d", newBranchName, resp.StatusCode)
	}

	// Wait for the branch to actually exist
	gomega.Eventually(func(gm gomega.Gomega) {
		exists, err := fc.ExistsBranch(projectID, newBranchName)
		gm.Expect(err).NotTo(gomega.HaveOccurred())
		gm.Expect(exists).To(gomega.BeTrue())
	}, 2*time.Minute, 2*time.Second).Should(gomega.Succeed())

	return nil
}

// ExistsBranch checks if a branch exists in a Forgejo repository
func (fc *ForgejoClient) ExistsBranch(projectID, branchName string) (bool, error) {
	owner, repo := splitProjectID(projectID)

	_, resp, err := fc.client.GetRepoBranch(owner, repo, branchName)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// DeleteBranch deletes a branch from a Forgejo repository
func (fc *ForgejoClient) DeleteBranch(projectID, branchName string) error {
	owner, repo := splitProjectID(projectID)

	_, _, err := fc.client.DeleteRepoBranch(owner, repo, branchName)
	if err != nil {
		return fmt.Errorf("failed to delete branch %s: %w", branchName, err)
	}

	fmt.Printf("Deleted branch: %s\n", branchName)
	return nil
}

// GetPullRequests returns a list of all open pull requests in a repository
func (fc *ForgejoClient) GetPullRequests(projectID string) ([]*forgejo.PullRequest, error) {
	owner, repo := splitProjectID(projectID)

	opts := forgejo.ListPullRequestsOptions{
		State: forgejo.StateOpen,
		ListOptions: forgejo.ListOptions{
			Page:     1,
			PageSize: 100,
		},
	}

	prs, _, err := fc.client.ListRepoPullRequests(owner, repo, opts)
	if err != nil {
		return nil, err
	}

	return prs, nil
}

// CreatePullRequest creates a new pull request
func (fc *ForgejoClient) CreatePullRequest(projectID, title, body, head, base string) (*forgejo.PullRequest, error) {
	owner, repo := splitProjectID(projectID)

	opts := forgejo.CreatePullRequestOption{
		Title: title,
		Body:  body,
		Head:  head,
		Base:  base,
	}

	pr, _, err := fc.client.CreatePullRequest(owner, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create pull request: %w", err)
	}

	return pr, nil
}

// MergePullRequest merges a pull request
func (fc *ForgejoClient) MergePullRequest(projectID string, prNumber int64) (*forgejo.PullRequest, error) {
	owner, repo := splitProjectID(projectID)

	opts := forgejo.MergePullRequestOption{
		Style: forgejo.MergeStyleMerge,
	}

	success, _, err := fc.client.MergePullRequest(owner, repo, prNumber, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to merge pull request: %w", err)
	}
	if !success {
		return nil, fmt.Errorf("merge was not successful")
	}

	// Get the updated PR to get merge commit SHA
	pr, _, err := fc.client.GetPullRequest(owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get merged pull request: %w", err)
	}

	return pr, nil
}

// ClosePullRequest closes a pull request without merging
func (fc *ForgejoClient) ClosePullRequest(projectID string, prNumber int64) error {
	owner, repo := splitProjectID(projectID)

	state := forgejo.StateClosed
	opts := forgejo.EditPullRequestOption{
		State: &state,
	}

	_, _, err := fc.client.EditPullRequest(owner, repo, prNumber, opts)
	if err != nil {
		return fmt.Errorf("failed to close pull request %d: %w", prNumber, err)
	}

	return nil
}

// CreateFile creates a new file in a repository
func (fc *ForgejoClient) CreateFile(projectID, pathToFile, content, branchName string) (*forgejo.FileResponse, error) {
	owner, repo := splitProjectID(projectID)

	opts := forgejo.CreateFileOptions{
		FileOptions: forgejo.FileOptions{
			Message:    "e2e test commit message",
			BranchName: branchName,
		},
		Content: content,
	}

	fileResp, _, err := fc.client.CreateFile(owner, repo, pathToFile, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create file %s: %w", pathToFile, err)
	}

	return fileResp, nil
}

// GetFile gets the content of a file from a repository
func (fc *ForgejoClient) GetFile(projectID, pathToFile, branchName string) (string, *forgejo.ContentsResponse, error) {
	owner, repo := splitProjectID(projectID)

	content, _, err := fc.client.GetContents(owner, repo, branchName, pathToFile)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get file %s: %w", pathToFile, err)
	}

	// Content is base64 encoded
	if content.Content == nil {
		return "", content, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(*content.Content)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode file content: %w", err)
	}

	return string(decoded), content, nil
}

// DeleteWebhooks deletes webhooks matching the cluster app domain
func (fc *ForgejoClient) DeleteWebhooks(projectID, clusterAppDomain string) error {
	if clusterAppDomain == "" {
		return fmt.Errorf("clusterAppDomain is empty")
	}

	owner, repo := splitProjectID(projectID)

	hooks, _, err := fc.client.ListRepoHooks(owner, repo, forgejo.ListHooksOptions{})
	if err != nil {
		return fmt.Errorf("failed to list webhooks: %w", err)
	}

	for _, hook := range hooks {
		if hook.Config != nil {
			if url, ok := hook.Config["url"]; ok {
				if strings.Contains(url, clusterAppDomain) {
					_, err := fc.client.DeleteRepoHook(owner, repo, hook.ID)
					if err != nil {
						return fmt.Errorf("failed to delete webhook (ID: %d): %w", hook.ID, err)
					}
					fmt.Printf("Deleted webhook with URL: %s\n", url)
					break
				}
			}
		}
	}

	return nil
}

// ForkRepository forks a repository
func (fc *ForgejoClient) ForkRepository(sourceProjectID, targetProjectID string) (*forgejo.Repository, error) {
	sourceOwner, sourceRepo := splitProjectID(sourceProjectID)
	targetOwner, targetRepo := splitProjectID(targetProjectID)

	opts := forgejo.CreateForkOption{
		Organization: &targetOwner,
		Name:         &targetRepo,
	}

	var forkedRepo *forgejo.Repository

	err := utils.WaitUntilWithInterval(func() (done bool, err error) {
		forkedRepo, _, err = fc.client.CreateFork(sourceOwner, sourceRepo, opts)
		if err != nil {
			fmt.Printf("Failed to fork %s, trying again: %v\n", sourceProjectID, err)
			return false, nil
		}
		return true, nil
	}, time.Second*10, time.Minute*5)

	if err != nil {
		return nil, fmt.Errorf("error forking project %s to %s: %w", sourceProjectID, targetProjectID, err)
	}

	return forkedRepo, nil
}

// DeleteRepository deletes a repository if it exists
func (fc *ForgejoClient) DeleteRepository(projectID string) error {
	owner, repo := splitProjectID(projectID)

	_, err := fc.client.DeleteRepo(owner, repo)
	if err != nil {
		// Check if repo doesn't exist (already deleted)
		if strings.Contains(err.Error(), "404") {
			return nil
		}
		return fmt.Errorf("failed to delete repository %s: %w", projectID, err)
	}

	return nil
}

// DeleteRepositoryIfExists deletes a repository if it exists, no error if not found
func (fc *ForgejoClient) DeleteRepositoryIfExists(projectID string) error {
	owner, repo := splitProjectID(projectID)

	// Check if repo exists first
	_, resp, err := fc.client.GetRepo(owner, repo)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil
		}
		return fmt.Errorf("error checking if repository exists: %w", err)
	}

	return fc.DeleteRepository(projectID)
}

// GetCommitStatusConclusion waits for and returns the commit status conclusion
func (fc *ForgejoClient) GetCommitStatusConclusion(statusName, projectID, commitSHA string, prNumber int64) string {
	owner, repo := splitProjectID(projectID)
	var matchingStatus *forgejo.CombinedStatus
	timeout := time.Minute * 10

	gomega.Eventually(func() bool {
		combinedStatus, _, err := fc.client.GetCombinedStatus(owner, repo, commitSHA)
		if err != nil {
			fmt.Printf("got error when listing commit statuses: %+v\n", err)
			return false
		}
		for _, status := range combinedStatus.Statuses {
			if strings.Contains(status.Context, statusName) {
				matchingStatus = combinedStatus
				return true
			}
		}
		return false
	}, timeout, time.Second*2).Should(gomega.BeTrue(), fmt.Sprintf("timed out waiting for the PaC commit status to appear for %s", commitSHA))

	gomega.Eventually(func() bool {
		combinedStatus, _, err := fc.client.GetCombinedStatus(owner, repo, commitSHA)
		if err != nil {
			fmt.Printf("got error when checking commit status: %+v\n", err)
			return false
		}
		for _, status := range combinedStatus.Statuses {
			if strings.Contains(status.Context, statusName) {
				currentState := status.State
				// Forgejo only has StatusPending, no StatusRunning
				if currentState != forgejo.StatusPending {
					matchingStatus = combinedStatus
					return true
				}
				fmt.Printf("expecting commit status to be completed, got: %s\n", currentState)
				return false
			}
		}
		return false
	}, timeout, time.Second*2).Should(gomega.BeTrue(), fmt.Sprintf("timed out waiting for the PaC commit status to be completed for %s", commitSHA))

	// Return the state as a string
	for _, status := range matchingStatus.Statuses {
		if strings.Contains(status.Context, statusName) {
			return string(status.State)
		}
	}
	return ""
}

// splitProjectID splits a projectID in format "owner/repo" into owner and repo
func splitProjectID(projectID string) (string, string) {
	parts := strings.SplitN(projectID, "/", 2)
	if len(parts) != 2 {
		return projectID, ""
	}
	return parts[0], parts[1]
}
