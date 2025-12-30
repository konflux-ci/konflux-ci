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
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
)

const (
	// dexInternalURL is the internal Dex service URL used for token redemption and JWKS.
	dexInternalURL = "https://dex.konflux-ui.svc.cluster.local:9443"
)

// --- Provider Configuration ---

// WithProvider configures the OIDC provider settings.
// Sets provider type, display name, client ID, and HTTP address.
func WithProvider() customization.ContainerOption {
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_PROVIDER", Value: "oidc"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_PROVIDER_DISPLAY_NAME", Value: "Dex OIDC"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_CLIENT_ID", Value: "oauth2-proxy"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_HTTP_ADDRESS", Value: "127.0.0.1:6000"},
	)
}

// --- OIDC URL Configuration ---

// WithOIDCURLs configures the external-facing OIDC URLs based on hostname and port.
// These URLs are used by browsers to interact with the OIDC flow.
func WithOIDCURLs(hostname, port string) customization.ContainerOption {
	baseURL := buildBaseURL(hostname, port)
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_REDIRECT_URL", Value: fmt.Sprintf("%s/oauth2/callback", baseURL)},
		corev1.EnvVar{Name: "OAUTH2_PROXY_OIDC_ISSUER_URL", Value: fmt.Sprintf("%s/idp/", baseURL)},
		corev1.EnvVar{Name: "OAUTH2_PROXY_LOGIN_URL", Value: fmt.Sprintf("%s/idp/auth", baseURL)},
	)
}

// WithInternalDexURLs configures the internal URLs for direct Dex communication.
// Uses cluster DNS to communicate with Dex for token redemption and JWKS.
func WithInternalDexURLs() customization.ContainerOption {
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_SKIP_OIDC_DISCOVERY", Value: "true"},
		corev1.EnvVar{Name: "OAUTH2_PROXY_REDEEM_URL", Value: fmt.Sprintf("%s/idp/token", dexInternalURL)},
		corev1.EnvVar{Name: "OAUTH2_PROXY_OIDC_JWKS_URL", Value: fmt.Sprintf("%s/idp/keys", dexInternalURL)},
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

// WithTLSSkipVerify configures TLS to skip certificate verification.
// This is needed for communication with internal Dex using self-signed certificates.
func WithTLSSkipVerify() customization.ContainerOption {
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_SSL_INSECURE_SKIP_VERIFY", Value: "true"},
	)
}

// --- Domain Whitelist ---

// WithWhitelistDomain configures the allowed redirect domains.
func WithWhitelistDomain(hostname, port string) customization.ContainerOption {
	domain := hostname
	if port != "" {
		domain = fmt.Sprintf("%s:%s", hostname, port)
	}
	return customization.WithEnv(
		corev1.EnvVar{Name: "OAUTH2_PROXY_WHITELIST_DOMAINS", Value: domain},
	)
}

// --- Convenience Functions ---

// --- Helper Functions ---

// buildBaseURL constructs the base URL from hostname and port.
func buildBaseURL(hostname, port string) string {
	if port != "" {
		return fmt.Sprintf("https://%s:%s", hostname, port)
	}
	return fmt.Sprintf("https://%s", hostname)
}
