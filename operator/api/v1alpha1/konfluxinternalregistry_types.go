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

// KonfluxInternalRegistrySpec defines the desired state of KonfluxInternalRegistry.
type KonfluxInternalRegistrySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// KonfluxInternalRegistryStatus defines the observed state of KonfluxInternalRegistry.
type KonfluxInternalRegistryStatus struct {
	// Conditions represent the latest available observations of the KonfluxInternalRegistry state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-internal-registry'",message="KonfluxInternalRegistry CR must be named 'konflux-internal-registry'. Only one instance is allowed per cluster."

// KonfluxInternalRegistry is the Schema for the konfluxinternalregistries API.
// Enabling the internal registry requires trust-manager to be deployed for Certificate and Bundle resources.
type KonfluxInternalRegistry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default:={}
	Spec   KonfluxInternalRegistrySpec   `json:"spec"`
	Status KonfluxInternalRegistryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxInternalRegistryList contains a list of KonfluxInternalRegistry.
type KonfluxInternalRegistryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxInternalRegistry `json:"items"`
}

// GetConditions returns the conditions from the KonfluxInternalRegistry status.
func (k *KonfluxInternalRegistry) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxInternalRegistry status.
func (k *KonfluxInternalRegistry) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxInternalRegistry{}, &KonfluxInternalRegistryList{})
}
