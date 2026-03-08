package git

import (
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/forgejo"
)

type ForgejoClient struct {
	*forgejo.ForgejoClient
}

func NewForgejoClient(fc *forgejo.ForgejoClient) *ForgejoClient {
	return &ForgejoClient{fc}
}

func (f *ForgejoClient) CreateBranch(repository, baseBranchName, _, branchName string) error {
	return f.ForgejoClient.CreateBranch(repository, branchName, baseBranchName)
}

func (f *ForgejoClient) BranchExists(repository, branchName string) (bool, error) {
	return f.ExistsBranch(repository, branchName)
}

func (f *ForgejoClient) ListPullRequests(projectID string) ([]*PullRequest, error) {
	prs, err := f.GetPullRequests(projectID)
	if err != nil {
		return nil, err
	}
	var pullRequests []*PullRequest
	for _, pr := range prs {
		pullRequests = append(pullRequests, &PullRequest{
			Number:       int(pr.Index),
			SourceBranch: pr.Head.Ref,
			TargetBranch: pr.Base.Ref,
			HeadSHA:      pr.Head.Sha,
		})
	}
	return pullRequests, nil
}

func (f *ForgejoClient) CreateFile(repository, pathToFile, content, branchName string) (*RepositoryFile, error) {
	fileResp, err := f.ForgejoClient.CreateFile(repository, pathToFile, content, branchName)
	if err != nil {
		return nil, err
	}

	resultFile := &RepositoryFile{
		CommitSHA: fileResp.Commit.SHA,
	}
	return resultFile, nil
}

func (f *ForgejoClient) GetFile(repository, pathToFile, branchName string) (*RepositoryFile, error) {
	content, contentsResp, err := f.ForgejoClient.GetFile(repository, pathToFile, branchName)
	if err != nil {
		return nil, err
	}

	resultFile := &RepositoryFile{
		CommitSHA: contentsResp.SHA,
		Content:   content,
	}
	return resultFile, nil
}

func (f *ForgejoClient) MergePullRequest(repository string, prNumber int) (*PullRequest, error) {
	pr, err := f.ForgejoClient.MergePullRequest(repository, int64(prNumber))
	if err != nil {
		return nil, err
	}
	mergeCommitSHA := ""
	if pr.MergedCommitID != nil {
		mergeCommitSHA = *pr.MergedCommitID
	}
	return &PullRequest{
		Number:         int(pr.Index),
		SourceBranch:   pr.Head.Ref,
		TargetBranch:   pr.Base.Ref,
		HeadSHA:        pr.Head.Sha,
		MergeCommitSHA: mergeCommitSHA,
	}, nil
}

func (f *ForgejoClient) CreatePullRequest(repository, title, body, head, base string) (*PullRequest, error) {
	pr, err := f.ForgejoClient.CreatePullRequest(repository, title, body, head, base)
	if err != nil {
		return nil, err
	}
	return &PullRequest{
		Number:       int(pr.Index),
		SourceBranch: pr.Head.Ref,
		TargetBranch: pr.Base.Ref,
		HeadSHA:      pr.Head.Sha,
	}, nil
}

func (f *ForgejoClient) CleanupWebhooks(repository, clusterAppDomain string) error {
	return f.DeleteWebhooks(repository, clusterAppDomain)
}

func (f *ForgejoClient) DeleteBranchAndClosePullRequest(repository string, prNumber int) error {
	prs, err := f.GetPullRequests(repository)
	if err != nil {
		return err
	}

	// Find the PR to get its source branch
	var sourceBranch string
	for _, pr := range prs {
		if int(pr.Index) == prNumber {
			sourceBranch = pr.Head.Ref
			break
		}
	}

	if sourceBranch != "" {
		err = f.ForgejoClient.DeleteBranch(repository, sourceBranch)
		if err != nil {
			return err
		}
	}

	return f.ClosePullRequest(repository, int64(prNumber))
}

func (f *ForgejoClient) ForkRepository(sourceRepoName, targetRepoName string) error {
	_, err := f.ForgejoClient.ForkRepository(sourceRepoName, targetRepoName)
	return err
}

func (f *ForgejoClient) DeleteRepositoryIfExists(repoName string) error {
	return f.ForgejoClient.DeleteRepositoryIfExists(repoName)
}

// DeleteBranch deletes a branch from a repository
func (f *ForgejoClient) DeleteBranch(repository, branchName string) error {
	return f.ForgejoClient.DeleteBranch(repository, branchName)
}

// GetCommitStatusConclusion returns the commit status for a given commit
func (f *ForgejoClient) GetCommitStatusConclusion(statusName, projectID, commitSHA string, prNumber int) string {
	return f.ForgejoClient.GetCommitStatusConclusion(statusName, projectID, commitSHA, int64(prNumber))
}

