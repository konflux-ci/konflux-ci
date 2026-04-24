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

// KonfluxCLISpec defines the desired state of KonfluxCLI.
type KonfluxCLISpec struct {
}

// KonfluxCLIStatus defines the observed state of KonfluxCLI.
type KonfluxCLIStatus struct {
	// Conditions represent the latest available observations of the KonfluxCLI state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-cli'",message="KonfluxCLI CR must be named 'konflux-cli'. Only one instance is allowed per cluster."

// KonfluxCLI is the Schema for the konfluxclis API.
type KonfluxCLI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default:={}
	Spec   KonfluxCLISpec   `json:"spec"`
	Status KonfluxCLIStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxCLIList contains a list of KonfluxCLI.
type KonfluxCLIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxCLI `json:"items"`
}

// GetConditions returns the conditions from the KonfluxCLI status.
func (k *KonfluxCLI) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxCLI status.
func (k *KonfluxCLI) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxCLI{}, &KonfluxCLIList{})
}
