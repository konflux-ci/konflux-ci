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

// KonfluxBuildServiceSpec defines the desired state of KonfluxBuildService
type KonfluxBuildServiceSpec struct {
	// BuildControllerManager defines customizations for the controller-manager deployment.
	// +optional
	BuildControllerManager *ControllerManagerDeploymentSpec `json:"buildControllerManager,omitempty"`
}

// KonfluxBuildServiceStatus defines the observed state of KonfluxBuildService
type KonfluxBuildServiceStatus struct {
	// Conditions represent the latest available observations of the KonfluxBuildService state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-build-service'",message="KonfluxBuildService CR must be named 'konflux-build-service'. Only one instance is allowed per cluster."

// KonfluxBuildService is the Schema for the konfluxbuildservices API
type KonfluxBuildService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KonfluxBuildServiceSpec   `json:"spec"`
	Status KonfluxBuildServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxBuildServiceList contains a list of KonfluxBuildService
type KonfluxBuildServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxBuildService `json:"items"`
}

// GetConditions returns the conditions from the KonfluxBuildService status.
func (k *KonfluxBuildService) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxBuildService status.
func (k *KonfluxBuildService) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxBuildService{}, &KonfluxBuildServiceList{})
}
