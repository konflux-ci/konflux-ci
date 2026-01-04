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
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildOpenShiftOAuthServiceAccount(t *testing.T) {
	t.Run("creates service account with correct name and namespace", func(t *testing.T) {
		g := gomega.NewWithT(t)
		endpoint, _ := url.Parse("https://example.com")

		sa := BuildOpenShiftOAuthServiceAccount("konflux-ui", endpoint)

		g.Expect(sa.Name).To(gomega.Equal(DexClientServiceAccountName))
		g.Expect(sa.Namespace).To(gomega.Equal("konflux-ui"))
	})

	t.Run("includes oauth redirect uri annotation without port", func(t *testing.T) {
		g := gomega.NewWithT(t)
		endpoint, _ := url.Parse("https://example.com")

		sa := BuildOpenShiftOAuthServiceAccount("konflux-ui", endpoint)

		g.Expect(sa.Annotations).To(gomega.HaveKeyWithValue(
			"serviceaccounts.openshift.io/oauth-redirecturi.dex",
			"https://example.com/idp/callback",
		))
	})

	t.Run("includes oauth redirect uri annotation with port", func(t *testing.T) {
		g := gomega.NewWithT(t)
		endpoint, _ := url.Parse("https://example.com:9443")

		sa := BuildOpenShiftOAuthServiceAccount("konflux-ui", endpoint)

		g.Expect(sa.Annotations).To(gomega.HaveKeyWithValue(
			"serviceaccounts.openshift.io/oauth-redirecturi.dex",
			"https://example.com:9443/idp/callback",
		))
	})

	t.Run("sets correct TypeMeta", func(t *testing.T) {
		g := gomega.NewWithT(t)
		endpoint, _ := url.Parse("https://example.com")

		sa := BuildOpenShiftOAuthServiceAccount("konflux-ui", endpoint)

		g.Expect(sa.APIVersion).To(gomega.Equal("v1"))
		g.Expect(sa.Kind).To(gomega.Equal("ServiceAccount"))
	})
}

func TestBuildOpenShiftOAuthSecret(t *testing.T) {
	t.Run("creates secret with correct name and namespace", func(t *testing.T) {
		g := gomega.NewWithT(t)

		secret := BuildOpenShiftOAuthSecret("konflux-ui")

		g.Expect(secret.Name).To(gomega.Equal(DexClientSecretName))
		g.Expect(secret.Namespace).To(gomega.Equal("konflux-ui"))
	})

	t.Run("has service account token type", func(t *testing.T) {
		g := gomega.NewWithT(t)

		secret := BuildOpenShiftOAuthSecret("konflux-ui")

		g.Expect(secret.Type).To(gomega.Equal(corev1.SecretTypeServiceAccountToken))
	})

	t.Run("includes service account name annotation", func(t *testing.T) {
		g := gomega.NewWithT(t)

		secret := BuildOpenShiftOAuthSecret("konflux-ui")

		g.Expect(secret.Annotations).To(gomega.HaveKeyWithValue(
			"kubernetes.io/service-account.name",
			DexClientServiceAccountName,
		))
	})
}

func TestOpenShiftOAuthClientSecretEnv(t *testing.T) {
	t.Run("returns correct environment variable", func(t *testing.T) {
		g := gomega.NewWithT(t)

		env := OpenShiftOAuthClientSecretEnv()

		g.Expect(env.Name).To(gomega.Equal(OpenShiftOAuthClientSecretEnvVar))
	})

	t.Run("references dex-client secret", func(t *testing.T) {
		g := gomega.NewWithT(t)

		env := OpenShiftOAuthClientSecretEnv()

		g.Expect(env.ValueFrom).NotTo(gomega.BeNil())
		g.Expect(env.ValueFrom.SecretKeyRef).NotTo(gomega.BeNil())
		g.Expect(env.ValueFrom.SecretKeyRef.Name).To(gomega.Equal(DexClientSecretName))
		g.Expect(env.ValueFrom.SecretKeyRef.Key).To(gomega.Equal(DexClientSecretTokenKey))
	})
}
