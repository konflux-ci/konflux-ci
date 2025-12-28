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

package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// KonfluxInfoSpec defines the desired state of KonfluxInfo.
type KonfluxInfoSpec struct {
	// PublicInfo defines the configuration for the info.json ConfigMap.
	// If not specified, default development values will be used.
	// +optional
	PublicInfo *PublicInfo `json:"publicInfo,omitempty"`

	// Banner defines the configuration for the banner-content.yaml ConfigMap.
	// If not specified, an empty banner array will be used.
	// +optional
	Banner *Banner `json:"banner,omitempty"`
}

// Banner contains banner configuration
type Banner struct {
	// Items is the list of banners to display
	// +optional
	Items *[]BannerItem `json:"items,omitempty"`
}

// PublicInfo contains configurable parameters for info.json
type PublicInfo struct {
	// Environment is the environment type (development, production, staging)
	// +kubebuilder:validation:Enum=development;production;staging
	// +kubebuilder:default=development
	Environment string `json:"environment"`

	// Visibility is the visibility level (public, private)
	// +kubebuilder:validation:Enum=public;private
	// +kubebuilder:default=public
	Visibility string `json:"visibility"`

	// Integrations contains integration configuration
	// +optional
	Integrations *IntegrationsConfig `json:"integrations,omitempty"`

	// StatusPageUrl is the URL to the status page
	// +optional
	StatusPageUrl string `json:"statusPageUrl,omitempty"`

	// RBAC contains RBAC role definitions
	// +optional
	RBAC []RBACRole `json:"rbac,omitempty"`
}

// IntegrationsConfig contains integration configuration
type IntegrationsConfig struct {
	// GitHub contains GitHub integration configuration
	// +optional
	GitHub *GitHubIntegration `json:"github,omitempty"`

	// SBOMServer contains SBOM server configuration
	// +optional
	SBOMServer *SBOMServerConfig `json:"sbom_server,omitempty"`

	// ImageController contains image controller configuration
	// +optional
	ImageController *InfoImageControllerConfig `json:"image_controller,omitempty"`
}

// GitHubIntegration contains GitHub integration configuration
type GitHubIntegration struct {
	// ApplicationURL is the GitHub App installation URL
	ApplicationURL string `json:"application_url"`
}

// SBOMServerConfig contains SBOM server configuration
type SBOMServerConfig struct {
	// URL is the SBOM content URL
	URL string `json:"url"`

	// SBOMSha is the SBOM SHA URL
	SBOMSha string `json:"sbom_sha"`
}

// InfoImageControllerConfig contains image controller configuration for info.json
type InfoImageControllerConfig struct {
	// Enabled indicates if image controller is enabled
	Enabled bool `json:"enabled"`

	// Notifications contains notification configurations
	// +optional
	Notifications []InfoNotificationConfig `json:"notifications,omitempty"`
}

// InfoNotificationConfig contains notification configuration for info.json
type InfoNotificationConfig struct {
	// Title is the notification title
	Title string `json:"title"`

	// Event is the event type (e.g., "repo_push", "build_complete")
	Event string `json:"event"`

	// Method is the notification method (e.g., "webhook", "email")
	Method string `json:"method"`

	// Config contains method-specific configuration (as JSON).
	// For webhook method, use: {"url": "https://webhook.example.com/endpoint"}
	// For email method, use: {"email": "notifications@example.com"}
	// Example webhook config:
	//   config:
	//     url: "https://webhook.example.com/build"
	// Example email config:
	//   config:
	//     email: "team@example.com"
	// +kubebuilder:pruning:PreserveUnknownFields
	Config apiextensionsv1.JSON `json:"config"`
}

// RBACRole contains RBAC role definition
type RBACRole struct {
	// Name is the ClusterRole name (e.g., "konflux-admin-user-actions")
	Name string `json:"name"`

	// Description is the role description
	Description string `json:"description"`

	// DisplayName is the human-readable name displayed in the UI.
	// If not specified, defaults to the Name field.
	// +optional
	DisplayName string `json:"displayName,omitempty"`
}

// BannerItem contains individual banner configuration
type BannerItem struct {
	// Summary is the banner text (5-500 chars, supports Markdown)
	// +kubebuilder:validation:MinLength=5
	// +kubebuilder:validation:MaxLength=500
	Summary string `json:"summary"`

	// Type is the banner type (info, warning, danger)
	// +kubebuilder:validation:Enum=info;warning;danger
	Type string `json:"type"`

	// StartTime is the start time in HH:mm format (required if date fields are set)
	// +optional
	StartTime string `json:"startTime,omitempty"`

	// EndTime is the end time in HH:mm format (required if date fields are set)
	// +optional
	EndTime string `json:"endTime,omitempty"`

	// TimeZone is the IANA timezone (optional, defaults to UTC)
	// +optional
	TimeZone string `json:"timeZone,omitempty"`

	// Year is the year for one-time banners
	// +optional
	Year *int `json:"year,omitempty"`

	// Month is the month (1-12)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=12
	// +optional
	Month *int `json:"month,omitempty"`

	// DayOfWeek is the day of week (0-6, 0=Sunday)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=6
	// +optional
	DayOfWeek *int `json:"dayOfWeek,omitempty"`

	// DayOfMonth is the day of month (1-31)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=31
	// +optional
	DayOfMonth *int `json:"dayOfMonth,omitempty"`
}

// KonfluxInfoStatus defines the observed state of KonfluxInfo.
type KonfluxInfoStatus struct {
	// Conditions represent the latest available observations of the KonfluxInfo state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-info'",message="KonfluxInfo CR must be named 'konflux-info'. Only one instance is allowed per cluster."

// KonfluxInfo is the Schema for the konfluxinfoes API.
type KonfluxInfo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KonfluxInfoSpec   `json:"spec,omitempty"`
	Status KonfluxInfoStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxInfoList contains a list of KonfluxInfo.
type KonfluxInfoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxInfo `json:"items"`
}

// GetConditions returns the conditions from the KonfluxInfo status.
func (k *KonfluxInfo) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxInfo status.
func (k *KonfluxInfo) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxInfo{}, &KonfluxInfoList{})
}
