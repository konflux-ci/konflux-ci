package has

import (
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/forgejo"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/gitlab"
	kubeCl "github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kubernetes"
)

// Factory to initialize the comunication against different API like github, gitlab or kubernetes.
type HasController struct {
	// A Client manages communication with the GitHub API in a specific Organization.
	Github *github.Github

	// A Client manages communication with the GitLab API in a specific Organization.
	GitLab *gitlab.GitlabClient

	// A Client manages communication with Forgejo/Codeberg API in a specific Organization.
	Forgejo *forgejo.ForgejoClient

	// Generates a kubernetes client to interact with clusters.
	*kubeCl.CustomClient
}

// Initializes all the clients and return interface to operate with application-service controller.
func NewSuiteController(kube *kubeCl.CustomClient) (*HasController, error) {
	gh, err := github.NewGithubClient(utils.GetEnv(constants.GITHUB_TOKEN_ENV, ""),
		utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe"))
	if err != nil {
		return nil, err
	}

	groupId := utils.GetEnv("GITLAB_GROUP_ID", constants.DefaultGilabGroupId) // default id is for konflux-qe group
	gl, err := gitlab.NewGitlabClient(utils.GetEnv(constants.GITLAB_BOT_TOKEN_ENV, ""),
		utils.GetEnv(constants.GITLAB_API_URL_ENV, constants.DefaultGitLabAPIURL), groupId)
	if err != nil {
		return nil, err
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
			return nil, err
		}
	}

	return &HasController{
		Github:       gh,
		GitLab:       gl,
		Forgejo:      fj,
		CustomClient: kube,
	}, nil
}
