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
	"github.com/konflux-ci/konflux-ci/operator/pkg/dex"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProxyDeploymentSpec defines customizations for the proxy deployment.
type ProxyDeploymentSpec struct {
	// Replicas is the number of replicas for the proxy deployment.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas int32 `json:"replicas,omitempty"`
	// Nginx defines customizations for the nginx container.
	// +optional
	Nginx *ContainerSpec `json:"nginx,omitempty"`
	// OAuth2Proxy defines customizations for the oauth2-proxy container.
	// +optional
	OAuth2Proxy *ContainerSpec `json:"oauth2Proxy,omitempty"`
}

// DexDeploymentSpec defines customizations for the dex deployment.
type DexDeploymentSpec struct {
	// Replicas is the number of replicas for the dex deployment.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas int32 `json:"replicas,omitempty"`
	// Dex defines customizations for the dex container.
	// +optional
	Dex *ContainerSpec `json:"dex,omitempty"`
	// Config defines the Dex IdP configuration parameters.
	// +optional
	Config *dex.DexParams `json:"config,omitempty"`
}

// KonfluxUISpec defines the desired state of KonfluxUI
type KonfluxUISpec struct {
	// Proxy defines customizations for the proxy deployment.
	// +optional
	Proxy *ProxyDeploymentSpec `json:"proxy,omitempty"`
	// Dex defines customizations for the dex deployment.
	// +optional
	Dex *DexDeploymentSpec `json:"dex,omitempty"`
}

// KonfluxUIStatus defines the observed state of KonfluxUI
type KonfluxUIStatus struct {
	// Conditions represent the latest available observations of the KonfluxUI state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-ui'",message="KonfluxUI CR must be named 'konflux-ui'. Only one instance is allowed per cluster."

// KonfluxUI is the Schema for the konfluxuis API
type KonfluxUI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KonfluxUISpec   `json:"spec,omitempty"`
	Status KonfluxUIStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxUIList contains a list of KonfluxUI
type KonfluxUIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxUI `json:"items"`
}

// GetConditions returns the conditions from the KonfluxUI status.
func (k *KonfluxUI) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxUI status.
func (k *KonfluxUI) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxUI{}, &KonfluxUIList{})
}
