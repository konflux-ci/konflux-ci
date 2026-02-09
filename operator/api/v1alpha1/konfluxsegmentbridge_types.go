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

// KonfluxSegmentBridgeSpec defines the desired state of KonfluxSegmentBridge.
type KonfluxSegmentBridgeSpec struct {
	// SegmentKey is the write key used to authenticate with the Segment API.
	// When not specified, a default key baked into the operator build is used,
	// routing telemetry data to the Konflux dev team's Segment project.
	// +optional
	SegmentKey string `json:"segmentKey,omitempty"`

	// SegmentAPIURL is the URL of the Segment API endpoint.
	// When not specified, the default Segment API URL is used.
	// Users may override this to route telemetry to their own Segment project
	// or to another system that implements the Segment API.
	// +optional
	SegmentAPIURL string `json:"segmentAPIURL,omitempty"`
}

// KonfluxSegmentBridgeStatus defines the observed state of KonfluxSegmentBridge.
type KonfluxSegmentBridgeStatus struct {
	// Conditions represent the latest available observations of the KonfluxSegmentBridge state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-segment-bridge'",message="KonfluxSegmentBridge CR must be named 'konflux-segment-bridge'. Only one instance is allowed per cluster."

// KonfluxSegmentBridge is the Schema for the konfluxsegmentbridges API.
type KonfluxSegmentBridge struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default:={}
	Spec   KonfluxSegmentBridgeSpec   `json:"spec"`
	Status KonfluxSegmentBridgeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxSegmentBridgeList contains a list of KonfluxSegmentBridge.
type KonfluxSegmentBridgeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxSegmentBridge `json:"items"`
}

// GetConditions returns the conditions from the KonfluxSegmentBridge status.
func (k *KonfluxSegmentBridge) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxSegmentBridge status.
func (k *KonfluxSegmentBridge) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxSegmentBridge{}, &KonfluxSegmentBridgeList{})
}
