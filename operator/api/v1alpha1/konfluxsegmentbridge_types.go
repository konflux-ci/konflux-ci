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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DefaultSegmentAPIURL is the default Segment HTTP API base URL (without /batch).
	// The operator appends "/batch" to produce the full SEGMENT_BATCH_API value.
	DefaultSegmentAPIURL = "https://api.segment.io/v1"
)

// KonfluxSegmentBridgeSpec defines the desired state of KonfluxSegmentBridge.
type KonfluxSegmentBridgeSpec struct {
	// SegmentKey is the write key used to authenticate with the Segment API.
	// When not specified, a default key baked into the operator build is used,
	// routing telemetry data to the Konflux dev team's Segment project.
	// +optional
	SegmentKey string `json:"segmentKey,omitempty"`

	// SegmentAPIURL is the base URL of the Segment API endpoint, without "/batch".
	// The operator appends "/batch" to produce the SEGMENT_BATCH_API env var.
	// Example: "https://console.redhat.com/connections/api/v1"
	// When not specified, defaults to "https://api.segment.io/v1".
	// Only plain HTTPS base URLs are supported (no query strings or fragments).
	// +optional
	// +kubebuilder:validation:Pattern=`^https://[^?#]+$`
	SegmentAPIURL string `json:"segmentAPIURL,omitempty"`
}

// GetSegmentKey returns the configured Segment write key, or empty string if unset.
func (s *KonfluxSegmentBridgeSpec) GetSegmentKey() string {
	if s == nil {
		return ""
	}
	return s.SegmentKey
}

// GetSegmentAPIURL returns the configured Segment API base URL (without "/batch"),
// falling back to DefaultSegmentAPIURL when unset. Trailing slashes and an
// accidental "/batch" suffix are stripped so callers can safely append "/batch".
func (s *KonfluxSegmentBridgeSpec) GetSegmentAPIURL() string {
	url := DefaultSegmentAPIURL
	if s != nil && s.SegmentAPIURL != "" {
		url = s.SegmentAPIURL
	}
	return sanitizeSegmentHost(url)
}

// sanitizeSegmentHost strips trailing slashes and an accidental "/batch" suffix
// so that callers can safely append "/batch" to the result.
func sanitizeSegmentHost(url string) string {
	url = strings.TrimRight(url, "/")
	url = strings.TrimSuffix(url, "/batch")
	return strings.TrimRight(url, "/")
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
