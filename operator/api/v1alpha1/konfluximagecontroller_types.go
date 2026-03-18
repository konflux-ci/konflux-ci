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

// QuayCABundleSpec configures a custom CA bundle for Quay registry communication.
// The referenced ConfigMap must exist in the image-controller namespace.
type QuayCABundleSpec struct {
	// ConfigMapName is the name of the ConfigMap containing the CA certificate.
	// +kubebuilder:validation:MinLength=1
	ConfigMapName string `json:"configMapName"`
	// Key is the key within the ConfigMap that contains the CA certificate in PEM format.
	// Must be a plain filename without path separators or directory traversal sequences.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`
	Key string `json:"key"`
}

// KonfluxImageControllerSpec defines the desired state of KonfluxImageController.
type KonfluxImageControllerSpec struct {
	// QuayCABundle configures a custom CA bundle for Quay registry communication.
	// When set, the CA certificate from the referenced ConfigMap is mounted into the
	// image-controller pod and used for TLS verification when connecting to Quay.
	// This is required when using a self-hosted Quay registry with a custom CA.
	// +optional
	QuayCABundle *QuayCABundleSpec `json:"quayCABundle,omitempty"`
}

// KonfluxImageControllerStatus defines the observed state of KonfluxImageController.
type KonfluxImageControllerStatus struct {
	// Conditions represent the latest available observations of the KonfluxImageController state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-image-controller'",message="KonfluxImageController CR must be named 'konflux-image-controller'. Only one instance is allowed per cluster."

// KonfluxImageController is the Schema for the konfluximagecontrollers API.
type KonfluxImageController struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default:={}
	Spec   KonfluxImageControllerSpec   `json:"spec"`
	Status KonfluxImageControllerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxImageControllerList contains a list of KonfluxImageController.
type KonfluxImageControllerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxImageController `json:"items"`
}

// GetConditions returns the conditions from the KonfluxImageController status.
func (k *KonfluxImageController) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxImageController status.
func (k *KonfluxImageController) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxImageController{}, &KonfluxImageControllerList{})
}
