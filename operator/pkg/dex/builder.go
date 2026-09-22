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
	"fmt"

	"net/url"

	"k8s.io/utils/ptr"
)

const (
	// OAuth2ProxyClientID is the Dex static client used by the UI oauth2-proxy.
	OAuth2ProxyClientID = "oauth2-proxy"
	// CLIClientID is the public Dex static client for kubectl and other OIDC CLIs.
	CLIClientID = "cli"
	// DeviceCodeGrantType is the RFC 8628 device-code grant.
	DeviceCodeGrantType = "urn:ietf:params:oauth:grant-type:device_code"
	// DeviceCallbackURI is Dex's built-in device-flow callback. Dex sends
	// redirect_uri=/device/callback after the user submits the device code.
	// A public client with any RedirectURIs set does not get Dex's implicit
	// allow for this path, so it must be listed explicitly.
	DeviceCallbackURI = "/device/callback"
)

// cliRedirectURIs are loopback callbacks for kubelogin (ports 8000 and 18000)
// plus Dex's device-flow callback.
var cliRedirectURIs = []string{
	"http://localhost:8000",
	"http://localhost:8000/",
	"http://localhost:18000",
	"http://localhost:18000/",
	"http://127.0.0.1:8000",
	"http://127.0.0.1:8000/",
	"http://127.0.0.1:18000",
	"http://127.0.0.1:18000/",
	DeviceCallbackURI,
}

// +kubebuilder:object:generate=true

// DexParams contains the configurable parameters for the Dex IdP configuration.
type DexParams struct {
	// Hostname is the external hostname for the Dex issuer (e.g., "dex.example.com").
	// If empty, the hostname is determined from the ingress configuration.
	// +optional
	Hostname string `json:"hostname,omitempty"`

	// Port is the external port for the Dex issuer (e.g., "9443").
	// If empty, the port is determined from the ingress configuration (typically empty for HTTPS on 443).
	// +optional
	Port string `json:"port,omitempty"`

	// Connectors are upstream identity provider connectors.
	// +optional
	Connectors []Connector `json:"connectors,omitempty"`

	// EnablePasswordDB enables the local password database.
	// When nil (not set), defaults to true if no connectors are configured.
	// +optional
	// +nullable
	EnablePasswordDB *bool `json:"enablePasswordDB,omitempty"`

	// StaticPasswords are predefined user credentials for the local password database.
	// +optional
	StaticPasswords []Password `json:"staticPasswords,omitempty"`

	// PasswordConnector specifies the connector ID to use for password grants (e.g., "local").
	// +optional
	PasswordConnector string `json:"passwordConnector,omitempty"`

	// ConfigureLoginWithOpenShift enables the OpenShift connector for authentication.
	// When true (or nil on OpenShift), an OpenShift connector is automatically added using the Kubernetes API.
	// Set to false to explicitly disable OpenShift login even when running on OpenShift.
	// +optional
	// +nullable
	ConfigureLoginWithOpenShift *bool `json:"configureLoginWithOpenShift,omitempty"`
}

// NewDexConfig creates a Dex configuration for the Konflux UI.
// This configuration uses Kubernetes storage, HTTPS with TLS, an oauth2-proxy
// confidential client, and a public CLI client for kubectl and other OIDC tools.
// endpoint is the base URL for the Dex issuer (e.g., https://dex.example.com).
func NewDexConfig(endpoint *url.URL, params *DexParams) *Config {
	baseURL := endpoint.String()

	defaultRedirectURI := fmt.Sprintf("%s/idp/callback", baseURL)

	// Start with provided connectors, setting default RedirectURI if not provided
	connectors := make([]Connector, len(params.Connectors))
	for i, c := range params.Connectors {
		connectors[i] = c
		// Set default RedirectURI if not explicitly provided
		if c.Config != nil && c.Config.RedirectURI == "" {
			connectors[i].Config.RedirectURI = defaultRedirectURI
		}
	}

	// Note: The controller resolves the default-on-OpenShift logic before calling this function
	if ptr.Deref(params.ConfigureLoginWithOpenShift, false) {
		openShiftConnector := Connector{
			Type: "openshift",
			ID:   "openshift",
			Name: "OpenShift",
			Config: &ConnectorConfig{ //nolint:gosec // env var placeholder, not a real credential
				Issuer:       "https://kubernetes.default.svc",
				ClientID:     "$OPENSHIFT_OAUTH_CLIENT_ID",
				ClientSecret: "$OPENSHIFT_OAUTH_CLIENT_SECRET",
				RedirectURI:  defaultRedirectURI,
				// Use the service account's CA certificate to verify the Kubernetes API server
				RootCA: "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
			},
		}
		connectors = append(connectors, openShiftConnector)
	}

	// Enable password DB if explicitly set to true,
	// or if not set and no connectors are configured
	enablePasswordDB := ptr.Deref(params.EnablePasswordDB, len(connectors) == 0)

	// Explicit list replaces Dex defaults. device_code is for headless CLIs.
	// "password" is server-wide: include it only when the local password DB
	// is on so Kind/CI ExtractToken still works, but the public CLI client
	// cannot use ROPC in connector-based deployments.
	grantTypes := []string{
		"authorization_code",
		"refresh_token",
		DeviceCodeGrantType,
	}
	if enablePasswordDB {
		grantTypes = append(grantTypes, "password")
	}

	return &Config{
		Issuer: fmt.Sprintf("%s/idp/", baseURL),
		Storage: &Storage{
			Type: "kubernetes",
			Config: &StorageConfig{
				InCluster: true,
			},
		},
		Web: &Web{
			HTTPS:   "0.0.0.0:9443",
			TLSCert: "/etc/dex/tls/tls.crt",
			TLSKey:  "/etc/dex/tls/tls.key",
		},
		OAuth2: &OAuth2{
			SkipApprovalScreen: true,
			PasswordConnector:  params.PasswordConnector,
			GrantTypes:         grantTypes,
		},
		StaticClients: []Client{
			{
				ID:        OAuth2ProxyClientID,
				SecretEnv: "CLIENT_SECRET",
				Name:      "oauth2-proxy",
				RedirectURIs: []string{
					fmt.Sprintf("%s/oauth2/callback", baseURL),
				},
			},
			{
				ID:           CLIClientID,
				Name:         "CLI",
				Public:       true,
				RedirectURIs: cliRedirectURIs,
			},
		},
		Connectors:       connectors,
		EnablePasswordDB: enablePasswordDB,
		StaticPasswords:  params.StaticPasswords,
		Telemetry: &Telemetry{
			HTTP: "0.0.0.0:5558",
		},
	}
}
