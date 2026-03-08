package common

import (
	"fmt"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/forgejo"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/git"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/gitlab"
	kubeCl "github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kubernetes"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

// Create the struct for kubernetes and github clients.
type SuiteController struct {
	// Wrap K8S client go to interact with Kube cluster
	*kubeCl.CustomClient

	Git git.Client
	// Github client to interact with GH apis
	Github *github.Github
	Gitlab *gitlab.GitlabClient
	// Forgejo client to interact with Forgejo/Codeberg APIs
	Forgejo *forgejo.ForgejoClient
}

/*
Create controller for the common kubernetes API crud operations. This controller should be used only to interact with non RHTAP/AppStudio APIS like routes, deployment, pods etc...
Check if a github organization env var is set, if not use by default the redhat-appstudio-qe org. See: https://github.com/redhat-appstudio-qe
*/
func NewSuiteController(kubeC *kubeCl.CustomClient) (*SuiteController, error) {
	gh, err := github.NewGithubClient(utils.GetEnv(constants.GITHUB_TOKEN_ENV, ""), utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe"))
	if err != nil {
		return nil, err
	}
	groupId := utils.GetEnv("GITLAB_GROUP_ID", constants.DefaultGilabGroupId) // default id is for konflux-qe group
	gl, err := gitlab.NewGitlabClient(utils.GetEnv(constants.GITLAB_BOT_TOKEN_ENV, ""), utils.GetEnv(constants.GITLAB_API_URL_ENV, constants.DefaultGitLabAPIURL), groupId)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with GitLab: %w", err)
	}

	// Initialize Forgejo client (for Codeberg)
	var fj *forgejo.ForgejoClient
	forgejoToken := utils.GetEnv(constants.CODEBERG_BOT_TOKEN_ENV, "")
	if forgejoToken != "" {
		fj, err = forgejo.NewForgejoClient(
			forgejoToken,
			utils.GetEnv(constants.CODEBERG_API_URL_ENV, constants.DefaultCodebergAPIURL),
			utils.GetEnv(constants.CODEBERG_QE_ORG_ENV, constants.DefaultCodebergQEOrg),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate with Forgejo/Codeberg: %w", err)
		}
	}

	return &SuiteController{
		CustomClient: kubeC,
		Github:       gh,
		Gitlab:       gl,
		Forgejo:      fj,
	}, nil
}
