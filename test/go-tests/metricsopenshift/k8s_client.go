package metricsopenshift

import (
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var (
	kubeScheme = runtime.NewScheme()
	kubeREST   *rest.Config
	kubeClient crclient.Client
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(kubeScheme))
	utilruntime.Must(rbacv1.AddToScheme(kubeScheme))
}

func initKubernetesClient() error {
	cfg, err := config.GetConfig()
	if err != nil {
		return err
	}
	kubeREST = cfg
	kubeClient, err = crclient.New(cfg, crclient.Options{Scheme: kubeScheme})
	return err
}
