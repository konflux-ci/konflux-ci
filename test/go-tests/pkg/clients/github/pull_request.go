package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v44/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	ginkgo "github.com/onsi/ginkgo/v2"
)

func (g *Github) GetPullRequest(repository string, id int) (*github.PullRequest, error) {
	pr, _, err := g.client.PullRequests.Get(context.Background(), g.organization, repository, id)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (g *Github) CreatePullRequest(repository, title, body, head, base string) (*github.PullRequest, error) {
	newPR := &github.NewPullRequest{
		Title: &title,
		Body:  &body,
		Head:  &head,
		Base:  &base,
	}
	pr, _, err := g.client.PullRequests.Create(context.Background(), g.organization, repository, newPR)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (g *Github) ListPullRequests(repository string) ([]*github.PullRequest, error) {
	prs, _, err := g.client.PullRequests.List(context.Background(), g.organization, repository, &github.PullRequestListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error when listing pull requests for the repo %s: %v", repository, err)
	}

	return prs, nil
}

func (g *Github) ListPullRequestCommentsSince(repository string, prNumber int, since time.Time) ([]*github.IssueComment, error) {
	comments, _, err := g.client.Issues.ListComments(context.Background(), g.organization, repository, prNumber, &github.IssueListCommentsOptions{
		Since:     &since,
		Sort:      github.String("created"),
		Direction: github.String("asc"),
	})
	if err != nil {
		return nil, fmt.Errorf("error when listing pull requests comments for the repo %s: %v", repository, err)
	}

	return comments, nil
}

func (g *Github) MergePullRequest(repository string, prNumber int) (*github.PullRequestMergeResult, error) {
	mergeResult, _, err := g.client.PullRequests.Merge(context.Background(), g.organization, repository, prNumber, "", &github.PullRequestOptions{})
	if err != nil {
		mergeErr := fmt.Errorf("error when merging pull request number %d for the repo %s: %v", prNumber, repository, err)
		// If the head branch is out of date (409), trigger a branch update so the next retry can succeed
		if strings.Contains(err.Error(), "409") {
			fmt.Printf("[github] PR #%d in %s: head branch out of date, triggering branch update\n", prNumber, repository)
			if updateErr := g.UpdatePullRequestBranch(repository, prNumber); updateErr != nil {
				fmt.Printf("[github] failed to update PR #%d branch: %v\n", prNumber, updateErr)
			}
		}
		return nil, mergeErr
	}

	return mergeResult, nil
}

// UpdatePullRequestBranch updates the PR branch with the latest changes from the base branch.
// This is useful when the PR branch is out of date and GitHub returns 409 on merge.
func (g *Github) UpdatePullRequestBranch(repository string, prNumber int) error {
	_, _, err := g.client.PullRequests.UpdateBranch(context.Background(), g.organization, repository, prNumber, nil)
	if err != nil {
		// UpdateBranch returns AcceptedError (HTTP 202) when the update is queued -- that's fine
		if _, ok := err.(*github.AcceptedError); ok {
			return nil
		}
		return fmt.Errorf("error when updating branch for pull request number %d in repo %s: %v", prNumber, repository, err)
	}
	return nil
}

func (g *Github) ListCheckRuns(repository string, ref string) ([]*github.CheckRun, error) {
	checkRunResults, _, err := g.client.Checks.ListCheckRunsForRef(context.Background(), g.organization, repository, ref, &github.ListCheckRunsOptions{})
	if err != nil {
		return nil, fmt.Errorf("error when listing check runs for the repo %s and ref %s: %v", repository, ref, err)
	}
	return checkRunResults.CheckRuns, nil
}

func (g *Github) GetCheckRun(repository string, id int64) (*github.CheckRun, error) {
	checkRun, _, err := g.client.Checks.GetCheckRun(context.Background(), g.organization, repository, id)
	if err != nil {
		return nil, fmt.Errorf("error when getting check run with id %d for the repo %s: %v", id, repository, err)
	}
	return checkRun, nil
}

func (g *Github) GetPRDetails(ghRepo string, prID int) (string, string, error) {
	pullRequest, err := g.GetPullRequest(ghRepo, prID)
	if err != nil {
		return "", "", err
	}
	return *pullRequest.Head.Repo.CloneURL, *pullRequest.Head.Ref, nil
}

// GetCheckRunConclusion fetches a specific CheckRun within a given repo
// by matching the CheckRun's name with the given checkRunName, and
// then returns the CheckRun conclusion
func (g *Github) GetCheckRunConclusion(checkRunName, repoName, prHeadSha string, prNumber int) (string, error) {
	var errMsgSuffix = fmt.Sprintf("repository: %s, PR number: %d, PR head SHA: %s, checkRun name: %s\n", repoName, prNumber, prHeadSha, checkRunName)

	var checkRun *github.CheckRun
	var timeout time.Duration
	var err error

	timeout = time.Minute * 5

	err = utils.WaitUntil(func() (done bool, err error) {
		checkRuns, err := g.ListCheckRuns(repoName, prHeadSha)
		if err != nil {
			ginkgo.GinkgoWriter.Printf("got error when listing CheckRuns: %+v\n", err)
			return false, nil
		}
		for _, cr := range checkRuns {
			if strings.Contains(cr.GetName(), checkRunName) {
				checkRun = cr
				return true, nil
			}
		}
		return false, nil
	}, timeout)
	if err != nil {
		return "", fmt.Errorf("timed out when waiting for the PaC CheckRun to appear for %s", errMsgSuffix)
	}
	err = utils.WaitUntil(func() (done bool, err error) {
		checkRun, err = g.GetCheckRun(repoName, checkRun.GetID())
		if err != nil {
			ginkgo.GinkgoWriter.Printf("got error when listing CheckRuns: %+v\n", errMsgSuffix, err)
			return false, nil
		}
		currentCheckRunStatus := checkRun.GetStatus()
		if currentCheckRunStatus != constants.CheckrunStatusCompleted {
			ginkgo.GinkgoWriter.Printf("expecting CheckRun status %s, got: %s", constants.CheckrunStatusCompleted, currentCheckRunStatus)
			return false, nil
		}
		return true, nil
	}, timeout)
	if err != nil {
		return "", fmt.Errorf("timed out when waiting for the PaC CheckRun status to be '%s' for %s", constants.CheckrunStatusCompleted, errMsgSuffix)
	}
	return checkRun.GetConclusion(), nil
}

// GetCheckRunStatus fetches a specific CheckRun within a given repo
// by matching the CheckRun's name with the given checkRunName, and
// then returns the CheckRun status
func (g *Github) GetCheckRunStatus(checkRunName, repoName, prHeadSha string, prNumber int) (string, error) {
	var errMsgSuffix = fmt.Sprintf("repository: %s, PR number: %d, PR head SHA: %s, checkRun name: %s\n", repoName, prNumber, prHeadSha, checkRunName)

	var checkRun *github.CheckRun
	var timeout time.Duration
	var err error

	timeout = time.Minute * 5

	err = utils.WaitUntil(func() (done bool, err error) {
		checkRuns, err := g.ListCheckRuns(repoName, prHeadSha)
		if err != nil {
			ginkgo.GinkgoWriter.Printf("got error when listing CheckRuns: %+v\n", err)
			return false, nil
		}
		for _, cr := range checkRuns {
			if strings.Contains(cr.GetName(), checkRunName) {
				checkRun = cr
				return true, nil
			}
		}
		return false, nil
	}, timeout)
	if err != nil {
		return "", fmt.Errorf("timed out when waiting for the PaC CheckRun to appear for %s", errMsgSuffix)
	}

	return checkRun.GetStatus(), nil
}

// GetCheckRunText fetches a specific CheckRun within a given repo
// by matching the CheckRun's name with the given checkRunName, and
// then returns the CheckRun text
func (g *Github) GetCheckRunText(checkRunName, repoName, prHeadSha string, prNumber int) (string, error) {
	var errMsgSuffix = fmt.Sprintf("repository: %s, PR number: %d, PR head SHA: %s, checkRun name: %s\n", repoName, prNumber, prHeadSha, checkRunName)

	var checkRun *github.CheckRun
	var timeout time.Duration
	var err error

	timeout = time.Minute * 5

	err = utils.WaitUntil(func() (done bool, err error) {
		checkRuns, err := g.ListCheckRuns(repoName, prHeadSha)
		if err != nil {
			ginkgo.GinkgoWriter.Printf("got error when listing CheckRuns: %+v\n", err)
			return false, nil
		}
		for _, cr := range checkRuns {
			if strings.Contains(cr.GetName(), checkRunName) {
				checkRun = cr
				return true, nil
			}
		}
		return false, nil
	}, timeout)
	if err != nil {
		return "", fmt.Errorf("timed out when waiting for the PaC CheckRun to appear for %s", errMsgSuffix)
	}

	return checkRun.GetOutput().GetText(), nil
}
