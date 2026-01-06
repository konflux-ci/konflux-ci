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

// KonfluxReleaseServiceSpec defines the desired state of KonfluxReleaseService
type KonfluxReleaseServiceSpec struct {
	// ReleaseControllerManager defines customizations for the controller-manager deployment.
	// +optional
	ReleaseControllerManager *ControllerManagerDeploymentSpec `json:"releaseControllerManager,omitempty"`
}

// KonfluxReleaseServiceStatus defines the observed state of KonfluxReleaseService
type KonfluxReleaseServiceStatus struct {
	// Conditions represent the latest available observations of the KonfluxReleaseService state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-release-service'",message="KonfluxReleaseService CR must be named 'konflux-release-service'. Only one instance is allowed per cluster."

// KonfluxReleaseService is the Schema for the konfluxreleaseservices API
type KonfluxReleaseService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default:={}
	Spec   KonfluxReleaseServiceSpec   `json:"spec"`
	Status KonfluxReleaseServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxReleaseServiceList contains a list of KonfluxReleaseService
type KonfluxReleaseServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxReleaseService `json:"items"`
}

// GetConditions returns the conditions from the KonfluxReleaseService status.
func (k *KonfluxReleaseService) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxReleaseService status.
func (k *KonfluxReleaseService) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxReleaseService{}, &KonfluxReleaseServiceList{})
}
