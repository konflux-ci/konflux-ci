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

// Package dex provides types and utilities for generating Dex IdP configuration.
package dex

import (
	"sigs.k8s.io/yaml"
)

// Config is the configuration format for Dex.
// This struct mirrors the configuration file format used by Dex.
type Config struct {
	// Issuer is the base path of Dex and the external name of the OpenID Connect service.
	Issuer string `json:"issuer,omitempty"`

	// Storage configures the backend storage for Dex.
	Storage *Storage `json:"storage,omitempty"`

	// Web configures the HTTP(S) server.
	Web *Web `json:"web,omitempty"`

	// Telemetry configures the telemetry/metrics server.
	Telemetry *Telemetry `json:"telemetry,omitempty"`

	// OAuth2 configures OAuth2 settings.
	OAuth2 *OAuth2 `json:"oauth2,omitempty"`

	// GRPC configures the gRPC API.
	GRPC *GRPC `json:"grpc,omitempty"`

	// Expiry configures token expiration settings.
	Expiry *Expiry `json:"expiry,omitempty"`

	// Logger configures logging settings.
	Logger *Logger `json:"logger,omitempty"`

	// Connectors are used to authenticate users against upstream identity providers.
	Connectors []Connector `json:"connectors,omitempty"`

	// StaticClients are predefined OAuth2 clients.
	StaticClients []Client `json:"staticClients,omitempty"`

	// EnablePasswordDB enables the local password database.
	EnablePasswordDB bool `json:"enablePasswordDB,omitempty"`

	// StaticPasswords are predefined user credentials for the local password database.
	StaticPasswords []Password `json:"staticPasswords,omitempty"`
}

// Storage configures the backend storage for Dex.
type Storage struct {
	// Type specifies the storage backend type (e.g., "kubernetes", "memory", "sqlite3", "postgres").
	Type string `json:"type,omitempty"`

	// Config contains storage-specific configuration.
	Config *StorageConfig `json:"config,omitempty"`
}

// StorageConfig contains storage-specific configuration options.
type StorageConfig struct {
	// InCluster indicates whether Dex is running inside a Kubernetes cluster.
	// Used for Kubernetes storage type.
	InCluster bool `json:"inCluster,omitempty"`
}

// Web configures the HTTP(S) server settings.
type Web struct {
	// HTTP is the address to listen on for HTTP requests (e.g., "0.0.0.0:5556").
	HTTP string `json:"http,omitempty"`

	// HTTPS is the address to listen on for HTTPS requests (e.g., "0.0.0.0:5554").
	HTTPS string `json:"https,omitempty"`

	// TLSCert is the path to the TLS certificate file.
	TLSCert string `json:"tlsCert,omitempty"`

	// TLSKey is the path to the TLS private key file.
	TLSKey string `json:"tlsKey,omitempty"`

	// AllowedOrigins is a list of allowed origins for CORS requests.
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`
}

// Telemetry configures the telemetry/metrics server.
type Telemetry struct {
	// HTTP is the address to listen on for telemetry requests (e.g., "0.0.0.0:5558").
	HTTP string `json:"http,omitempty"`
}

// OAuth2 configures OAuth2 settings.
type OAuth2 struct {
	// ResponseTypes specifies the allowed OAuth2 response types.
	ResponseTypes []string `json:"responseTypes,omitempty"`

	// SkipApprovalScreen skips the user approval screen during authorization.
	SkipApprovalScreen bool `json:"skipApprovalScreen,omitempty"`

	// PasswordConnector specifies the connector ID to use for password grants.
	PasswordConnector string `json:"passwordConnector,omitempty"`
}

// GRPC configures the gRPC API.
type GRPC struct {
	// Addr is the address to listen on for gRPC requests (e.g., "0.0.0.0:5557").
	Addr string `json:"addr,omitempty"`

	// TLSCert is the path to the TLS certificate file for gRPC.
	TLSCert string `json:"tlsCert,omitempty"`

	// TLSKey is the path to the TLS private key file for gRPC.
	TLSKey string `json:"tlsKey,omitempty"`

	// TLSClientCA is the path to the CA certificate for client certificate authentication.
	TLSClientCA string `json:"tlsClientCA,omitempty"`
}

// Expiry configures token expiration settings.
type Expiry struct {
	// IDTokens specifies the duration for which ID tokens are valid (e.g., "24h").
	IDTokens string `json:"idTokens,omitempty"`

	// SigningKeys specifies the duration for which signing keys are valid (e.g., "6h").
	SigningKeys string `json:"signingKeys,omitempty"`
}

// Logger configures logging settings.
type Logger struct {
	// Level specifies the log level (e.g., "debug", "info", "warn", "error").
	Level string `json:"level,omitempty"`

	// Format specifies the log format (e.g., "json", "text").
	Format string `json:"format,omitempty"`
}

// +kubebuilder:object:generate=true

// Connector represents an upstream identity provider connector.
type Connector struct {
	// Type specifies the connector type (e.g., "oidc", "ldap", "github", "openshift").
	Type string `json:"type,omitempty"`

	// ID is a unique identifier for this connector.
	ID string `json:"id,omitempty"`

	// Name is a human-readable name for this connector.
	Name string `json:"name,omitempty"`

	// Config contains connector-specific configuration.
	Config *ConnectorConfig `json:"config,omitempty"`
}

// +kubebuilder:object:generate=true

// ConnectorConfig contains connector-specific configuration.
// Different connector types use different fields.
type ConnectorConfig struct {
	// Common OIDC/OAuth fields
	ClientID     string   `json:"clientID,omitempty"`
	ClientSecret string   `json:"clientSecret,omitempty"`
	RedirectURI  string   `json:"redirectURI,omitempty"`
	Issuer       string   `json:"issuer,omitempty"`
	InsecureCA   bool     `json:"insecureCA,omitempty"`
	Groups       []string `json:"groups,omitempty"`

	// LDAP-specific fields
	Host               string           `json:"host,omitempty"`
	InsecureNoSSL      bool             `json:"insecureNoSSL,omitempty"`
	InsecureSkipVerify bool             `json:"insecureSkipVerify,omitempty"`
	BindDN             string           `json:"bindDN,omitempty"`
	BindPW             string           `json:"bindPW,omitempty"`
	UserSearch         *LDAPUserSearch  `json:"userSearch,omitempty"`
	GroupSearch        *LDAPGroupSearch `json:"groupSearch,omitempty"`

	// GitHub-specific fields
	Orgs []GitHubOrg `json:"orgs,omitempty"`

	// Additional fields can be added as needed
}

// +kubebuilder:object:generate=true

// LDAPUserSearch configures LDAP user search settings.
type LDAPUserSearch struct {
	BaseDN    string `json:"baseDN,omitempty"`
	Filter    string `json:"filter,omitempty"`
	Username  string `json:"username,omitempty"`
	IDAttr    string `json:"idAttr,omitempty"`
	EmailAttr string `json:"emailAttr,omitempty"`
	NameAttr  string `json:"nameAttr,omitempty"`
}

// +kubebuilder:object:generate=true

// LDAPGroupSearch configures LDAP group search settings.
type LDAPGroupSearch struct {
	BaseDN       string            `json:"baseDN,omitempty"`
	Filter       string            `json:"filter,omitempty"`
	UserMatchers []LDAPUserMatcher `json:"userMatchers,omitempty"`
	NameAttr     string            `json:"nameAttr,omitempty"`
}

// LDAPUserMatcher configures how users are matched to groups.
type LDAPUserMatcher struct {
	UserAttr  string `json:"userAttr,omitempty"`
	GroupAttr string `json:"groupAttr,omitempty"`
}

// +kubebuilder:object:generate=true

// GitHubOrg represents a GitHub organization for authentication.
type GitHubOrg struct {
	Name  string   `json:"name,omitempty"`
	Teams []string `json:"teams,omitempty"`
}

// Client represents a static OAuth2 client.
type Client struct {
	// ID is the client identifier.
	ID string `json:"id,omitempty"`

	// Secret is the client secret. Either Secret or SecretEnv should be set.
	Secret string `json:"secret,omitempty"`

	// SecretEnv is the name of an environment variable containing the client secret.
	SecretEnv string `json:"secretEnv,omitempty"`

	// RedirectURIs is a list of allowed redirect URIs.
	RedirectURIs []string `json:"redirectURIs,omitempty"`

	// TrustedPeers is a list of trusted peer client IDs.
	TrustedPeers []string `json:"trustedPeers,omitempty"`

	// Public indicates if this is a public client (no secret required).
	Public bool `json:"public,omitempty"`

	// Name is a human-readable name for this client.
	Name string `json:"name,omitempty"`

	// LogoURL is the URL to the client's logo.
	LogoURL string `json:"logoURL,omitempty"`
}

// Password represents a static user password entry.
type Password struct {
	// Email is the user's email address (used as the login identifier).
	Email string `json:"email,omitempty"`

	// Hash is the bcrypt hash of the user's password.
	Hash string `json:"hash,omitempty"`

	// Username is the display name for the user.
	Username string `json:"username,omitempty"`

	// UserID is a unique identifier for the user.
	UserID string `json:"userID,omitempty"`
}

// ToYAML serializes the Config to YAML format.
func (c *Config) ToYAML() ([]byte, error) {
	return yaml.Marshal(c)
}
