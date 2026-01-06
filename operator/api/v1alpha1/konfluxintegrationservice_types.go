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

// KonfluxIntegrationServiceSpec defines the desired state of KonfluxIntegrationService
type KonfluxIntegrationServiceSpec struct {
	// IntegrationControllerManager defines customizations for the controller-manager deployment.
	// +optional
	IntegrationControllerManager *ControllerManagerDeploymentSpec `json:"integrationControllerManager,omitempty"`
}

// KonfluxIntegrationServiceStatus defines the observed state of KonfluxIntegrationService
type KonfluxIntegrationServiceStatus struct {
	// Conditions represent the latest available observations of the KonfluxIntegrationService state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-integration-service'",message="KonfluxIntegrationService CR must be named 'konflux-integration-service'. Only one instance is allowed per cluster."

// KonfluxIntegrationService is the Schema for the konfluxintegrationservices API
type KonfluxIntegrationService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default:={}
	Spec   KonfluxIntegrationServiceSpec   `json:"spec"`
	Status KonfluxIntegrationServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxIntegrationServiceList contains a list of KonfluxIntegrationService
type KonfluxIntegrationServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxIntegrationService `json:"items"`
}

// GetConditions returns the conditions from the KonfluxIntegrationService status.
func (k *KonfluxIntegrationService) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxIntegrationService status.
func (k *KonfluxIntegrationService) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxIntegrationService{}, &KonfluxIntegrationServiceList{})
}
