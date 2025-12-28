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

// KonfluxApplicationAPISpec defines the desired state of KonfluxApplicationAPI.
type KonfluxApplicationAPISpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// KonfluxApplicationAPIStatus defines the observed state of KonfluxApplicationAPI.
type KonfluxApplicationAPIStatus struct {
	// Conditions represent the latest available observations of the KonfluxApplicationAPI state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-application-api'",message="KonfluxApplicationAPI CR must be named 'konflux-application-api'. Only one instance is allowed per cluster."

// KonfluxApplicationAPI is the Schema for the konfluxapplicationapis API.
type KonfluxApplicationAPI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KonfluxApplicationAPISpec   `json:"spec,omitempty"`
	Status KonfluxApplicationAPIStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxApplicationAPIList contains a list of KonfluxApplicationAPI.
type KonfluxApplicationAPIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxApplicationAPI `json:"items"`
}

// GetConditions returns the conditions from the KonfluxApplicationAPI status.
func (k *KonfluxApplicationAPI) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxApplicationAPI status.
func (k *KonfluxApplicationAPI) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxApplicationAPI{}, &KonfluxApplicationAPIList{})
}
