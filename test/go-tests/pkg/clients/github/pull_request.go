package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v90/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

const (
	// mergeInProgressPollTimeout is how long to poll for merge completion
	// after receiving a 405 "Merge already in progress" response.
	// This must be shorter than the caller's timeout (mergePRTimeout = 1 min
	// in conformance tests) so that Eventually can observe the timeout.
	mergeInProgressPollTimeout = 45 * time.Second

	// defaultPollInterval is the default interval between GetPullRequest
	// calls when polling for merge completion. Override Client.pollInterval
	// for faster tests.
	defaultPollInterval = 5 * time.Second

	// maxConsecutivePollErrors is the number of consecutive GetPullRequest
	// failures inside waitForMerge before giving up early.
	maxConsecutivePollErrors = 3
)

func (c *Client) GetPullRequest(repository string, id int) (*github.PullRequest, error) {
	pr, _, err := c.client.PullRequests.Get(context.Background(), c.organization, repository, id)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (c *Client) ListPullRequests(repository string) ([]*github.PullRequest, error) {
	prs, _, err := c.client.PullRequests.List(context.Background(), c.organization, repository, &github.PullRequestListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error when listing pull requests for the repo %s: %v", repository, err)
	}

	return prs, nil
}

func (c *Client) MergePullRequest(repository string, prNumber int) (*github.PullRequestMergeResult, error) {
	mergeResult, _, err := c.client.PullRequests.Merge(context.Background(), c.organization, repository, prNumber, "", &github.PullRequestOptions{})
	if err != nil {
		// Check if the PR is already merged (fast path) or the merge is in progress (poll)
		if isPRAlreadyMerged(err) {
			fmt.Printf("[github] PR #%d in %s: already merged, fetching merge result\n", prNumber, repository)
			return c.getMergeResultFromPR(repository, prNumber)
		}
		if isMergeInProgress(err) {
			fmt.Printf("[github] PR #%d in %s: merge already in progress, polling for completion\n", prNumber, repository)
			return c.waitForMerge(repository, prNumber)
		}

		mergeErr := fmt.Errorf("error when merging pull request number %d for the repo %s: %v", prNumber, repository, err)
		// If the head branch is out of date (409), trigger a branch update so the next retry can succeed
		if strings.Contains(err.Error(), "409") {
			fmt.Printf("[github] PR #%d in %s: head branch out of date, triggering branch update\n", prNumber, repository)
			if updateErr := c.UpdatePullRequestBranch(repository, prNumber); updateErr != nil {
				fmt.Printf("[github] failed to update PR #%d branch: %v\n", prNumber, updateErr)
			}
		}
		return nil, mergeErr
	}

	return mergeResult, nil
}

// isMergeInProgress returns true when GitHub responds with HTTP 405 and a
// message indicating that a merge is already being processed.
// It matches specifically on "merge already in progress" to avoid false
// positives from other 405 responses (e.g., "already been merged",
// "Base branch was modified").
func isMergeInProgress(err error) bool {
	var ghErr *github.ErrorResponse
	if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == 405 {
		return strings.Contains(strings.ToLower(ghErr.Message), "merge already in progress")
	}
	return false
}

// isPRAlreadyMerged returns true when GitHub responds with HTTP 405 and a
// message indicating that the pull request has already been merged.
// Only matches the explicit "already been merged" wording; generic
// "not mergeable" responses (conflicts, failing checks) are excluded.
func isPRAlreadyMerged(err error) bool {
	var ghErr *github.ErrorResponse
	if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == 405 {
		return strings.Contains(strings.ToLower(ghErr.Message), "already been merged")
	}
	return false
}

// waitForMerge polls the PR until it transitions to merged state or times out.
// It fails fast on consecutive API errors and when the PR is closed without
// being merged.
func (c *Client) waitForMerge(repository string, prNumber int) (*github.PullRequestMergeResult, error) {
	var pr *github.PullRequest
	var lastErr error
	consecutiveErrors := 0

	interval := c.pollInterval
	if interval == 0 {
		interval = defaultPollInterval
	}
	timeout := c.pollTimeout
	if timeout == 0 {
		timeout = mergeInProgressPollTimeout
	}

	err := utils.WaitUntilWithInterval(func() (bool, error) {
		var getErr error
		pr, getErr = c.GetPullRequest(repository, prNumber)
		if getErr != nil {
			consecutiveErrors++
			lastErr = getErr
			fmt.Printf("[github] error polling PR #%d merge state (%d/%d): %v\n",
				prNumber, consecutiveErrors, maxConsecutivePollErrors, getErr)
			if consecutiveErrors >= maxConsecutivePollErrors {
				return false, fmt.Errorf("giving up after %d consecutive errors polling PR #%d in %s: %v",
					maxConsecutivePollErrors, prNumber, repository, getErr)
			}
			return false, nil
		}
		consecutiveErrors = 0
		lastErr = nil

		if pr.GetMerged() {
			return true, nil
		}
		if pr.GetState() == "closed" {
			return false, fmt.Errorf("PR #%d in %s was closed without being merged", prNumber, repository)
		}
		fmt.Printf("[github] PR #%d in %s: merge still in progress (state=%s, merged=%v)\n",
			prNumber, repository, pr.GetState(), pr.GetMerged())
		return false, nil
	}, interval, timeout)
	if err != nil {
		if lastErr != nil {
			return nil, fmt.Errorf("waiting for PR #%d in %s to be merged after 405 response: %v (last API error: %v)",
				prNumber, repository, err, lastErr)
		}
		return nil, fmt.Errorf("waiting for PR #%d in %s to be merged after 405 response: %v", prNumber, repository, err)
	}

	merged := true
	return &github.PullRequestMergeResult{
		SHA:    github.String(pr.GetMergeCommitSHA()),
		Merged: &merged,
	}, nil
}

// getMergeResultFromPR fetches the PR and returns a synthetic merge result
// when the PR is already in merged state.
func (c *Client) getMergeResultFromPR(repository string, prNumber int) (*github.PullRequestMergeResult, error) {
	pr, err := c.GetPullRequest(repository, prNumber)
	if err != nil {
		return nil, fmt.Errorf("error fetching PR #%d in %s after already-merged response: %v", prNumber, repository, err)
	}
	if !pr.GetMerged() {
		return nil, fmt.Errorf("PR #%d in %s reported as already merged but GetMerged() is false (state=%s)", prNumber, repository, pr.GetState())
	}

	merged := true
	return &github.PullRequestMergeResult{
		SHA:    github.String(pr.GetMergeCommitSHA()),
		Merged: &merged,
	}, nil
}

// UpdatePullRequestBranch updates the PR branch with the latest changes from the base branch.
// This is useful when the PR branch is out of date and GitHub returns 409 on merge.
func (c *Client) UpdatePullRequestBranch(repository string, prNumber int) error {
	_, _, err := c.client.PullRequests.UpdateBranch(context.Background(), c.organization, repository, prNumber, nil)
	if err != nil {
		// UpdateBranch returns AcceptedError (HTTP 202) when the update is queued -- that's fine
		if _, ok := err.(*github.AcceptedError); ok {
			return nil
		}
		return fmt.Errorf("error when updating branch for pull request number %d in repo %s: %v", prNumber, repository, err)
	}
	return nil
}

func (c *Client) GetPRDetails(ghRepo string, prID int) (string, string, error) {
	pullRequest, err := c.GetPullRequest(ghRepo, prID)
	if err != nil {
		return "", "", err
	}
	cloneURL := pullRequest.GetHead().GetRepo().GetCloneURL()
	ref := pullRequest.GetHead().GetRef()
	if cloneURL == "" || ref == "" {
		return "", "", fmt.Errorf("incomplete PR head metadata for %s#%d: cloneURL=%q, ref=%q", ghRepo, prID, cloneURL, ref)
	}
	return cloneURL, ref, nil
}

