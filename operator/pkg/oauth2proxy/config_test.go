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

package oauth2proxy

import (
	"net/url"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
)

// applyOption is a test helper that applies a ContainerOption and returns the resulting env vars.
func applyOption(opt customization.ContainerOption) []corev1.EnvVar {
	c := &corev1.Container{}
	opt(c, customization.DeploymentContext{})
	return c.Env
}

// envVarsToMap converts a slice of EnvVar to a map for easier testing.
func envVarsToMap(envVars []corev1.EnvVar) map[string]string {
	result := make(map[string]string)
	for _, env := range envVars {
		result[env.Name] = env.Value
	}
	return result
}

func TestWithProvider(t *testing.T) {
	g := NewGomegaWithT(t)

	envVars := applyOption(WithProvider())
	envMap := envVarsToMap(envVars)

	g.Expect(envMap["OAUTH2_PROXY_PROVIDER"]).To(Equal("oidc"))
	g.Expect(envMap["OAUTH2_PROXY_PROVIDER_DISPLAY_NAME"]).To(Equal("Dex OIDC"))
	g.Expect(envMap["OAUTH2_PROXY_CLIENT_ID"]).To(Equal("oauth2-proxy"))
	g.Expect(envMap["OAUTH2_PROXY_HTTP_ADDRESS"]).To(Equal("127.0.0.1:6000"))
	g.Expect(envMap["OAUTH2_PROXY_SKIP_PROVIDER_BUTTON"]).To(Equal("true"))
	g.Expect(envMap["OAUTH2_PROXY_PROMPT"]).To(Equal("login"))
}

func TestWithOIDCURLs(t *testing.T) {
	tests := []struct {
		name     string
		endpoint *url.URL
		want     map[string]string
	}{
		{
			name:     "with hostname and port",
			endpoint: &url.URL{Scheme: "https", Host: "example.com:9443"},
			want: map[string]string{
				"OAUTH2_PROXY_REDIRECT_URL":    "https://example.com:9443/oauth2/callback",
				"OAUTH2_PROXY_OIDC_ISSUER_URL": "https://example.com:9443/idp/",
				"OAUTH2_PROXY_LOGIN_URL":       "https://example.com:9443/idp/auth",
			},
		},
		{
			name:     "with hostname only (no port)",
			endpoint: &url.URL{Scheme: "https", Host: "konflux.example.com"},
			want: map[string]string{
				"OAUTH2_PROXY_REDIRECT_URL":    "https://konflux.example.com/oauth2/callback",
				"OAUTH2_PROXY_OIDC_ISSUER_URL": "https://konflux.example.com/idp/",
				"OAUTH2_PROXY_LOGIN_URL":       "https://konflux.example.com/idp/auth",
			},
		},
		{
			name:     "localhost default",
			endpoint: &url.URL{Scheme: "https", Host: "localhost:9443"},
			want: map[string]string{
				"OAUTH2_PROXY_REDIRECT_URL":    "https://localhost:9443/oauth2/callback",
				"OAUTH2_PROXY_OIDC_ISSUER_URL": "https://localhost:9443/idp/",
				"OAUTH2_PROXY_LOGIN_URL":       "https://localhost:9443/idp/auth",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			envVars := applyOption(WithOIDCURLs(tt.endpoint))
			envMap := envVarsToMap(envVars)

			for key, wantValue := range tt.want {
				g.Expect(envMap[key]).To(Equal(wantValue), "env var %s", key)
			}
		})
	}
}

func TestWithInternalDexURLs(t *testing.T) {
	g := NewGomegaWithT(t)

	envVars := applyOption(WithInternalDexURLs())
	envMap := envVarsToMap(envVars)

	g.Expect(envMap["OAUTH2_PROXY_SKIP_OIDC_DISCOVERY"]).To(Equal("true"))
	g.Expect(envMap["OAUTH2_PROXY_REDEEM_URL"]).To(Equal("https://dex.konflux-ui.svc.cluster.local:9443/idp/token"))
	g.Expect(envMap["OAUTH2_PROXY_OIDC_JWKS_URL"]).To(Equal("https://dex.konflux-ui.svc.cluster.local:9443/idp/keys"))
}

func TestWithCookieConfig(t *testing.T) {
	g := NewGomegaWithT(t)

	envVars := applyOption(WithCookieConfig())
	envMap := envVarsToMap(envVars)

	g.Expect(envMap["OAUTH2_PROXY_COOKIE_SECURE"]).To(Equal("true"))
	g.Expect(envMap["OAUTH2_PROXY_COOKIE_NAME"]).To(Equal("__Host-konflux-ci-cookie"))
}

func TestWithAuthSettings(t *testing.T) {
	g := NewGomegaWithT(t)

	envVars := applyOption(WithAuthSettings())
	envMap := envVarsToMap(envVars)

	g.Expect(envMap["OAUTH2_PROXY_EMAIL_DOMAINS"]).To(Equal("*"))
	g.Expect(envMap["OAUTH2_PROXY_SET_XAUTHREQUEST"]).To(Equal("true"))
	g.Expect(envMap["OAUTH2_PROXY_SKIP_JWT_BEARER_TOKENS"]).To(Equal("true"))
}

func TestWithCABundle(t *testing.T) {
	g := NewGomegaWithT(t)

	c := &corev1.Container{}
	WithCABundle()(c, customization.DeploymentContext{})

	// Check environment variable
	envMap := envVarsToMap(c.Env)
	g.Expect(envMap["OAUTH2_PROXY_PROVIDER_CA_FILES"]).To(Equal(CABundleMountPath))

	// Check volume mount
	g.Expect(c.VolumeMounts).To(HaveLen(1))
	g.Expect(c.VolumeMounts[0].Name).To(Equal(CABundleVolumeName))
	g.Expect(c.VolumeMounts[0].MountPath).To(Equal(CABundleMountDir))
	g.Expect(c.VolumeMounts[0].SubPath).To(Equal(""), "subPath should be empty to enable rotation")
	g.Expect(c.VolumeMounts[0].ReadOnly).To(BeTrue())
}

func TestWithAllowUnverifiedEmail(t *testing.T) {
	g := NewGomegaWithT(t)

	envVars := applyOption(WithAllowUnverifiedEmail())
	envMap := envVarsToMap(envVars)

	g.Expect(envMap["OAUTH2_PROXY_INSECURE_OIDC_ALLOW_UNVERIFIED_EMAIL"]).To(Equal("true"))
}

func TestWithWhitelistDomain(t *testing.T) {
	tests := []struct {
		name     string
		endpoint *url.URL
		want     string
	}{
		{
			name:     "with port",
			endpoint: &url.URL{Scheme: "https", Host: "example.com:9443"},
			want:     "example.com:9443",
		},
		{
			name:     "without port",
			endpoint: &url.URL{Scheme: "https", Host: "example.com"},
			want:     "example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			envVars := applyOption(WithWhitelistDomain(tt.endpoint))
			envMap := envVarsToMap(envVars)

			g.Expect(envMap["OAUTH2_PROXY_WHITELIST_DOMAINS"]).To(Equal(tt.want))
		})
	}
}

func TestOptionsAreComposable(t *testing.T) {
	g := NewGomegaWithT(t)

	// Test that options can be composed together
	c := &corev1.Container{}
	ctx := customization.DeploymentContext{}

	// Apply multiple options
	endpoint := &url.URL{Scheme: "https", Host: "example.com:8443"}
	WithProvider()(c, ctx)
	WithOIDCURLs(endpoint)(c, ctx)
	WithCookieConfig()(c, ctx)

	envMap := envVarsToMap(c.Env)

	// Verify provider settings
	g.Expect(envMap["OAUTH2_PROXY_PROVIDER"]).To(Equal("oidc"))
	// Verify OIDC URLs with custom port
	g.Expect(envMap["OAUTH2_PROXY_REDIRECT_URL"]).To(Equal("https://example.com:8443/oauth2/callback"))
	// Verify cookie config
	g.Expect(envMap["OAUTH2_PROXY_COOKIE_SECURE"]).To(Equal("true"))
}
