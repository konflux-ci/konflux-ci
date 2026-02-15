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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// KonfluxSpec defines the desired state of Konflux.
type KonfluxSpec struct {
	// ImageController configures the image-controller component.
	// The runtime configuration is copied to the KonfluxImageController CR by the operator.
	// +optional
	ImageController *ImageControllerConfig `json:"imageController,omitempty"`

	// KonfluxUI configures the UI component.
	// The runtime configuration is copied to the KonfluxUI CR by the operator.
	// +optional
	KonfluxUI *KonfluxUIConfig `json:"ui,omitempty"`

	// KonfluxIntegrationService configures the integration-service component.
	// The runtime configuration is copied to the KonfluxIntegrationService CR by the operator.
	// +optional
	KonfluxIntegrationService *IntegrationServiceConfig `json:"integrationService,omitempty"`

	// KonfluxReleaseService configures the release-service component.
	// The runtime configuration is copied to the KonfluxReleaseService CR by the operator.
	// +optional
	KonfluxReleaseService *ReleaseServiceConfig `json:"releaseService,omitempty"`

	// KonfluxBuildService configures the build-service component.
	// The runtime configuration is copied to the KonfluxBuildService CR by the operator.
	// +optional
	KonfluxBuildService *BuildServiceConfig `json:"buildService,omitempty"`

	// NamespaceLister configures the namespace-lister component.
	// The runtime configuration is copied to the KonfluxNamespaceLister CR by the operator.
	// +optional
	NamespaceLister *NamespaceListerConfig `json:"namespaceLister,omitempty"`

	// KonfluxInfo configures the info component.
	// The runtime configuration is copied to the KonfluxInfo CR by the operator.
	// +optional
	KonfluxInfo *KonfluxInfoConfig `json:"info,omitempty"`

	// CertManager configures the cert-manager component.
	// The runtime configuration is copied to the KonfluxCertManager CR by the operator.
	// +optional
	CertManager *CertManagerConfig `json:"certManager,omitempty"`

	// InternalRegistry configures the internal registry component.
	// The runtime configuration is copied to the KonfluxInternalRegistry CR by the operator.
	// Enabling internal registry requires trust-manager to be deployed.
	// +optional
	InternalRegistry *InternalRegistryConfig `json:"internalRegistry,omitempty"`

	// DefaultTenant configures the default tenant component.
	// The default tenant provides a namespace accessible by all authenticated users.
	// The runtime configuration is copied to the KonfluxDefaultTenant CR by the operator.
	// +optional
	DefaultTenant *DefaultTenantConfig `json:"defaultTenant,omitempty"`

	// SegmentBridge configures the segment-bridge telemetry component.
	// When enabled, the operator deploys a CronJob that collects anonymized usage
	// data from the cluster and sends it to Segment for analysis.
	// The runtime configuration is copied to the KonfluxSegmentBridge CR by the operator.
	// +optional
	SegmentBridge *SegmentBridgeConfig `json:"segmentBridge,omitempty"`
}

// ImageControllerConfig defines the configuration for the image-controller component.
// The Enabled field controls whether the component is deployed (top-level concern).
// Other fields are runtime configuration passed to the component.
type ImageControllerConfig struct {
	// Enabled indicates whether image-controller should be deployed.
	// If false or unset, the component CR will not be created.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// KonfluxUIConfig defines the configuration for the UI component.
// The Spec field is the runtime configuration passed to the component.
type KonfluxUIConfig struct {
	// Spec configures the UI component.
	// +optional
	Spec *KonfluxUISpec `json:"spec,omitempty"`
}

// IntegrationServiceConfig defines the configuration for the integration-service component.
// The Spec field is the runtime configuration passed to the component.
type IntegrationServiceConfig struct {
	// Spec configures the integration-service component.
	// +optional
	Spec *KonfluxIntegrationServiceSpec `json:"spec,omitempty"`
}

// ReleaseServiceConfig defines the configuration for the release-service component.
// The Spec field is the runtime configuration passed to the component.
type ReleaseServiceConfig struct {
	// Spec configures the release-service component.
	// +optional
	Spec *KonfluxReleaseServiceSpec `json:"spec,omitempty"`
}

// BuildServiceConfig defines the configuration for the build-service component.
// The Spec field is the runtime configuration passed to the component.
type BuildServiceConfig struct {
	// Spec configures the build-service component.
	// +optional
	Spec *KonfluxBuildServiceSpec `json:"spec,omitempty"`
}

// NamespaceListerConfig defines the configuration for the namespace-lister component.
// The Spec field is the runtime configuration passed to the component.
type NamespaceListerConfig struct {
	// Spec configures the namespace-lister component.
	// +optional
	Spec *KonfluxNamespaceListerSpec `json:"spec,omitempty"`
}

// CertManagerConfig defines the configuration for the cert-manager component.
type CertManagerConfig struct {
	// CreateClusterIssuer controls whether cluster issuer resources are created.
	// Defaults to true if not specified.
	// +optional
	CreateClusterIssuer *bool `json:"createClusterIssuer,omitempty"`
}

// InternalRegistryConfig defines the configuration for the internal registry component.
// Enabling internal registry requires trust-manager to be deployed.
type InternalRegistryConfig struct {
	// Enabled controls whether internal registry resources are deployed.
	// Defaults to false if not specified.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// DefaultTenantConfig defines the configuration for the default tenant component.
// The default tenant provides a namespace accessible by all authenticated users.
type DefaultTenantConfig struct {
	// Enabled controls whether the default tenant is created.
	// Defaults to true if not specified.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// SegmentBridgeConfig defines the configuration for the segment-bridge telemetry component.
// The Enabled field controls whether the component is deployed (top-level concern).
// The Spec field is the runtime configuration passed to the component.
type SegmentBridgeConfig struct {
	// Enabled controls whether the segment-bridge telemetry CronJob is deployed.
	// Defaults to false if not specified.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Spec configures the segment-bridge component.
	// +optional
	Spec *KonfluxSegmentBridgeSpec `json:"spec,omitempty"`
}

// KonfluxInfoConfig defines the configuration for the info component.
// The Spec field is the runtime configuration passed to the component.
type KonfluxInfoConfig struct {
	// Spec configures the info component.
	// +optional
	Spec *KonfluxInfoSpec `json:"spec,omitempty"`
}

// ComponentStatus represents the status of a Konflux component.
type ComponentStatus struct {
	// Name of the component
	Name string `json:"name"`
	// Ready indicates if the component is ready
	Ready bool `json:"ready"`
	// Message provides additional information about the component status
	// +optional
	Message string `json:"message,omitempty"`
}

// KonfluxStatus defines the observed state of Konflux.
type KonfluxStatus struct {
	// Conditions represent the latest available observations of the Konflux state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Components shows the status of individual Konflux components
	// +optional
	Components []ComponentStatus `json:"components,omitempty"`

	// UIURL is the URL to access the Konflux UI.
	// This is populated from the KonfluxUI status when ingress is enabled.
	// +optional
	UIURL string `json:"uiURL,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Ready status"
// +kubebuilder:printcolumn:name="UI-URL",type="string",JSONPath=".status.uiURL",description="URL to access the Konflux UI"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux'",message="Konflux CR must be named 'konflux'. Only one instance is allowed per cluster."

// Konflux is the Schema for the konfluxes API.
type Konflux struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KonfluxSpec   `json:"spec,omitempty"`
	Status KonfluxStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxList contains a list of Konflux.
type KonfluxList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Konflux `json:"items"`
}

// GetConditions returns the conditions from the Konflux status.
func (k *Konflux) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the Konflux status.
func (k *Konflux) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

// IsImageControllerEnabled returns true if image-controller is enabled.
// Defaults to false if not specified.
func (k *KonfluxSpec) IsImageControllerEnabled() bool {
	if k.ImageController == nil || k.ImageController.Enabled == nil {
		return false
	}
	return *k.ImageController.Enabled
}

// IsInternalRegistryEnabled returns true if internal registry is enabled.
// Defaults to false if not specified.
func (k *KonfluxSpec) IsInternalRegistryEnabled() bool {
	if k.InternalRegistry == nil || k.InternalRegistry.Enabled == nil {
		return false
	}
	return *k.InternalRegistry.Enabled
}

// IsDefaultTenantEnabled returns true if the default tenant is enabled.
// Defaults to true if not specified.
func (k *KonfluxSpec) IsDefaultTenantEnabled() bool {
	if k.DefaultTenant == nil || k.DefaultTenant.Enabled == nil {
		return true
	}
	return *k.DefaultTenant.Enabled
}

// IsSegmentBridgeEnabled returns true if the segment-bridge telemetry component is enabled.
// Defaults to false if not specified.
// NOTE: OpenShift console telemetry flag detection is deferred to a future iteration.
// Once implemented, unspecified will match the OpenShift console telemetry state.
func (k *KonfluxSpec) IsSegmentBridgeEnabled() bool {
	if k.SegmentBridge == nil || k.SegmentBridge.Enabled == nil {
		return false
	}
	return *k.SegmentBridge.Enabled
}

func init() {
	SchemeBuilder.Register(&Konflux{}, &KonfluxList{})
}
