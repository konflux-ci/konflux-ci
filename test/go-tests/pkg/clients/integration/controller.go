package integration

import (
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kube"
)

type Controller struct {
	*kube.CustomClient
}

func NewController(kube *kube.CustomClient) (*Controller, error) {
	return &Controller{
		kube,
	}, nil
}
