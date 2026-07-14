package kube

import (
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

type Controller struct {
	*CustomClient

	GitHub *github.Client
}

func NewController(kubeC *CustomClient) (*Controller, error) {
	gh, err := github.NewClient(utils.GetEnv(constants.GITHUB_TOKEN_ENV, ""), utils.GetEnv(constants.GITHUB_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe"))
	if err != nil {
		return nil, err
	}

	return &Controller{
		CustomClient: kubeC,
		GitHub:       gh,
	}, nil
}
