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
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

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
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
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

func init() {
	SchemeBuilder.Register(&Konflux{}, &KonfluxList{})
}
