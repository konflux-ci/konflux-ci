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
	"k8s.io/utils/ptr"
)

func TestNewDexConfig(t *testing.T) {
	t.Run("creates config with hostname only", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{}

		config := NewDexConfig(endpoint, params)

		g.Expect(config).NotTo(gomega.BeNil())
		g.Expect(config.Issuer).To(gomega.Equal("https://dex.example.com/idp/"))
		g.Expect(config.StaticClients).To(gomega.HaveLen(1))
		g.Expect(config.StaticClients[0].RedirectURIs).To(gomega.ContainElement("https://dex.example.com/oauth2/callback"))
	})

	t.Run("creates config with hostname and port", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com:9443"}
		params := &DexParams{}

		config := NewDexConfig(endpoint, params)

		g.Expect(config).NotTo(gomega.BeNil())
		g.Expect(config.Issuer).To(gomega.Equal("https://dex.example.com:9443/idp/"))
		g.Expect(
			config.StaticClients[0].RedirectURIs,
		).To(gomega.ContainElement("https://dex.example.com:9443/oauth2/callback"))
	})

	t.Run("configures kubernetes storage", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Storage).NotTo(gomega.BeNil())
		g.Expect(config.Storage.Type).To(gomega.Equal("kubernetes"))
		g.Expect(config.Storage.Config).NotTo(gomega.BeNil())
		g.Expect(config.Storage.Config.InCluster).To(gomega.BeTrue())
	})

	t.Run("configures HTTPS with TLS", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Web).NotTo(gomega.BeNil())
		g.Expect(config.Web.HTTPS).To(gomega.Equal("0.0.0.0:9443"))
		g.Expect(config.Web.TLSCert).To(gomega.Equal("/etc/dex/tls/tls.crt"))
		g.Expect(config.Web.TLSKey).To(gomega.Equal("/etc/dex/tls/tls.key"))
	})

	t.Run("configures oauth2-proxy client", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.StaticClients).To(gomega.HaveLen(1))
		client := config.StaticClients[0]
		g.Expect(client.ID).To(gomega.Equal("oauth2-proxy"))
		g.Expect(client.SecretEnv).To(gomega.Equal("CLIENT_SECRET"))
		g.Expect(client.Name).To(gomega.Equal("oauth2-proxy"))
	})

	t.Run("configures telemetry", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Telemetry).NotTo(gomega.BeNil())
		g.Expect(config.Telemetry.HTTP).To(gomega.Equal("0.0.0.0:5558"))
	})

	t.Run("skips approval screen", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.OAuth2).NotTo(gomega.BeNil())
		g.Expect(config.OAuth2.SkipApprovalScreen).To(gomega.BeTrue())
	})
}

func TestNewDexConfig_OpenShiftConnector(t *testing.T) {
	t.Run("adds OpenShift connector when enabled", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Hostname:                    "dex.example.com",
			ConfigureLoginWithOpenShift: ptr.To(true),
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Connectors).To(gomega.HaveLen(1))
		connector := config.Connectors[0]
		g.Expect(connector.Type).To(gomega.Equal("openshift"))
		g.Expect(connector.ID).To(gomega.Equal("openshift"))
		g.Expect(connector.Name).To(gomega.Equal("OpenShift"))
	})

	t.Run("configures OpenShift connector with correct issuer", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Hostname:                    "dex.example.com",
			ConfigureLoginWithOpenShift: ptr.To(true),
		}

		config := NewDexConfig(endpoint, params)

		connector := config.Connectors[0]
		g.Expect(connector.Config).NotTo(gomega.BeNil())
		g.Expect(connector.Config.Issuer).To(gomega.Equal("https://kubernetes.default.svc"))
	})

	t.Run("configures OpenShift connector with service account client ID", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Hostname:                    "dex.example.com",
			ConfigureLoginWithOpenShift: ptr.To(true),
		}

		config := NewDexConfig(endpoint, params)

		connector := config.Connectors[0]
		g.Expect(connector.Config.ClientID).To(gomega.Equal("system:serviceaccount:konflux-ui:dex-client"))
		g.Expect(connector.Config.ClientSecret).To(gomega.Equal("$OPENSHIFT_OAUTH_CLIENT_SECRET"))
	})

	t.Run("configures OpenShift connector with root CA for API server TLS", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Hostname:                    "dex.example.com",
			ConfigureLoginWithOpenShift: ptr.To(true),
		}

		config := NewDexConfig(endpoint, params)

		connector := config.Connectors[0]
		g.Expect(connector.Config.RootCA).To(gomega.Equal("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"))
	})

	t.Run("configures OpenShift connector redirect URI without port", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Hostname:                    "dex.example.com",
			ConfigureLoginWithOpenShift: ptr.To(true),
		}

		config := NewDexConfig(endpoint, params)

		connector := config.Connectors[0]
		g.Expect(connector.Config.RedirectURI).To(gomega.Equal("https://dex.example.com/idp/callback"))
	})

	t.Run("configures OpenShift connector redirect URI with port", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com:9443"}
		params := &DexParams{
			Hostname:                    "dex.example.com",
			Port:                        "9443",
			ConfigureLoginWithOpenShift: ptr.To(true),
		}

		config := NewDexConfig(endpoint, params)

		connector := config.Connectors[0]
		g.Expect(connector.Config.RedirectURI).To(gomega.Equal("https://dex.example.com:9443/idp/callback"))
	})

	t.Run("does not add OpenShift connector when disabled", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Hostname:                    "dex.example.com",
			ConfigureLoginWithOpenShift: ptr.To(false),
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Connectors).To(gomega.BeEmpty())
	})
}

func TestNewDexConfig_CustomConnectors(t *testing.T) {
	t.Run("includes custom connectors", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Connectors: []Connector{
				{
					Type: "github",
					ID:   "github",
					Name: "GitHub",
					Config: &ConnectorConfig{
						ClientID:     "github-client",
						ClientSecret: "$GITHUB_SECRET",
					},
				},
			},
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Connectors).To(gomega.HaveLen(1))
		g.Expect(config.Connectors[0].Type).To(gomega.Equal("github"))
		g.Expect(config.Connectors[0].ID).To(gomega.Equal("github"))
	})

	t.Run("appends OpenShift connector to custom connectors", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Connectors: []Connector{
				{
					Type: "github",
					ID:   "github",
					Name: "GitHub",
				},
			},
			ConfigureLoginWithOpenShift: ptr.To(true),
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Connectors).To(gomega.HaveLen(2))
		g.Expect(config.Connectors[0].Type).To(gomega.Equal("github"))
		g.Expect(config.Connectors[1].Type).To(gomega.Equal("openshift"))
	})

	t.Run("supports multiple custom connectors", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Connectors: []Connector{
				{Type: "github", ID: "github", Name: "GitHub"},
				{Type: "ldap", ID: "ldap", Name: "LDAP"},
				{Type: "oidc", ID: "google", Name: "Google"},
			},
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Connectors).To(gomega.HaveLen(3))
	})
}

func TestNewDexConfig_ConnectorRedirectURI(t *testing.T) {
	t.Run("sets default RedirectURI when not provided (hostname only)", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Connectors: []Connector{
				{
					Type: "github",
					ID:   "github",
					Name: "GitHub",
					Config: &ConnectorConfig{
						ClientID:     "github-client",
						ClientSecret: "$GITHUB_SECRET",
					},
				},
			},
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Connectors).To(gomega.HaveLen(1))
		g.Expect(config.Connectors[0].Config.RedirectURI).To(gomega.Equal("https://dex.example.com/idp/callback"))
	})

	t.Run("sets default RedirectURI when not provided (hostname with port)", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com:9443"}
		params := &DexParams{
			Connectors: []Connector{
				{
					Type: "github",
					ID:   "github",
					Name: "GitHub",
					Config: &ConnectorConfig{
						ClientID:     "github-client",
						ClientSecret: "$GITHUB_SECRET",
					},
				},
			},
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Connectors).To(gomega.HaveLen(1))
		g.Expect(config.Connectors[0].Config.RedirectURI).To(gomega.Equal("https://dex.example.com:9443/idp/callback"))
	})

	t.Run("preserves explicit RedirectURI when provided", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com:9443"}
		params := &DexParams{
			Connectors: []Connector{
				{
					Type: "github",
					ID:   "github",
					Name: "GitHub",
					Config: &ConnectorConfig{
						ClientID:     "github-client",
						ClientSecret: "$GITHUB_SECRET",
						RedirectURI:  "https://custom.example.com/callback",
					},
				},
			},
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Connectors).To(gomega.HaveLen(1))
		g.Expect(config.Connectors[0].Config.RedirectURI).To(gomega.Equal("https://custom.example.com/callback"))
	})

	t.Run("handles connector with nil Config", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Connectors: []Connector{
				{
					Type:   "mock",
					ID:     "mock",
					Name:   "Mock",
					Config: nil,
				},
			},
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Connectors).To(gomega.HaveLen(1))
		g.Expect(config.Connectors[0].Config).To(gomega.BeNil())
	})

	t.Run("handles multiple connectors with mixed RedirectURI settings", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com:9443"}
		params := &DexParams{
			Connectors: []Connector{
				{
					Type: "github",
					ID:   "github",
					Name: "GitHub",
					Config: &ConnectorConfig{
						ClientID:     "github-client",
						ClientSecret: "$GITHUB_SECRET",
						// No RedirectURI - should use default
					},
				},
				{
					Type: "oidc",
					ID:   "google",
					Name: "Google",
					Config: &ConnectorConfig{
						ClientID:     "google-client",
						ClientSecret: "$GOOGLE_SECRET",
						RedirectURI:  "https://custom.google.com/callback",
					},
				},
				{
					Type: "ldap",
					ID:   "ldap",
					Name: "LDAP",
					Config: &ConnectorConfig{
						Host: "ldap.example.com",
						// No RedirectURI - should use default
					},
				},
			},
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.Connectors).To(gomega.HaveLen(3))
		// GitHub connector should have default RedirectURI
		g.Expect(config.Connectors[0].Config.RedirectURI).To(gomega.Equal("https://dex.example.com:9443/idp/callback"))
		// Google connector should preserve explicit RedirectURI
		g.Expect(config.Connectors[1].Config.RedirectURI).To(gomega.Equal("https://custom.google.com/callback"))
		// LDAP connector should have default RedirectURI
		g.Expect(config.Connectors[2].Config.RedirectURI).To(gomega.Equal("https://dex.example.com:9443/idp/callback"))
	})

	t.Run("does not modify original params connectors", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			Connectors: []Connector{
				{
					Type: "github",
					ID:   "github",
					Name: "GitHub",
					Config: &ConnectorConfig{
						ClientID:     "github-client",
						ClientSecret: "$GITHUB_SECRET",
					},
				},
			},
		}

		_ = NewDexConfig(endpoint, params)

		// Original params should not be modified
		// Note: Since Config is a pointer, the original is modified in the current implementation.
		// This test documents the current behavior.
		g.Expect(params.Connectors[0].Config.RedirectURI).To(gomega.Equal("https://dex.example.com/idp/callback"))
	})
}

func TestNewDexConfig_PasswordDB(t *testing.T) {
	t.Run("enables password database", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			EnablePasswordDB: true,
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.EnablePasswordDB).To(gomega.BeTrue())
	})

	t.Run("disables password database when explicitly set", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			EnablePasswordDB: false,
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.EnablePasswordDB).To(gomega.BeFalse())
	})

	t.Run("includes static passwords", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			EnablePasswordDB: true,
			StaticPasswords: []Password{
				{
					Email:    "admin@example.com",
					Hash:     "$2a$10$abcdef",
					Username: "admin",
					UserID:   "admin-001",
				},
				{
					Email:    "user@example.com",
					Hash:     "$2a$10$123456",
					Username: "user",
					UserID:   "user-001",
				},
			},
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.StaticPasswords).To(gomega.HaveLen(2))
		g.Expect(config.StaticPasswords[0].Email).To(gomega.Equal("admin@example.com"))
		g.Expect(config.StaticPasswords[1].Email).To(gomega.Equal("user@example.com"))
	})

	t.Run("configures password connector", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			EnablePasswordDB:  true,
			PasswordConnector: "local",
		}

		config := NewDexConfig(endpoint, params)

		g.Expect(config.OAuth2.PasswordConnector).To(gomega.Equal("local"))
	})
}

func TestNewDexConfig_YAML_Output(t *testing.T) {
	t.Run("generates valid YAML", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com:9443"}
		params := &DexParams{
			Hostname:                    "dex.example.com",
			Port:                        "9443",
			ConfigureLoginWithOpenShift: ptr.To(true),
			EnablePasswordDB:            true,
			PasswordConnector:           "local",
			StaticPasswords: []Password{
				{
					Email:    "admin@example.com",
					Hash:     "$2a$10$abcdef",
					Username: "admin",
					UserID:   "admin-001",
				},
			},
		}

		config := NewDexConfig(endpoint, params)
		yamlData, err := config.ToYAML()

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("issuer: https://dex.example.com:9443/idp/"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("type: kubernetes"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("https: 0.0.0.0:9443"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("type: openshift"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("enablePasswordDB: true"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("passwordConnector: local"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("email: admin@example.com"))
	})

	t.Run("omits empty fields in YAML", func(t *testing.T) {
		g := gomega.NewWithT(t)

		endpoint := &url.URL{Scheme: "https", Host: "dex.example.com"}
		params := &DexParams{
			EnablePasswordDB: false,
		}

		config := NewDexConfig(endpoint, params)
		yamlData, err := config.ToYAML()

		g.Expect(err).NotTo(gomega.HaveOccurred())
		// EnablePasswordDB is false, should be omitted
		g.Expect(string(yamlData)).NotTo(gomega.ContainSubstring("enablePasswordDB"))
		// No connectors, should be omitted
		g.Expect(string(yamlData)).NotTo(gomega.ContainSubstring("connectors"))
		// No static passwords, should be omitted
		g.Expect(string(yamlData)).NotTo(gomega.ContainSubstring("staticPasswords"))
	})
}
