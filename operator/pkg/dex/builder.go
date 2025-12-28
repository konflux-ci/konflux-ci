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
)

// +kubebuilder:object:generate=true

// DexParams contains the configurable parameters for the Dex IdP configuration.
type DexParams struct {
	// Hostname is the external hostname for the Dex issuer (e.g., "dex.example.com").
	// +kubebuilder:default=localhost
	Hostname string `json:"hostname,omitempty"`

	// Port is the external port for the Dex issuer (e.g., "9443").
	// If empty, no port will be included in URLs.
	// +kubebuilder:default="9443"
	Port string `json:"port,omitempty"`

	// Connectors are upstream identity provider connectors.
	// +optional
	Connectors []Connector `json:"connectors,omitempty"`

	// EnablePasswordDB enables the local password database.
	// +optional
	// +kubebuilder:default=true
	EnablePasswordDB bool `json:"enablePasswordDB,omitempty"`

	// StaticPasswords are predefined user credentials for the local password database.
	// +optional
	StaticPasswords []Password `json:"staticPasswords,omitempty"`

	// PasswordConnector specifies the connector ID to use for password grants (e.g., "local").
	// +optional
	PasswordConnector string `json:"passwordConnector,omitempty"`

	// ConfigureLoginWithOpenShift enables the OpenShift connector for authentication.
	// When true, an OpenShift connector is automatically added using the Kubernetes API.
	// +optional
	ConfigureLoginWithOpenShift bool `json:"configureLoginWithOpenShift,omitempty"`
}

// NewDexConfig creates a Dex configuration for the Konflux UI.
// This configuration uses Kubernetes storage, HTTPS with TLS, and an oauth2-proxy client.
func NewDexConfig(params *DexParams) *Config {
	baseURL := fmt.Sprintf("https://%s", params.Hostname)
	if params.Port != "" {
		baseURL = fmt.Sprintf("https://%s:%s", params.Hostname, params.Port)
	}

	// Start with provided connectors
	connectors := params.Connectors

	// Add OpenShift connector if configured
	if params.ConfigureLoginWithOpenShift {
		openShiftConnector := Connector{
			Type: "openshift",
			ID:   "openshift",
			Name: "OpenShift",
			Config: &ConnectorConfig{
				Issuer:       "https://kubernetes.default.svc",
				ClientID:     "system:serviceaccount:konflux-ui:dex-client",
				ClientSecret: "$OPENSHIFT_OAUTH_CLIENT_SECRET",
				RedirectURI:  fmt.Sprintf("%s/idp/callback", baseURL),
			},
		}
		connectors = append(connectors, openShiftConnector)
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
		EnablePasswordDB: params.EnablePasswordDB,
		StaticPasswords:  params.StaticPasswords,
		Telemetry: &Telemetry{
			HTTP: "0.0.0.0:5558",
		},
	}
}
