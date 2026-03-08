package git

import (
	"fmt"
	"strings"
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/github"
	"k8s.io/klog/v2"
)

type GitHubClient struct {
	*github.Github
}

func NewGitHubClient(gh *github.Github) *GitHubClient {
	return &GitHubClient{gh}
}

func (g *GitHubClient) CreateBranch(repository, baseBranchName, revision, branchName string) error {
	return g.CreateRef(repository, baseBranchName, revision, branchName)
}

func (g *GitHubClient) DeleteBranch(repository, branchName string) error {
	return g.DeleteRef(repository, branchName)
}

func (g *GitHubClient) BranchExists(repository, branchName string) (bool, error) {
	return g.ExistsRef(repository, branchName)
}

// ListPullRequestsWithRetry wraps Client.ListPullRequests with up to 3 retries
// on transient errors (e.g. GitHub 503). Returns the PRs or the last error
// after all retries are exhausted.
func ListPullRequestsWithRetry(client Client, repository string) ([]*PullRequest, error) {
	const maxRetries = 3
	var prs []*PullRequest
	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		prs, err = client.ListPullRequests(repository)
		if err == nil {
			return prs, nil
		}
		klog.Warningf("error listing PRs in %s (attempt %d/%d): %v", repository, attempt, maxRetries, err)
		if attempt < maxRetries {
			time.Sleep(5 * time.Second)
		}
	}
	return nil, fmt.Errorf("failed to list pull requests for %s after %d retries: %w", repository, maxRetries, err)
}

func (g *GitHubClient) ListPullRequests(repository string) ([]*PullRequest, error) {
	prs, err := g.Github.ListPullRequests(repository)
	if err != nil {
		return nil, err
	}
	var pullRequests []*PullRequest
	for _, pr := range prs {
		pullRequests = append(pullRequests, &PullRequest{
			Number:       pr.GetNumber(),
			SourceBranch: pr.Head.GetRef(),
			TargetBranch: pr.Base.GetRef(),
			HeadSHA:      pr.Head.GetSHA(),
		})
	}
	return pullRequests, nil
}

func (g *GitHubClient) CreateFile(repository, pathToFile, content, branchName string) (*RepositoryFile, error) {
	file, err := g.Github.CreateFile(repository, pathToFile, content, branchName)
	if err != nil {
		return nil, err
	}
	resultFile := &RepositoryFile{
		CommitSHA: file.GetSHA(),
	}
	return resultFile, nil
}

func (g *GitHubClient) GetFile(repository, pathToFile, branchName string) (*RepositoryFile, error) {
	contents, err := g.Github.GetFile(repository, pathToFile, branchName)
	if err != nil {
		return nil, err
	}
	content, err := contents.GetContent()
	if err != nil {
		return nil, err
	}
	resultFile := &RepositoryFile{
		CommitSHA: contents.GetSHA(),
		Content:   content,
	}
	return resultFile, nil
}

func (g *GitHubClient) MergePullRequest(repository string, prNumber int) (*PullRequest, error) {
	mergeResult, err := g.Github.MergePullRequest(repository, prNumber)
	if err != nil {
		return nil, err
	}
	return &PullRequest{
		Number:         prNumber,
		MergeCommitSHA: mergeResult.GetSHA(),
	}, nil
}

func (g *GitHubClient) UpdatePullRequestBranch(repository string, prNumber int) error {
	return g.Github.UpdatePullRequestBranch(repository, prNumber)
}

func (g *GitHubClient) CreatePullRequest(repository, title, body, head, base string) (*PullRequest, error) {
	pr, err := g.Github.CreatePullRequest(repository, title, body, head, base)
	if err != nil {
		return nil, err
	}
	return &PullRequest{
		Number:       pr.GetNumber(),
		SourceBranch: pr.Head.GetRef(),
		TargetBranch: pr.Base.GetRef(),
		HeadSHA:      pr.Head.GetSHA(),
	}, nil
}

func (g *GitHubClient) CleanupWebhooks(repository, clusterAppDomain string) error {
	hooks, err := g.ListRepoWebhooks(repository)
	if err != nil {
		return err
	}
	for _, h := range hooks {
		hookUrl := h.Config["url"].(string)
		if strings.Contains(hookUrl, clusterAppDomain) {
			fmt.Printf("removing webhook URL: %s\n", hookUrl)
			err = g.DeleteWebhook(repository, h.GetID())
			if err != nil {
				return err
			}
			break
		}
	}
	return nil
}

func (g *GitHubClient) DeleteBranchAndClosePullRequest(repository string, prNumber int) error {
	pr, err := g.GetPullRequest(repository, prNumber)
	if err != nil {
		return err
	}
	err = g.DeleteBranch(repository, pr.Head.GetRef())
	if err != nil && strings.Contains(err.Error(), "Reference does not exist") {
		return nil
	}
	return err
}

func (g *GitHubClient) ForkRepository(sourceRepoName, targetRepoName string) error {
	_, err := g.Github.ForkRepository(sourceRepoName, targetRepoName)
	if err != nil {
		return err
	}
	return nil
}

func (g *GitHubClient) DeleteRepositoryIfExists(repoName string) error {
	err := g.Github.DeleteRepositoryIfExists(repoName)
	if err != nil {
		return err
	}
	return nil
}
