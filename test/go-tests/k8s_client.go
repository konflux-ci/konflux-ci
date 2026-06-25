package go_tests

import (
	"context"
	"fmt"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var (
	proxyScheme     = runtime.NewScheme()
	proxyKubeConfig *rest.Config
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(proxyScheme))
	utilruntime.Must(konfluxv1alpha1.AddToScheme(proxyScheme))
}

// NewClient builds a controller-runtime client from KUBECONFIG or ~/.kube/config.
func NewClient() (crclient.Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	proxyKubeConfig = cfg
	return crclient.New(cfg, crclient.Options{Scheme: proxyScheme})
}

func serviceProxyGet(ctx context.Context, namespace, serviceName, portName, path string) ([]byte, error) {
	if proxyKubeConfig == nil {
		return nil, fmt.Errorf("kubernetes client not initialized")
	}
	clientset, err := kubernetes.NewForConfig(proxyKubeConfig)
	if err != nil {
		return nil, err
	}
	return clientset.CoreV1().Services(namespace).
		ProxyGet("http", serviceName, portName, path, nil).
		DoRaw(ctx)
}
