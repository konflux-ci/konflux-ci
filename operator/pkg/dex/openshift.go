/*
Copyright 2025 Konflux CI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dex

import (
	"net/url"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DexClientServiceAccountName is the name of the service account used for OpenShift OAuth.
	DexClientServiceAccountName = "dex-client"
	// DexClientSecretName is the name of the secret for the dex-client service account.
	DexClientSecretName = "dex-client"
	// DexClientSecretTokenKey is the key in the secret that contains the token.
	DexClientSecretTokenKey = "token"
	// OpenShiftOAuthClientSecretEnvVar is the environment variable name for the OAuth client secret.
	OpenShiftOAuthClientSecretEnvVar = "OPENSHIFT_OAUTH_CLIENT_SECRET"
	// DexCallbackPath is the OAuth callback path used by Dex.
	DexCallbackPath = "/idp/callback"
)

// BuildOpenShiftOAuthServiceAccount creates a ServiceAccount for OpenShift OAuth integration.
// The ServiceAccount includes the oauth-redirecturi annotation that configures
// OpenShift OAuth with the exact redirect URI for the Dex callback.
// endpoint is the base URL used to construct the full redirect URI.
func BuildOpenShiftOAuthServiceAccount(namespace string, endpoint *url.URL) *corev1.ServiceAccount {
	// Build the redirect URI with the Dex callback path
	redirectURI := endpoint.JoinPath(DexCallbackPath).String()

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DexClientServiceAccountName,
			Namespace: namespace,
			Annotations: map[string]string{
				// Use direct redirect URI annotation with the full callback path
				"serviceaccounts.openshift.io/oauth-redirecturi.dex": redirectURI,
			},
		},
	}
}

// BuildOpenShiftOAuthSecret creates a Secret for the dex-client ServiceAccount.
// The secret is bound to the service account via the kubernetes.io/service-account.name annotation,
// which causes Kubernetes to automatically populate the token.
func BuildOpenShiftOAuthSecret(namespace string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DexClientSecretName,
			Namespace: namespace,
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": DexClientServiceAccountName,
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
}

// OpenShiftOAuthClientSecretEnvVar returns the environment variable that injects
// the OPENSHIFT_OAUTH_CLIENT_SECRET from the dex-client secret.
func OpenShiftOAuthClientSecretEnv() corev1.EnvVar {
	return corev1.EnvVar{
		Name: OpenShiftOAuthClientSecretEnvVar,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: DexClientSecretName,
				},
				Key: DexClientSecretTokenKey,
			},
		},
	}
}
