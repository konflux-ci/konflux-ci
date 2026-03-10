package gitlab

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/onsi/gomega"
	"github.com/xanzy/go-gitlab"

	utils "github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

const (
	defaultForkImportTimeoutMinutes = 20
)

func forkImportTimeout() time.Duration {
	if m := utils.GetEnv("GITLAB_FORK_IMPORT_TIMEOUT_MINUTES", ""); m != "" {
		if mins, err := strconv.Atoi(m); err == nil && mins > 0 {
			return time.Duration(mins) * time.Minute
		}
	}
	return defaultForkImportTimeoutMinutes * time.Minute
}

// CreateBranch creates a new branch in a GitLab project with the given projectID and newBranchName
func (c *Client) CreateBranch(projectID, newBranchName, defaultBranch string) error {
	// Prepare the branch creation request
	branchOpts := &gitlab.CreateBranchOptions{
		Branch: gitlab.Ptr(newBranchName),
		Ref:    gitlab.Ptr(defaultBranch),
	}

	// Perform the branch creation
	_, _, err := c.client.Branches.CreateBranch(projectID, branchOpts)
	if err != nil {
		return fmt.Errorf("failed to create branch %s in project %s: %w", newBranchName, projectID, err)
	}

	// Wait for the branch to actually exist
	gomega.Eventually(func(gm gomega.Gomega) {
		exist, err := c.ExistsBranch(projectID, newBranchName)
		gm.Expect(err).NotTo(gomega.HaveOccurred())
		gm.Expect(exist).To(gomega.BeTrue())

	}, 2*time.Minute, 2*time.Second).Should(gomega.Succeed())

	return nil
}

// ExistsBranch checks if a branch exists in a specified GitLab repository.
func (c *Client) ExistsBranch(projectID, branchName string) (bool, error) {

	_, _, err := c.client.Branches.GetBranch(projectID, branchName)
	if err == nil {
		return true, nil
	}
	if err, ok := err.(*gitlab.ErrorResponse); ok && err.Response.StatusCode == 404 {
		return false, nil
	}
	return false, err
}

// DeleteBranch deletes a branch by its name and project ID
func (c *Client) DeleteBranch(projectID, branchName string) error {

	_, err := c.client.Branches.DeleteBranch(projectID, branchName)
	if err != nil {
		return fmt.Errorf("failed to delete branch %s: %v", branchName, err)
	}

	fmt.Printf("Deleted branch: %s", branchName)

	return nil
}

// CreateGitlabNewBranch creates a new branch
func (c *Client) CreateGitlabNewBranch(projectID, branchName, sha, baseBranch string) error {

	// If sha is not provided, get the latest commit from the base branch
	if sha == "" {
		commit, _, err := c.client.Commits.GetCommit(projectID, baseBranch, &gitlab.GetCommitOptions{})
		if err != nil {
			return fmt.Errorf("failed to get latest commit from base branch: %v", err)
		}
		sha = commit.ID
	}

	opt := &gitlab.CreateBranchOptions{
		Branch: &branchName,
		Ref:    &sha,
	}
	_, resp, err := c.client.Branches.CreateBranch(projectID, opt)
	if err != nil {
		// Check if the error is due to the branch already existing
		if resp != nil && resp.StatusCode == http.StatusConflict {
			return fmt.Errorf("branch '%s' already exists", branchName)
		}
		return fmt.Errorf("failed to create branch '%s': %v", branchName, err)
	}

	return nil
}

// GetMergeRequests returns a list of all MergeRequests in a given project ID and repository name
func (c *Client) GetMergeRequests(projectId string) ([]*gitlab.MergeRequest, error) {

	listMRsOptions := &gitlab.ListProjectMergeRequestsOptions{
		State: gitlab.Ptr("opened"), // Filter for only open merge requests
		ListOptions: gitlab.ListOptions{
			Page:    1,
			PerPage: 100,
		},
	}
	// Get merge requests for the specific group
	mergeRequests, _, err := c.client.MergeRequests.ListProjectMergeRequests(projectId, listMRsOptions)
	if err != nil {
		return nil, err
	}

	return mergeRequests, nil
}

// CloseMergeRequest closes merge request in Gitlab repo by given MR IID
func (c *Client) CloseMergeRequest(projectID string, mergeRequestIID int) error {

	// Get merge requests using Gitlab client
	_, _, err := c.client.MergeRequests.GetMergeRequest(projectID, mergeRequestIID, nil)
	if err != nil {
		return fmt.Errorf("failed to get MR of IID %d in projectID %s, %v", mergeRequestIID, projectID, err)
	}

	_, _, err = c.client.MergeRequests.UpdateMergeRequest(projectID, mergeRequestIID, &gitlab.UpdateMergeRequestOptions{
		StateEvent: gitlab.Ptr("close"),
	})
	if err != nil {
		return fmt.Errorf("failed to close MR of IID %d in projectID %s, %v", mergeRequestIID, projectID, err)
	}

	return nil
}

// DeleteWebhooks deletes webhooks in Gitlab repo by given project ID,
// and if the webhook URL contains the cluster's domain name.
func (c *Client) DeleteWebhooks(projectID, clusterAppDomain string) error {

	// Check if clusterAppDomain is empty returns error, else continue
	if clusterAppDomain == "" {
		return fmt.Errorf("Framework.ClusterAppDomain is empty")
	}

	// List project hooks
	webhooks, _, err := c.client.Projects.ListProjectHooks(projectID, nil)
	if err != nil {
		return fmt.Errorf("failed to list project hooks for project id: %s with error: %v", projectID, err)
	}

	// Delete matching webhooks
	for _, webhook := range webhooks {
		if strings.Contains(webhook.URL, clusterAppDomain) {
			if _, err := c.client.Projects.DeleteProjectHook(projectID, webhook.ID); err != nil {
				return fmt.Errorf("failed to delete webhook (ID: %d): %v", webhook.ID, err)
			}
			break
		}
	}

	return nil
}

func (c *Client) CreateFile(projectId, pathToFile, fileContent, branchName string) (*gitlab.FileInfo, error) {
	opts := &gitlab.CreateFileOptions{
		Branch:        gitlab.Ptr(branchName),
		Content:       &fileContent,
		CommitMessage: gitlab.Ptr("e2e test commit message"),
	}

	file, resp, err := c.client.RepositoryFiles.CreateFile(projectId, pathToFile, opts)
	if resp.StatusCode != 201 || err != nil {
		return nil, fmt.Errorf("error when creating file contents: response (%v) and error: %v", resp, err)
	}

	return file, nil
}

func (c *Client) GetFile(projectId, pathToFile, branchName string) (string, error) {
	file, _, err := c.client.RepositoryFiles.GetFile(projectId, pathToFile, gitlab.Ptr(gitlab.GetFileOptions{Ref: gitlab.Ptr(branchName)}))
	if err != nil {
		return "", fmt.Errorf("failed to get file: %v", err)
	}

	decodedContent, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil {
		return "", fmt.Errorf("failed to decode file content: %v", err)
	}
	fileContentString := string(decodedContent)

	return fileContentString, nil
}

func (c *Client) GetFileMetaData(projectID, pathToFile, branchName string) (*gitlab.File, error) {
	metadata, _, err := c.client.RepositoryFiles.GetFileMetaData(projectID, pathToFile, gitlab.Ptr(gitlab.GetFileMetaDataOptions{Ref: gitlab.Ptr(branchName)}))
	return metadata, err
}

func (c *Client) UpdateFile(projectId, pathToFile, fileContent, branchName string) (string, error) {
	updateOptions := &gitlab.UpdateFileOptions{
		Branch:        gitlab.Ptr(branchName),
		Content:       gitlab.Ptr(fileContent),
		CommitMessage: gitlab.Ptr("e2e test commit message"),
	}

	_, _, err := c.client.RepositoryFiles.UpdateFile(projectId, pathToFile, updateOptions)
	if err != nil {
		return "", fmt.Errorf("failed to update/create file: %v", err)
	}

	// Well, this is not atomic, but best I figured.
	file, _, err := c.client.RepositoryFiles.GetFile(projectId, pathToFile, gitlab.Ptr(gitlab.GetFileOptions{Ref: gitlab.Ptr(branchName)}))
	if err != nil {
		return "", fmt.Errorf("failed to get file: %v", err)
	}

	return file.CommitID, nil
}

func (c *Client) AcceptMergeRequest(projectID string, mrID int) (*gitlab.MergeRequest, error) {
	mr, _, err := c.client.MergeRequests.AcceptMergeRequest(projectID, mrID, nil)
	return mr, err
}

func (c *Client) GetCommitStatusConclusion(statusName, projectID, commitSHA string, mergeRequestID int) string {
	var matchingStatus *gitlab.CommitStatus
	timeout := time.Minute * 10

	gomega.Eventually(func() bool {
		statuses, _, err := c.client.Commits.GetCommitStatuses(projectID, commitSHA, &gitlab.GetCommitStatusesOptions{})
		if err != nil {
			fmt.Printf("got error when listing commit statuses: %+v\n", err)
			return false
		}
		for _, status := range statuses {
			if strings.Contains(status.Name, statusName) {
				matchingStatus = status
				return true
			}
		}
		return false
	}, timeout, time.Second*2).Should(gomega.BeTrue(), fmt.Sprintf("timed out waiting for the PaC commit status to appear for %s", commitSHA))

	gomega.Eventually(func() bool {
		statuses, _, err := c.client.Commits.GetCommitStatuses(projectID, commitSHA, &gitlab.GetCommitStatusesOptions{})
		if err != nil {
			fmt.Printf("got error when checking commit status: %+v\n", err)
			return false
		}
		for _, status := range statuses {
			if strings.Contains(status.Name, statusName) {
				currentState := status.Status
				if currentState != "pending" && currentState != "running" {
					matchingStatus = status
					return true
				}
				fmt.Printf("expecting commit status to be completed, got: %s\n", currentState)
				return false
			}
		}
		return false
	}, timeout, time.Second*2).Should(gomega.BeTrue(), fmt.Sprintf("timed out waiting for the PaC commit status to be completed for %s", commitSHA))

	return matchingStatus.Status
}

// DeleteRepositoryIfExists deletes a GitLab repository if it exists.
// Returns an error if the deletion fails except for project not being found (404).
func (c *Client) DeleteRepositoryIfExists(projectID string) error {
	getProj, getResp, getErr := c.client.Projects.GetProject(projectID, nil)
	if getErr != nil {
		if getResp != nil && getResp.StatusCode == http.StatusNotFound {
			return nil
		} else {
			return fmt.Errorf("error getting project %s: %v", projectID, getErr)
		}
	}
	if getProj.PathWithNamespace != projectID && (strings.Contains(getProj.PathWithNamespace, projectID+"-deleted-") || strings.Contains(getProj.PathWithNamespace, projectID+"-deletion_scheduled-")) {
		// We asked for repo like "jhutar/nodejs-devfile-sample7-ocpp01v1-konflux-perfscale"
		// and got "jhutar/nodejs-devfile-sample7-ocpp01v1-konflux-perfscale-deleted-138805"
		// and that means repo was moved by being deleted for a first
		// time, entering a grace period.

		// Now we need to delete the repository for a second time to limit
		// number of repos we keep behind as per request in INC3755661
		err := c.DeleteRepositoryReally(getProj.PathWithNamespace)
		return err
	}

	resp, err := c.client.Projects.DeleteProject(projectID, &gitlab.DeleteProjectOptions{})

	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil
		}
		return fmt.Errorf("error deleting project %s: %w", projectID, err)
	}

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected status code when deleting project %s: %d", projectID, resp.StatusCode)
	}

	err = utils.WaitUntilWithInterval(func() (done bool, err error) {
		getProj, getResp, getErr := c.client.Projects.GetProject(projectID, nil)

		if getErr != nil {
			if getResp != nil && getResp.StatusCode == http.StatusNotFound {
				return true, nil
			} else {
				return false, getErr
			}
		}

		if getProj.PathWithNamespace != projectID && (strings.Contains(getProj.PathWithNamespace, projectID+"-deleted-") || strings.Contains(getProj.PathWithNamespace, projectID+"-deletion_scheduled-")) {
			errDel := c.DeleteRepositoryReally(getProj.PathWithNamespace)
			if errDel != nil {
				return false, errDel
			}
			return true, nil
		}

		fmt.Printf("Repo %s still exists: %+v\n", projectID, getResp)
		return false, nil
	}, time.Second*10, time.Minute*5)

	return err
}

// DeleteRepositoryOnlyIfExists soft deletes a GitLab repository if it exists.
// Returns an error if the deletion fails except for project not being found (404).
func (c *Client) DeleteRepositoryOnlyIfExists(projectID string) error {
	_, getResp, getErr := c.client.Projects.GetProject(projectID, nil)
	if getErr != nil {
		if getResp != nil && getResp.StatusCode == http.StatusNotFound {
			return nil
		} else {
			return fmt.Errorf("error getting project %s: %v", projectID, getErr)
		}
	}
	// Delete the project, the response will indicate if the request was successful
	resp, err := c.client.Projects.DeleteProject(projectID, nil)
	if err != nil {
		return fmt.Errorf("failed to delete gitlab project: %v", err)
	}

	// Check the response status code
	switch resp.StatusCode {
	case http.StatusAccepted:
		fmt.Printf("Project %s marked for deletion (soft delete).\n", projectID)
		return nil
	case http.StatusNoContent:
		fmt.Printf("Project %s permanently deleted.\n", projectID)
		return nil
	default:
		return fmt.Errorf("unexpected status code %d while deleting gitlab project %s", resp.StatusCode, projectID)
	}
}

// GitLab have a concept of two deletes. First one just renames the repo,
// and only second one really deletes it. DeleteRepositoryReally is meant for
// the second deletition.
func (c *Client) DeleteRepositoryReally(projectID string) error {
	opts := &gitlab.DeleteProjectOptions{
		FullPath:          gitlab.Ptr(projectID),
		PermanentlyRemove: gitlab.Ptr(true),
	}
	_, err := c.client.Projects.DeleteProject(projectID, opts)
	if err != nil {
		return fmt.Errorf("error on permanently deleting project %s: %w", projectID, err)
	}
	return nil
}

// ForkRepository forks a source GitLab repository to a target repository.
// Returns the newly forked repository and an error if the operation fails.
func (c *Client) ForkRepository(sourceOrgName, sourceName, targetOrgName, targetName string) (*gitlab.Project, error) {
	var forkedProject *gitlab.Project
	var resp *gitlab.Response
	var err error

	sourceProjectID := sourceOrgName + "/" + sourceName
	targetProjectID := targetOrgName + "/" + targetName

	opts := &gitlab.ForkProjectOptions{
		Name:          gitlab.Ptr(targetName),
		NamespacePath: gitlab.Ptr(targetOrgName),
		Path:          gitlab.Ptr(targetName),
	}

	err = utils.WaitUntilWithInterval(func() (done bool, err error) {
		forkedProject, resp, err = c.client.Projects.ForkProject(sourceProjectID, opts)
		if err != nil {
			fmt.Printf("[gitlab] fork %s -> %s: API call failed, retrying: %v\n", sourceProjectID, targetProjectID, err)
			return false, nil
		}
		if forkedProject == nil {
			return false, fmt.Errorf("fork API returned success but project is nil for %s", sourceProjectID)
		}
		return true, nil
	}, time.Second*10, time.Minute*5)
	if err != nil {
		return nil, fmt.Errorf("fork project %s to %s did not succeed within 5m (GitLab fork API): %w", sourceProjectID, targetProjectID, err)
	}

	if forkedProject == nil {
		return nil, fmt.Errorf("fork project %s to %s: project is nil after fork API success", sourceProjectID, targetProjectID)
	}
	if resp != nil && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("unexpected status code when forking project %s: %d", sourceProjectID, resp.StatusCode)
	}

	var lastImportStatus string
	err = utils.WaitUntilWithInterval(func() (done bool, err error) {
		var getErr error
		var proj *gitlab.Project

		proj, _, getErr = c.client.Projects.GetProject(forkedProject.ID, nil)
		if getErr != nil {
			// Treat GetProject errors as transient (e.g. GitLab 500) — retry instead of failing
			fmt.Printf("[gitlab] fork %s (ID: %d): GetProject error (retrying): %v\n", targetProjectID, forkedProject.ID, getErr)
			return false, nil
		}
		if proj == nil {
			return false, fmt.Errorf("GetProject returned nil for project ID %d", forkedProject.ID)
		}
		forkedProject = proj

		lastImportStatus = forkedProject.ImportStatus
		switch forkedProject.ImportStatus {
		case "finished":
			return true, nil
		case "failed", "timeout":
			return false, fmt.Errorf("fork import failed for project %s (ID: %d): import_status=%s", forkedProject.Name, forkedProject.ID, forkedProject.ImportStatus)
		default:
			fmt.Printf("[gitlab] fork %s (ID: %d): waiting for import, status=%q\n", targetProjectID, forkedProject.ID, forkedProject.ImportStatus)
			return false, nil
		}
	}, time.Second*10, forkImportTimeout())

	if err != nil {
		projectIDDesc := targetProjectID
		if forkedProject != nil {
			projectIDDesc = fmt.Sprintf("%s (ID: %d)", targetProjectID, forkedProject.ID)
		}
		return nil, fmt.Errorf("fork import for project %s did not complete within %v (last import_status: %q): %w", projectIDDesc, forkImportTimeout(), lastImportStatus, err)
	}

	if forkedProject == nil {
		return nil, fmt.Errorf("fork completed but project %s is nil", targetProjectID)
	}
	return forkedProject, nil
}

// EnsureBranchExists checks if a branch exists in the repository and creates one from
// fallback branch if not.
func (c *Client) EnsureBranchExists(projectID, branchName, fallbackBranch string) error {
	exists, err := c.ExistsBranch(projectID, branchName)
	if err != nil {
		return fmt.Errorf("error checking if branch '%s' exists: %w", branchName, err)
	}
	if exists {
		return nil
	}
	// Branch doesn't exist, create it from fallback branch
	return c.CreateBranch(projectID, branchName, fallbackBranch)
}

func (c *Client) GetAllProjects() ([]*gitlab.Project, error) {
	listProjectsOptions := &gitlab.ListProjectsOptions{
		Membership: gitlab.Ptr(true),
		ListOptions: gitlab.ListOptions{
			Page:    1,
			PerPage: 100,
		},
	}
	var allProjects []*gitlab.Project
	for {
		// Get the current page of projects
		projects, resp, err := c.client.Projects.ListProjects(listProjectsOptions)
		if err != nil {
			return allProjects, fmt.Errorf("failed to list projects: %v", err)
		}
		allProjects = append(allProjects, projects...)
		// Check if there are more pages. If not, break the loop.
		if resp.NextPage == 0 {
			break
		}

		// Update the page number to fetch the next page in the next iteration
		listProjectsOptions.Page = resp.NextPage
	}

	return allProjects, nil
}
