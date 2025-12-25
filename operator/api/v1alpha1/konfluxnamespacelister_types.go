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

// KonfluxNamespaceListerSpec defines the desired state of KonfluxNamespaceLister.
type KonfluxNamespaceListerSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of KonfluxNamespaceLister. Edit konfluxnamespacelister_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

// KonfluxNamespaceListerStatus defines the observed state of KonfluxNamespaceLister.
type KonfluxNamespaceListerStatus struct {
	// Conditions represent the latest available observations of the KonfluxNamespaceLister state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-namespace-lister'",message="KonfluxNamespaceLister CR must be named 'konflux-namespace-lister'. Only one instance is allowed per cluster."

// KonfluxNamespaceLister is the Schema for the konfluxnamespacelisters API.
type KonfluxNamespaceLister struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KonfluxNamespaceListerSpec   `json:"spec,omitempty"`
	Status KonfluxNamespaceListerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxNamespaceListerList contains a list of KonfluxNamespaceLister.
type KonfluxNamespaceListerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxNamespaceLister `json:"items"`
}

// GetConditions returns the conditions from the KonfluxNamespaceLister status.
func (k *KonfluxNamespaceLister) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxNamespaceLister status.
func (k *KonfluxNamespaceLister) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxNamespaceLister{}, &KonfluxNamespaceListerList{})
}
