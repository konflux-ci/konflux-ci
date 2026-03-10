package tekton

import (
	"context"
	"fmt"
	"os"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetTektonChainsPublicKey returns a TektonChains public key.
func (t *Controller) GetTektonChainsPublicKey() ([]byte, error) {
	namespace := constants.TEKTON_CHAINS_NS
	if os.Getenv(constants.TEST_ENVIRONMENT_ENV) == constants.UpstreamTestEnvironment {
		namespace = "tekton-pipelines"
	}
	secretName := "public-key"
	dataKey := "cosign.pub"

	secret, err := t.KubeInterface().CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("couldn't get the secret %s from %s namespace: %+v", secretName, namespace, err)
	}
	publicKey := secret.Data[dataKey]
	if len(publicKey) < 1 {
		return nil, fmt.Errorf("the content of the public key '%s' in secret %s in %s namespace is empty", dataKey, secretName, namespace)
	}
	return publicKey, err
}
