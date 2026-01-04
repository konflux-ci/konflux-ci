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

// KonfluxCertManagerSpec defines the desired state of KonfluxCertManager.
type KonfluxCertManagerSpec struct {
	// CreateClusterIssuer controls whether cluster issuer resources are created.
	// Defaults to true if not specified.
	// The cluster-Issuer will be used for generating certificates for the Konflux components
	// +optional
	CreateClusterIssuer *bool `json:"createClusterIssuer,omitempty"`
}

// KonfluxCertManagerStatus defines the observed state of KonfluxCertManager.
type KonfluxCertManagerStatus struct {
	// Conditions represent the latest available observations of the KonfluxCertManager state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-cert-manager'",message="KonfluxCertManager CR must be named 'konflux-cert-manager'. Only one instance is allowed per cluster."

// KonfluxCertManager is the Schema for the konfluxcertmanagers API.
type KonfluxCertManager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KonfluxCertManagerSpec   `json:"spec,omitempty"`
	Status KonfluxCertManagerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxCertManagerList contains a list of KonfluxCertManager.
type KonfluxCertManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxCertManager `json:"items"`
}

// GetConditions returns the conditions from the KonfluxCertManager status.
func (k *KonfluxCertManager) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxCertManager status.
func (k *KonfluxCertManager) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

// ShouldCreateClusterIssuer returns true if cluster issuer resources should be created.
// Defaults to true if not specified.
func (k *KonfluxCertManagerSpec) ShouldCreateClusterIssuer() bool {
	if k.CreateClusterIssuer == nil {
		return true
	}
	return *k.CreateClusterIssuer
}

func init() {
	SchemeBuilder.Register(&KonfluxCertManager{}, &KonfluxCertManagerList{})
}
