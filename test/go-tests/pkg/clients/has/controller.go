package has

import (
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kube"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

type Controller struct {
	GitHub *github.Client
	*kube.CustomClient
}

func NewController(k *kube.CustomClient) (*Controller, error) {
	gh, err := github.NewClient(utils.GetEnv(constants.GITHUB_TOKEN_ENV, ""),
		utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe"))
	if err != nil {
		return nil, err
	}

	return &Controller{
		GitHub:       gh,
		CustomClient: k,
	}, nil
}
