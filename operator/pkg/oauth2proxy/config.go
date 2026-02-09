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

// Package oauth2proxy provides configuration options for the oauth2-proxy container.
// Configuration is done via environment variables as per:
// https://oauth2-proxy.github.io/oauth2-proxy/configuration/overview/#environment-variables
//
// Options are designed to be composable using the customization.ContainerOption pattern,
// allowing flexible configuration based on deployment needs.
package oauth2proxy

import (
	"net/url"

	corev1 "k8s.io/api/core/v1"

	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
)

const (
	// CABundleVolumeName is the name of the volume containing the CA bundle
	CABundleVolumeName = "ca-bundle"
	// CABundleSecretName is the name of the Secret containing the CA bundle
	CABundleSecretName = "oauth2-proxy-cert"
	// CABundleSecretKey is the key in the Secret containing the CA certificate
	CABundleSecretKey = "ca.crt"
	// CABundleFilename is the filename used in the projected volume for the CA certificate
	CABundleFilename = "ca-bundle.crt"
	// CABundleMountDir is the directory where the CA bundle is mounted
	CABundleMountDir = "/etc/ssl/certs"
	// CABundleMountPath is the full path where the CA bundle file is located in the container
	// The file is projected into a directory mount to enable automatic rotation
	CABundleMountPath = CABundleMountDir + "/" + CABundleFilename
)

var (
	// dexInternalURL is the internal Dex service URL used for token redemption and JWKS.
	dexInternalURL = &url.URL{
		Scheme: "https",
		Host:   "dex.konflux-ui.svc.cluster.local:9443",
	}
)

// --- Provider Configuration ---

// WithProvider configures the OIDC provider settings.
// Sets provider type, display name, client ID, HTTP address, skips the provider button,
// and sets the OIDC prompt to "login".
func WithProvider() customization.ContainerOption {
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_PROVIDER", Value: "oidc"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_PROVIDER_DISPLAY_NAME", Value: "Dex OIDC"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_CLIENT_ID", Value: "oauth2-proxy"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_HTTP_ADDRESS", Value: "127.0.0.1:6000"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_SKIP_PROVIDER_BUTTON", Value: "true"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_PROMPT", Value: "login"},
	)
}

// --- OIDC URL Configuration ---

// WithOIDCURLs configures the external-facing OIDC URLs based on the endpoint URL.
// These URLs are used by browsers to interact with the OIDC flow.
func WithOIDCURLs(endpoint *url.URL) customization.ContainerOption {
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_REDIRECT_URL", Value: endpoint.JoinPath("/oauth2/callback").String()},
		corev1.EnvVar{Name: "OAUTH2_PROXY_OIDC_ISSUER_URL", Value: endpoint.JoinPath("/idp/").String()},
		corev1.EnvVar{Name: "OAUTH2_PROXY_LOGIN_URL", Value: endpoint.JoinPath("/idp/auth").String()},
	)
}

// WithInternalDexURLs configures the internal URLs for direct Dex communication.
// Uses cluster DNS to communicate with Dex for token redemption and JWKS.
func WithInternalDexURLs() customization.ContainerOption {
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_SKIP_OIDC_DISCOVERY", Value: "true"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_REDEEM_URL", Value: dexInternalURL.JoinPath("/idp/token").String()},
		corev1.EnvVar{Name: "OAUTH2_PROXY_OIDC_JWKS_URL", Value: dexInternalURL.JoinPath("/idp/keys").String()},
	)
}

// --- Cookie Configuration ---

// WithCookieConfig configures cookie settings for session management.
func WithCookieConfig() customization.ContainerOption {
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_COOKIE_SECURE", Value: "true"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_COOKIE_NAME", Value: "__Host-konflux-ci-cookie"},
	)
}

// --- Authentication Settings ---

// WithAuthSettings configures authentication behavior.
// Sets email domain restrictions, X-Auth-Request header, and JWT handling.
func WithAuthSettings() customization.ContainerOption {
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_EMAIL_DOMAINS", Value: "*"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_SET_XAUTHREQUEST", Value: "true"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_SKIP_JWT_BEARER_TOKENS", Value: "true"},
	)
}

// --- TLS Configuration ---

// WithCABundle configures TLS to use a custom CA bundle for certificate verification.
// Adds the volume mount and environment variable for the CA bundle.
func WithCABundle() customization.ContainerOption {
	return func(c *corev1.Container, ctx customization.DeploymentContext) {
		// Add volume mount for CA bundle.
		// Mount to directory (not subPath) to enable automatic certificate rotation.
		// The projected volume ensures only ca.crt is accessible (not tls.key).
		customization.WithVolumeMounts(corev1.VolumeMount{
			Name:      CABundleVolumeName,
			MountPath: CABundleMountDir,
			ReadOnly:  true,
		})(c, ctx)
		// Set OAUTH2_PROXY_PROVIDER_CA_FILES to trust the CA for OIDC provider (Dex) connections.
		// Points to the specific file within the mounted directory.
		customization.WithEnv(
			corev1.EnvVar{Name: "OAUTH2_PROXY_PROVIDER_CA_FILES", Value: CABundleMountPath},
		)(c, ctx)
	}
}

// --- Email Verification ---

// WithAllowUnverifiedEmail configures oauth2-proxy to allow unverified emails.
// This is needed when using identity providers like OpenShift that may not return
// email verification information.
func WithAllowUnverifiedEmail() customization.ContainerOption {
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_INSECURE_OIDC_ALLOW_UNVERIFIED_EMAIL", Value: "true"},
	)
}

// --- Domain Whitelist ---

// WithWhitelistDomain configures the allowed redirect domains.
func WithWhitelistDomain(endpoint *url.URL) customization.ContainerOption {
	// endpoint.Host contains "hostname:port" or just "hostname" if port is default
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_WHITELIST_DOMAINS", Value: endpoint.Host},
	)
}
