package has

import (
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/gitlab"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kube"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

type Controller struct {
	GitHub *github.Client
	GitLab *gitlab.Client
	*kube.CustomClient
}

func NewController(k *kube.CustomClient) (*Controller, error) {
	gh, err := github.NewClient(utils.GetEnv(constants.GITHUB_TOKEN_ENV, ""),
		utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe"))
	if err != nil {
		return nil, err
	}

	groupId := utils.GetEnv("GITLAB_GROUP_ID", constants.DefaultGilabGroupId)
	gl, err := gitlab.NewClient(utils.GetEnv(constants.GITLAB_BOT_TOKEN_ENV, ""),
		utils.GetEnv(constants.GITLAB_API_URL_ENV, constants.DefaultGitLabAPIURL), groupId)
	if err != nil {
		return nil, err
	}

	return &Controller{
		GitHub:       gh,
		GitLab:       gl,
		CustomClient: k,
	}, nil
}
