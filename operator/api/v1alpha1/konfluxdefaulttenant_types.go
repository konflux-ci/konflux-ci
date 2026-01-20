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

// KonfluxDefaultTenantSpec defines the desired state of KonfluxDefaultTenant.
type KonfluxDefaultTenantSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// KonfluxDefaultTenantStatus defines the observed state of KonfluxDefaultTenant.
type KonfluxDefaultTenantStatus struct {
	// Conditions represent the latest available observations of the KonfluxDefaultTenant state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-default-tenant'",message="KonfluxDefaultTenant CR must be named 'konflux-default-tenant'. Only one instance is allowed per cluster."

// KonfluxDefaultTenant is the Schema for the konfluxdefaulttenants API.
type KonfluxDefaultTenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default:={}
	Spec   KonfluxDefaultTenantSpec   `json:"spec"`
	Status KonfluxDefaultTenantStatus `json:"status,omitempty"`
}

// GetConditions returns the conditions from the KonfluxDefaultTenant status.
func (k *KonfluxDefaultTenant) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxDefaultTenant status.
func (k *KonfluxDefaultTenant) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// KonfluxDefaultTenantList contains a list of KonfluxDefaultTenant
type KonfluxDefaultTenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxDefaultTenant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KonfluxDefaultTenant{}, &KonfluxDefaultTenantList{})
}
