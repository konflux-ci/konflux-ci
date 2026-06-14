package go_tests

import (
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func kubeconfigPath() (string, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig != "" {
		return kubeconfig, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return homeDir + "/.kube/config", nil
}

func restConfigFromKubeconfig() (*rest.Config, error) {
	path, err := kubeconfigPath()
	if err != nil {
		return nil, err
	}
	return clientcmd.BuildConfigFromFlags("", path)
}

// CreateK8sClient builds a Kubernetes clientset from KUBECONFIG or ~/.kube/config.
func CreateK8sClient() (*kubernetes.Clientset, error) {
	config, err := restConfigFromKubeconfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}
