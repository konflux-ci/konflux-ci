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
// This configuration uses Kubernetes storage, HTTPS with TLS, and an oauth2-proxy client.
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
			Config: &ConnectorConfig{
				Issuer:       "https://kubernetes.default.svc",
				ClientID:     "system:serviceaccount:konflux-ui:dex-client",
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
		},
		StaticClients: []Client{
			{
				ID:        "oauth2-proxy",
				SecretEnv: "CLIENT_SECRET",
				Name:      "oauth2-proxy",
				RedirectURIs: []string{
					fmt.Sprintf("%s/oauth2/callback", baseURL),
				},
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
