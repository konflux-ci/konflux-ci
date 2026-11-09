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
	"testing"

	"github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

func TestConfig_ToYAML(t *testing.T) {
	t.Run("serializes basic config", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			Issuer: "https://dex.example.com/idp/",
			Storage: &Storage{
				Type: "kubernetes",
				Config: &StorageConfig{
					InCluster: true,
				},
			},
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("issuer: https://dex.example.com/idp/"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("type: kubernetes"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("inCluster: true"))
	})

	t.Run("omits empty fields", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			Issuer: "https://dex.example.com/idp/",
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).NotTo(gomega.ContainSubstring("storage:"))
		g.Expect(string(yamlData)).NotTo(gomega.ContainSubstring("web:"))
		g.Expect(string(yamlData)).NotTo(gomega.ContainSubstring("oauth2:"))
		g.Expect(string(yamlData)).NotTo(gomega.ContainSubstring("connectors:"))
		g.Expect(string(yamlData)).NotTo(gomega.ContainSubstring("staticClients:"))
		g.Expect(string(yamlData)).NotTo(gomega.ContainSubstring("enablePasswordDB:"))
	})

	t.Run("includes enablePasswordDB when true", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			Issuer:           "https://dex.example.com/idp/",
			EnablePasswordDB: true,
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("enablePasswordDB: true"))
	})

	t.Run("serializes static clients", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			Issuer: "https://dex.example.com/idp/",
			StaticClients: []Client{
				{
					ID:        "my-client",
					SecretEnv: "CLIENT_SECRET",
					Name:      "My Client",
					RedirectURIs: []string{
						"https://app.example.com/callback",
					},
				},
			},
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("staticClients:"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("id: my-client"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("secretEnv: CLIENT_SECRET"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("https://app.example.com/callback"))
	})

	t.Run("serializes connectors", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			Issuer: "https://dex.example.com/idp/",
			Connectors: []Connector{
				{
					Type: "openshift",
					ID:   "openshift",
					Name: "OpenShift",
					Config: &ConnectorConfig{
						Issuer:       "https://kubernetes.default.svc",
						ClientID:     "my-client-id",
						ClientSecret: "$SECRET",
						RedirectURI:  "https://dex.example.com/callback",
					},
				},
			},
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("connectors:"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("type: openshift"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("id: openshift"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("issuer: https://kubernetes.default.svc"))
	})

	t.Run("serializes static passwords", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			Issuer:           "https://dex.example.com/idp/",
			EnablePasswordDB: true,
			StaticPasswords: []Password{
				{
					Email:    "admin@example.com",
					Hash:     "$2a$10$abcdef",
					Username: "admin",
					UserID:   "admin-id",
				},
			},
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("staticPasswords:"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("email: admin@example.com"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("username: admin"))
	})

	t.Run("roundtrip serialization", func(t *testing.T) {
		g := gomega.NewWithT(t)

		original := &Config{
			Issuer: "https://dex.example.com/idp/",
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
			},
			StaticClients: []Client{
				{
					ID:           "oauth2-proxy",
					SecretEnv:    "CLIENT_SECRET",
					Name:         "oauth2-proxy",
					RedirectURIs: []string{"https://example.com/callback"},
				},
			},
			Telemetry: &Telemetry{
				HTTP: "0.0.0.0:5558",
			},
		}

		yamlData, err := original.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Deserialize back
		var restored Config
		err = yaml.Unmarshal(yamlData, &restored)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(restored.Issuer).To(gomega.Equal(original.Issuer))
		g.Expect(restored.Storage.Type).To(gomega.Equal(original.Storage.Type))
		g.Expect(restored.Storage.Config.InCluster).To(gomega.Equal(original.Storage.Config.InCluster))
		g.Expect(restored.Web.HTTPS).To(gomega.Equal(original.Web.HTTPS))
		g.Expect(restored.OAuth2.SkipApprovalScreen).To(gomega.Equal(original.OAuth2.SkipApprovalScreen))
		g.Expect(restored.StaticClients).To(gomega.HaveLen(1))
		g.Expect(restored.StaticClients[0].ID).To(gomega.Equal("oauth2-proxy"))
	})
}

func TestWeb_Serialization(t *testing.T) {
	t.Run("serializes TLS configuration", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			Web: &Web{
				HTTPS:   "0.0.0.0:9443",
				TLSCert: "/etc/dex/tls/tls.crt",
				TLSKey:  "/etc/dex/tls/tls.key",
			},
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("https: 0.0.0.0:9443"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("tlsCert: /etc/dex/tls/tls.crt"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("tlsKey: /etc/dex/tls/tls.key"))
	})

	t.Run("serializes allowed origins", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			Web: &Web{
				HTTP: "0.0.0.0:5556",
				AllowedOrigins: []string{
					"https://app1.example.com",
					"https://app2.example.com",
				},
			},
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("allowedOrigins:"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("https://app1.example.com"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("https://app2.example.com"))
	})
}

func TestOAuth2_Serialization(t *testing.T) {
	t.Run("serializes password connector", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			OAuth2: &OAuth2{
				SkipApprovalScreen: true,
				PasswordConnector:  "local",
			},
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("skipApprovalScreen: true"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("passwordConnector: local"))
	})

	t.Run("serializes response types", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			OAuth2: &OAuth2{
				ResponseTypes: []string{"code", "token", "id_token"},
			},
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("responseTypes:"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("code"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("token"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("id_token"))
	})
}

func TestConnector_Serialization(t *testing.T) {
	t.Run("serializes LDAP connector", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			Connectors: []Connector{
				{
					Type: "ldap",
					ID:   "ldap",
					Name: "LDAP",
					Config: &ConnectorConfig{
						Host:               "ldap.example.com:636",
						InsecureSkipVerify: true,
						BindDN:             "cn=admin,dc=example,dc=com",
						BindPW:             "$LDAP_PASSWORD",
						UserSearch: &LDAPUserSearch{
							BaseDN:    "ou=users,dc=example,dc=com",
							Filter:    "(objectClass=person)",
							Username:  "uid",
							IDAttr:    "uid",
							EmailAttr: "mail",
							NameAttr:  "cn",
						},
						GroupSearch: &LDAPGroupSearch{
							BaseDN:   "ou=groups,dc=example,dc=com",
							Filter:   "(objectClass=groupOfNames)",
							NameAttr: "cn",
							UserMatchers: []LDAPUserMatcher{
								{
									UserAttr:  "DN",
									GroupAttr: "member",
								},
							},
						},
					},
				},
			},
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("type: ldap"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("host: ldap.example.com:636"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("userSearch:"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("groupSearch:"))
	})

	t.Run("serializes GitHub connector", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			Connectors: []Connector{
				{
					Type: "github",
					ID:   "github",
					Name: "GitHub",
					Config: &ConnectorConfig{
						ClientID:     "github-client-id",
						ClientSecret: "$GITHUB_SECRET",
						RedirectURI:  "https://dex.example.com/callback",
						Orgs: []GitHubOrg{
							{
								Name:  "my-org",
								Teams: []string{"team1", "team2"},
							},
						},
					},
				},
			},
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("type: github"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("orgs:"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("name: my-org"))
		g.Expect(string(yamlData)).To(gomega.ContainSubstring("teams:"))
	})

	t.Run("serializes the Dex OIDC connector options", func(t *testing.T) {
		g := gomega.NewWithT(t)

		basicAuthUnsupported := false
		original := &Config{
			Connectors: []Connector{
				{
					Type: "oidc",
					ID:   "rh-sso",
					Name: "Red Hat SSO",
					Config: &ConnectorConfig{
						Issuer:                    "https://sso.example.com/auth/realms/example",
						IssuerAlias:               "https://sso.example.com/auth/realms/example",
						ClientID:                  "konflux",
						ClientSecret:              "$OIDC_CLIENT_SECRET",
						RedirectURI:               "https://konflux.example.com/idp/callback",
						BasicAuthUnsupported:      &basicAuthUnsupported,
						Scopes:                    []string{"openid", "profile", "email", "groups"},
						InsecureSkipEmailVerified: true,
						InsecureEnableGroups:      true,
						AllowedGroups:             []string{"engineering"},
						GetUserInfo:               true,
						UserIDKey:                 "sub",
						UserNameKey:               "preferred_username",
						AcrValues:                 []string{"phr"},
						PromptType:                "login",
						ClaimMapping: &OIDCClaimMapping{
							PreferredUsername: "preferred_username",
							Email:             "mail",
							Groups:            "groups",
						},
						ClaimModifications: &OIDCClaimModifications{
							NewGroupFromClaims: []OIDCNewGroupFromClaims{{
								Prefix:         "example",
								Delimiter:      "::",
								ClearDelimiter: true,
								Claims:         []string{"organization", "email"},
							}},
							FilterGroupClaims: &OIDCFilterGroupClaims{
								GroupsFilter: "^konflux-.*",
							},
							ModifyGroupNames: &OIDCModifyGroupNames{
								Prefix: "konflux-",
								Suffix: "-users",
							},
						},
						OverrideClaimMapping: true,
						ProviderDiscoveryOverrides: &OIDCProviderDiscoveryOverrides{
							TokenURL: "https://sso.example.com/token",
							AuthURL:  "https://sso.example.com/auth",
						},
						RootCA:             "/etc/ssl/certs/ca.crt",
						InsecureSkipVerify: true,
					},
				},
			},
		}

		yamlData, err := original.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rendered := string(yamlData)
		// Keys must match Dex's config file, including claim names Dex does not camel-case.
		for _, key := range []string{
			"issuerAlias:",
			"basicAuthUnsupported: false",
			"scopes:",
			"insecureSkipEmailVerified: true",
			"insecureEnableGroups: true",
			"allowedGroups:",
			"getUserInfo: true",
			"userIDKey: sub",
			"userNameKey: preferred_username",
			"acrValues:",
			"promptType: login",
			"preferred_username: preferred_username",
			"claimModifications:",
			"newGroupFromClaims:",
			"clearDelimiter: true",
			"groupsFilter: ^konflux-.*",
			"modifyGroupNames:",
			"prefix: konflux-",
			"overrideClaimMapping: true",
			"providerDiscoveryOverrides:",
			"tokenURL: https://sso.example.com/token",
			"authURL: https://sso.example.com/auth",
			"rootCA: /etc/ssl/certs/ca.crt",
			"insecureSkipVerify: true",
		} {
			g.Expect(rendered).To(gomega.ContainSubstring(key), "rendered Dex config missing %q", key)
		}

		var restored Config
		err = yaml.Unmarshal(yamlData, &restored)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(restored).To(gomega.Equal(*original))
	})

	t.Run("omits unset OIDC connector options", func(t *testing.T) {
		g := gomega.NewWithT(t)

		config := &Config{
			Connectors: []Connector{
				{
					Type: "oidc",
					ID:   "google",
					Name: "Google",
					Config: &ConnectorConfig{
						ClientID:     "google-client",
						ClientSecret: "$GOOGLE_SECRET",
						Issuer:       "https://accounts.google.com",
					},
				},
			},
		}

		yamlData, err := config.ToYAML()
		g.Expect(err).NotTo(gomega.HaveOccurred())
		rendered := string(yamlData)
		for _, key := range []string{
			"issuerAlias:",
			"basicAuthUnsupported:",
			"scopes:",
			"insecureSkipEmailVerified:",
			"insecureEnableGroups:",
			"allowedGroups:",
			"getUserInfo:",
			"userIDKey:",
			"userNameKey:",
			"acrValues:",
			"promptType:",
			"claimMapping:",
			"claimModifications:",
			"overrideClaimMapping:",
			"providerDiscoveryOverrides:",
		} {
			g.Expect(rendered).NotTo(gomega.ContainSubstring(key), "unset OIDC option %q was rendered", key)
		}
	})
}
