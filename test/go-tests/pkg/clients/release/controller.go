package release

import "github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kube"

// Factory to initialize the comunication against different API like github or kubernetes.
type Controller struct {
	// Generates a kubernetes client to interact with clusters.
	*kube.CustomClient
}

// Initializes all the clients and return interface to operate with release controller.
func NewController(kube *kube.CustomClient) (*Controller, error) {
	return &Controller{
		kube,
	}, nil
}
