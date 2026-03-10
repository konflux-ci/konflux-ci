package kube

import (
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	ecp "github.com/conforma/crds/api/v1alpha1"
	appstudioApi "github.com/konflux-ci/application-api/api/v1alpha1"
	imagecontroller "github.com/konflux-ci/image-controller/api/v1alpha1"
	integrationservicev1beta2 "github.com/konflux-ci/integration-service/api/v1beta2"
	pacv1alpha1 "github.com/openshift-pipelines/pipelines-as-code/pkg/apis/pipelinesascode/v1alpha1"
	ocpOauth "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	userv1 "github.com/openshift/api/user/v1"
	routeclientset "github.com/openshift/client-go/route/clientset/versioned"
	jvmbuildservice "github.com/redhat-appstudio/jvm-build-service/pkg/apis/jvmbuildservice/v1alpha1"
	jvmbuildserviceclientset "github.com/redhat-appstudio/jvm-build-service/pkg/client/clientset/versioned"

	release "github.com/konflux-ci/release-service/api/v1alpha1"
	tekton "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	pipelineclientset "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	DefaultRetryInterval = time.Millisecond * 100
	DefaultTimeout       = time.Second * 240
)

type CustomClient struct {
	kubeClient            *kubernetes.Clientset
	crClient              crclient.Client
	pipelineClient        pipelineclientset.Interface
	dynamicClient         dynamic.Interface
	jvmbuildserviceClient jvmbuildserviceclientset.Interface
	routeClient           routeclientset.Interface
}

type K8SClient struct {
	AsKubeAdmin   *CustomClient
	UserName      string
	UserNamespace string
}

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appstudioApi.AddToScheme(scheme))
	utilruntime.Must(ocpOauth.AddToScheme(scheme))
	utilruntime.Must(tekton.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(toolchainv1alpha1.AddToScheme(scheme))
	utilruntime.Must(release.AddToScheme(scheme))
	utilruntime.Must(integrationservicev1beta2.AddToScheme(scheme))
	utilruntime.Must(jvmbuildservice.AddToScheme(scheme))
	utilruntime.Must(ecp.AddToScheme(scheme))
	utilruntime.Must(userv1.AddToScheme(scheme))
	utilruntime.Must(imagecontroller.AddToScheme(scheme))
	utilruntime.Must(pacv1alpha1.AddToScheme(scheme))
}

func (c *CustomClient) KubeInterface() kubernetes.Interface {
	return c.kubeClient
}

func (c *CustomClient) KubeRest() crclient.Client {
	return c.crClient
}

func (c *CustomClient) PipelineClient() pipelineclientset.Interface {
	return c.pipelineClient
}

func (c *CustomClient) JvmbuildserviceClient() jvmbuildserviceclientset.Interface {
	return c.jvmbuildserviceClient
}

func (c *CustomClient) RouteClient() routeclientset.Interface {
	return c.routeClient
}

func (c *CustomClient) DynamicClient() dynamic.Interface {
	return c.dynamicClient
}

// NewAdminKubernetesClient creates a kubernetes client from default kubeconfig.
// Uses KUBECONFIG env if defined, otherwise falls back to $HOME/.kube/config.
func NewAdminKubernetesClient() (*CustomClient, error) {
	adminKubeconfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	clientSets, err := createClientSetsFromConfig(adminKubeconfig)
	if err != nil {
		return nil, err
	}

	crClient, err := crclient.New(adminKubeconfig, crclient.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}

	return &CustomClient{
		kubeClient:            clientSets.kubeClient,
		pipelineClient:        clientSets.pipelineClient,
		dynamicClient:         clientSets.dynamicClient,
		jvmbuildserviceClient: clientSets.jvmbuildserviceClient,
		routeClient:           clientSets.routeClient,
		crClient:              crClient,
	}, nil
}

func createClientSetsFromConfig(cfg *rest.Config) (*CustomClient, error) {
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	pipelineClient, err := pipelineclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	jvmbuildserviceClient, err := jvmbuildserviceclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	routeClient, err := routeclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &CustomClient{
		kubeClient:            client,
		pipelineClient:        pipelineClient,
		dynamicClient:         dynamicClient,
		jvmbuildserviceClient: jvmbuildserviceClient,
		routeClient:           routeClient,
	}, nil
}
