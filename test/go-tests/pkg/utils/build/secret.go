package build

import (
	"os"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetSecretDefForGitHub returns the definition of a
// Kubernetes Secret for GitHub SCM, and of type "basic-auth"
func GetSecretDefForGitHub(namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      constants.ComponentSecretName,
			Namespace: namespace,
			Labels: map[string]string{
				"appstudio.redhat.com/credentials": "scm",
				"appstudio.redhat.com/scm.host":    "github.com",
			},
		},
		Type: corev1.SecretTypeBasicAuth,
		StringData: map[string]string{
			"username": "git",
			"password": os.Getenv("GITHUB_TOKEN"),
		},
	}
}
