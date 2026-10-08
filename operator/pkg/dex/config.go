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

	// GrantTypes specifies the allowed OAuth2 grant types.
	// When set, this replaces Dex defaults. Include PasswordGrantType only when
	// the local password DB is enabled (Kind/local Dex password-grant tests).
	GrantTypes []string `json:"grantTypes,omitempty"`

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
// OIDC fields follow https://dexidp.io/docs/connectors/oidc/ and are copied into the Dex config.
type ConnectorConfig struct {
	// Common OIDC/OAuth fields
	ClientID     string `json:"clientID,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	RedirectURI  string `json:"redirectURI,omitempty"`
	Issuer       string `json:"issuer,omitempty"`

	// IssuerAlias overrides the issuer URL from the provider discovery document.
	// Some providers, such as Azure, publish a discovery issuer that differs from Issuer.
	IssuerAlias string `json:"issuerAlias,omitempty"`

	// InsecureCA skips TLS verification for connectors that read this field, such as OpenShift.
	InsecureCA bool `json:"insecureCA,omitempty"`

	// Groups is a static group list for connectors that honor it.
	// Dex's OIDC connector ignores this field. Enable OIDC groups with InsecureEnableGroups.
	Groups []string `json:"groups,omitempty"`

	// RootCA is the path to a trusted root certificate for verifying TLS connections.
	// Used by connectors that need to verify the TLS certificate of the upstream provider.
	RootCA string `json:"rootCA,omitempty"`

	// BasicAuthUnsupported passes the client secret as POST parameters instead of HTTP basic auth.
	// Leave unset to let Dex detect known providers. Set false to force basic auth.
	// +optional
	// +nullable
	BasicAuthUnsupported *bool `json:"basicAuthUnsupported,omitempty"`

	// Scopes are the scopes requested from the provider. Dex defaults to profile and email.
	Scopes []string `json:"scopes,omitempty"`

	// InsecureSkipEmailVerified treats the email as verified when the provider omits email_verified.
	InsecureSkipEmailVerified bool `json:"insecureSkipEmailVerified,omitempty"`

	// InsecureEnableGroups forwards the provider's groups claim. Dex leaves this off by default.
	InsecureEnableGroups bool `json:"insecureEnableGroups,omitempty"`

	// AllowedGroups rejects login unless the user belongs to at least one listed group.
	AllowedGroups []string `json:"allowedGroups,omitempty"`

	// GetUserInfo loads claims from the UserInfo endpoint when the ID token is missing them.
	GetUserInfo bool `json:"getUserInfo,omitempty"`

	// UserIDKey is the claim used as the user ID. Dex defaults to "sub".
	UserIDKey string `json:"userIDKey,omitempty"`

	// UserNameKey is the claim used as the user name. Dex defaults to "name".
	UserNameKey string `json:"userNameKey,omitempty"`

	// AcrValues are Authentication Context Class Reference values sent on the authorization request.
	AcrValues []string `json:"acrValues,omitempty"`

	// PromptType sets the OIDC prompt parameter. Dex defaults to "consent" when requesting offline_access.
	PromptType string `json:"promptType,omitempty"`

	// ClaimMapping maps non-standard upstream claims onto Dex's standard claims.
	ClaimMapping *OIDCClaimMapping `json:"claimMapping,omitempty"`

	// ClaimModifications rewrites claims during login.
	ClaimModifications *OIDCClaimModifications `json:"claimModifications,omitempty"`

	// OverrideClaimMapping forces Dex to use ClaimMapping even when the standard claim is also present.
	OverrideClaimMapping bool `json:"overrideClaimMapping,omitempty"`

	// ProviderDiscoveryOverrides replaces URLs discovered from the provider's well-known configuration.
	ProviderDiscoveryOverrides *OIDCProviderDiscoveryOverrides `json:"providerDiscoveryOverrides,omitempty"`

	// LDAP-specific fields
	Host          string `json:"host,omitempty"`
	InsecureNoSSL bool   `json:"insecureNoSSL,omitempty"`
	// InsecureSkipVerify disables TLS certificate verification.
	// LDAP and the Dex OIDC connector both read this field.
	InsecureSkipVerify bool             `json:"insecureSkipVerify,omitempty"`
	BindDN             string           `json:"bindDN,omitempty"`
	BindPW             string           `json:"bindPW,omitempty"`
	UserSearch         *LDAPUserSearch  `json:"userSearch,omitempty"`
	GroupSearch        *LDAPGroupSearch `json:"groupSearch,omitempty"`

	// GitHub-specific fields
	Orgs []GitHubOrg `json:"orgs,omitempty"`
}

// +kubebuilder:object:generate=true

// OIDCClaimMapping maps upstream claim names onto Dex's standard claims.
type OIDCClaimMapping struct {
	// PreferredUsername is the claim used as preferred_username. Dex defaults to "preferred_username".
	PreferredUsername string `json:"preferred_username,omitempty"`
	// Email is the claim used as email. Dex defaults to "email".
	Email string `json:"email,omitempty"`
	// Groups is the claim used as the groups list. Dex defaults to "groups".
	Groups string `json:"groups,omitempty"`
}

// +kubebuilder:object:generate=true

// OIDCClaimModifications rewrites claims during OIDC login.
type OIDCClaimModifications struct {
	// NewGroupFromClaims builds extra group names by joining other claims.
	NewGroupFromClaims []OIDCNewGroupFromClaims `json:"newGroupFromClaims,omitempty"`
	// FilterGroupClaims keeps only groups that match a regular expression.
	FilterGroupClaims *OIDCFilterGroupClaims `json:"filterGroupClaims,omitempty"`
	// ModifyGroupNames adds a prefix and/or suffix to every group name from the provider.
	ModifyGroupNames *OIDCModifyGroupNames `json:"modifyGroupNames,omitempty"`
}

// +kubebuilder:object:generate=true

// OIDCNewGroupFromClaims builds a group name from other claims.
type OIDCNewGroupFromClaims struct {
	// Prefix is placed before the joined claims.
	Prefix string `json:"prefix,omitempty"`
	// Delimiter separates the joined claims.
	Delimiter string `json:"delimiter,omitempty"`
	// ClearDelimiter removes Delimiter from claim values before joining them.
	ClearDelimiter bool `json:"clearDelimiter,omitempty"`
	// Claims are the claim names to join. Only string claims are used.
	Claims []string `json:"claims,omitempty"`
}

// +kubebuilder:object:generate=true

// OIDCFilterGroupClaims keeps groups whose names match GroupsFilter.
// Groups created by NewGroupFromClaims are not filtered.
type OIDCFilterGroupClaims struct {
	// GroupsFilter is an RE2 regular expression. Groups that do not match are dropped.
	GroupsFilter string `json:"groupsFilter,omitempty"`
}

// +kubebuilder:object:generate=true

// OIDCModifyGroupNames adds a prefix and/or suffix to group names from the provider.
// Dex applies this before groups from NewGroupFromClaims are added.
type OIDCModifyGroupNames struct {
	// Prefix is prepended to each group name.
	Prefix string `json:"prefix,omitempty"`
	// Suffix is appended to each group name.
	Suffix string `json:"suffix,omitempty"`
}

// +kubebuilder:object:generate=true

// OIDCProviderDiscoveryOverrides replaces endpoints from the provider discovery document.
type OIDCProviderDiscoveryOverrides struct {
	// TokenURL overrides the token_endpoint.
	TokenURL string `json:"tokenURL,omitempty"`
	// AuthURL overrides the authorization_endpoint.
	AuthURL string `json:"authURL,omitempty"`
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

// +kubebuilder:object:generate=true

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

	// Groups is a list of groups the user belongs to.
	// Requires Dex v2.45.0+. Groups are included in the ID token
	// when the "groups" scope is requested.
	Groups []string `json:"groups,omitempty"`
}

// ToYAML serializes the Config to YAML format.
func (c *Config) ToYAML() ([]byte, error) {
	return yaml.Marshal(c)
}
