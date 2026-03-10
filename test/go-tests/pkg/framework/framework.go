package framework

import (
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kube"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/has"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/integration"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/release"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/tekton"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type ControllerHub struct {
	HasController         *has.Controller
	CommonController      *kube.Controller
	TektonController      *tekton.Controller
	ReleaseController     *release.Controller
	IntegrationController *integration.Controller
}

type Framework struct {
	AsKubeAdmin      *ControllerHub
	ClusterAppDomain string
	UserNamespace    string
}

func NewFramework(userName string) (*Framework, error) {
	return NewFrameworkWithTimeout(userName, time.Second*60)
}

func NewFrameworkWithTimeout(userName string, timeout time.Duration) (*Framework, error) {
	if userName == "" {
		return nil, fmt.Errorf("userName cannot be empty when initializing a new framework instance")
	}

	client, err := kube.NewAdminKubernetesClient()
	if err != nil {
		return nil, err
	}

	asAdmin, err := InitControllerHub(client)
	if err != nil {
		return nil, fmt.Errorf("error when initializing appstudio hub controllers for admin user: %v", err)
	}

	nsName := os.Getenv(constants.E2E_APPLICATIONS_NAMESPACE_ENV)
	if nsName == "" {
		nsName = userName
		_, err := asAdmin.CommonController.CreateTestNamespace(userName)
		if err != nil {
			return nil, fmt.Errorf("failed to create test namespace %s: %+v", nsName, err)
		}
	}

	var clusterAppDomain string
	if os.Getenv(constants.TEST_ENVIRONMENT_ENV) == constants.UpstreamTestEnvironment {
		kubeconfig, err := config.GetConfig()
		if err != nil {
			return nil, fmt.Errorf("error when getting kubeconfig: %+v", err)
		}
		parsedURL, err := url.Parse(kubeconfig.Host)
		if err != nil {
			return nil, fmt.Errorf("failed to parse kubeconfig host URL: %+v", err)
		}
		clusterAppDomain = parsedURL.Hostname()
	}

	return &Framework{
		AsKubeAdmin:      asAdmin,
		ClusterAppDomain: clusterAppDomain,
		UserNamespace:    nsName,
	}, nil
}

func InitControllerHub(cc *kube.CustomClient) (*ControllerHub, error) {
	commonCtrl, err := kube.NewController(cc)
	if err != nil {
		return nil, err
	}

	hasController, err := has.NewController(cc)
	if err != nil {
		return nil, err
	}

	tektonController := tekton.NewController(cc)

	releaseController, err := release.NewController(cc)
	if err != nil {
		return nil, err
	}

	integrationController, err := integration.NewController(cc)
	if err != nil {
		return nil, err
	}

	return &ControllerHub{
		HasController:         hasController,
		CommonController:      commonCtrl,
		TektonController:      tektonController,
		ReleaseController:     releaseController,
		IntegrationController: integrationController,
	}, nil
}
