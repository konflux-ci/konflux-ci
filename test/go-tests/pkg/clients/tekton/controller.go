package tekton

import (
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kube"
)

// Create the struct for kubernetes clients
type Controller struct {
	*kube.CustomClient
}

// Create controller for Tekton Task/Pipeline CRUD operations
func NewController(kube *kube.CustomClient) *Controller {
	return &Controller{
		kube,
	}
}
