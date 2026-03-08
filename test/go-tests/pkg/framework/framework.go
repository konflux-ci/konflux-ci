package framework

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/avast/retry-go/v4"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/common"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/has"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/imagecontroller"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/integration"
	kubeCl "github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kubernetes"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/release"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/tekton"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/sandbox"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

type ControllerHub struct {
	HasController         *has.HasController
	CommonController      *common.SuiteController
	TektonController      *tekton.TektonController
	ReleaseController     *release.ReleaseController
	IntegrationController *integration.IntegrationController
	ImageController       *imagecontroller.ImageController
}

type Framework struct {
	AsKubeAdmin          *ControllerHub
	AsKubeDeveloper      *ControllerHub
	ClusterAppDomain     string
	OpenshiftConsoleHost string
	ProxyUrl             string
	SandboxController    *sandbox.SandboxController
	UserNamespace        string
	UserName             string
	UserToken            string
}

func NewFramework(userName string, stageConfig ...utils.Options) (*Framework, error) {
	return NewFrameworkWithTimeout(userName, time.Second*60, stageConfig...)
}

func NewFrameworkWithTimeout(userName string, timeout time.Duration, options ...utils.Options) (*Framework, error) {
	var err error
	var k *kubeCl.K8SClient
	var clusterAppDomain, openshiftConsoleHost string
	var option utils.Options
	var asUser *ControllerHub

	if userName == "" {
		return nil, fmt.Errorf("userName cannot be empty when initializing a new framework instance")
	}
	isStage, err := utils.CheckOptions(options)
	if err != nil {
		return nil, err
	}
	if isStage {
		option = options[0]
	} else {
		option = utils.Options{}
	}

	var asAdmin *ControllerHub
	if isStage {
		// in some very rare cases fail to get the client for some timeout in member operator.
		// Just try several times to get the user kubeconfig
		err = retry.Do(
			func() error {
				if k, err = kubeCl.NewDevSandboxProxyClient(userName, option); err != nil {
					ginkgo.GinkgoWriter.Printf("error when creating dev sandbox proxy client: %+v\n", err)
				}
				return err
			},
			retry.Attempts(20),
		)

		if err != nil {
			return nil, fmt.Errorf("error when initializing kubernetes clients: %v", err)
		}
		asUser, err = InitControllerHub(k.AsKubeDeveloper)
		if err != nil {
			return nil, fmt.Errorf("error when initializing appstudio hub controllers for sandbox user: %v", err)
		}
		asAdmin = asUser

	} else {
		client, err := kubeCl.NewAdminKubernetesClient()
		if err != nil {
			return nil, err
		}

		asAdmin, err = InitControllerHub(client)
		if err != nil {
			return nil, fmt.Errorf("error when initializing appstudio hub controllers for admin user: %v", err)
		}
		asUser = asAdmin

		nsName := os.Getenv(constants.E2E_APPLICATIONS_NAMESPACE_ENV)
		if nsName == "" {
			nsName = userName

			_, err := asAdmin.CommonController.CreateTestNamespace(userName)
			if err != nil {
				return nil, fmt.Errorf("failed to create test namespace %s: %+v", nsName, err)
			}

		}

		if os.Getenv(constants.TEST_ENVIRONMENT_ENV) == constants.UpstreamTestEnvironment {
			// Get cluster domain (IP address) from kubeconfig
			kubeconfig, err := config.GetConfig()
			if err != nil {
				return nil, fmt.Errorf("error when getting kubeconfig: %+v", err)
			}

			parsedURL, err := url.Parse(kubeconfig.Host)
			if err != nil {
				return nil, fmt.Errorf("failed to parse kubeconfig host URL: %+v", err)
			}
			clusterAppDomain = parsedURL.Hostname()
			openshiftConsoleHost = clusterAppDomain

		} else {
			// clusterAppDomain is not needed for running build-templates-e2e labeled tests, so skipping it
			if os.Getenv(constants.E2E_APPLICATIONS_NAMESPACE_ENV) == "" {
				r, err := asAdmin.CommonController.CustomClient.RouteClient().RouteV1().Routes("openshift-console").Get(context.Background(), "console", v1.GetOptions{})
				if err != nil {
					return nil, fmt.Errorf("cannot get openshift console route in order to determine cluster app domain: %+v", err)
				}
				openshiftConsoleHost = r.Spec.Host
				clusterAppDomain = strings.Join(strings.Split(openshiftConsoleHost, ".")[1:], ".")
			}

		}

		proxyAuthInfo := &sandbox.SandboxUserAuthInfo{}

		k = &kubeCl.K8SClient{
			AsKubeAdmin:     client,
			AsKubeDeveloper: client,
			ProxyUrl:        proxyAuthInfo.ProxyUrl,
			UserName:        userName,
			UserNamespace:   nsName,
			UserToken:       proxyAuthInfo.UserToken,
		}

	}

	return &Framework{
		AsKubeAdmin:          asAdmin,
		AsKubeDeveloper:      asUser,
		ClusterAppDomain:     clusterAppDomain,
		OpenshiftConsoleHost: openshiftConsoleHost,
		ProxyUrl:             k.ProxyUrl,
		SandboxController:    k.SandboxController,
		UserNamespace:        k.UserNamespace,
		UserName:             k.UserName,
		UserToken:            k.UserToken,
	}, nil
}

func InitControllerHub(cc *kubeCl.CustomClient) (*ControllerHub, error) {
	// Initialize Common controller
	commonCtrl, err := common.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	// Initialize Has controller
	hasController, err := has.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	// Initialize Tekton controller
	tektonController := tekton.NewSuiteController(cc)

	// Initialize Release Controller
	releaseController, err := release.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	// Initialize Integration Controller
	integrationController, err := integration.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	// Initialize Image Controller
	imageController, err := imagecontroller.NewSuiteController(cc)
	if err != nil {
		return nil, err
	}

	return &ControllerHub{
		HasController:         hasController,
		CommonController:      commonCtrl,
		TektonController:      tektonController,
		ReleaseController:     releaseController,
		IntegrationController: integrationController,
		ImageController:       imageController,
	}, nil
}
