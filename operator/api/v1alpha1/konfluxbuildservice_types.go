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

	// PACWebhookInsecureSSL controls TLS certificate verification when the build-service
	// configures webhooks on git providers.
	// When set to true, sets PAC_WEBHOOK_INSECURE_SSL=1 on the controller
	// (use only in development or test environments with self-signed certificates).
	// When set to false, forces PAC_WEBHOOK_INSECURE_SSL off, overriding any
	// value in buildControllerManager.manager.env.
	// When omitted, the upstream default applies and PAC_WEBHOOK_INSECURE_SSL
	// in buildControllerManager.manager.env (if set) takes effect.
	// +optional
	PACWebhookInsecureSSL *bool `json:"pacWebhookInsecureSSL,omitempty"`

	// LogEncoder sets the log encoding format for the build-service controller.
	// "json" produces structured JSON logs (upstream default), "console" produces
	// human-readable output useful for development and debugging.
	// When not set, the upstream default (json) is used.
	// +optional
	// +kubebuilder:validation:Enum=json;console
	LogEncoder string `json:"logEncoder,omitempty"`

	// WebhookURLs maps repository URL prefixes to externally-reachable webhook URLs.
	// When configured, the build-service uses these URLs instead of the default PaC webhook
	// URL when setting up webhooks for git repositories.
	//
	// Keys are matched against the full repository URL using longest-prefix matching.
	// An empty string key ("") serves as a catch-all default for any unmatched repository.
	//
	// Example:
	//   webhookURLs:
	//     "https://github.com": "https://smee.example.com/github-hook"
	//     "https://gitlab.com": "https://smee.example.com/gitlab-hook"
	// +optional
	WebhookURLs map[string]string `json:"webhookURLs,omitempty"`

	// PipelineConfig controls the contents of the build-pipeline-config ConfigMap.
	// The operator always manages this ConfigMap; use this field to customize which
	// pipelines are included.
	//
	// Default behavior (nil or empty):
	//   The operator applies the full set of default pipeline bundle references.
	//
	// Merge behavior:
	//   When set, the operator merges user-specified pipelines with the defaults.
	//   Pipelines with matching names override the defaults.
	//   Pipelines with removed: true exclude the matching default.
	//   Set removeDefaults: true to discard all defaults and use only user-specified pipelines.
	//
	// +optional
	PipelineConfig *PipelineConfigSpec `json:"pipelineConfig,omitempty"`
}

// PipelineConfigSpec defines how the operator should build the build-pipeline-config ConfigMap.
// +mapType=atomic
// +kubebuilder:validation:XValidation:rule="!has(self.removeDefaults) || !self.removeDefaults || has(self.defaultPipelineName)",message="defaultPipelineName is required when removeDefaults is true"
// +kubebuilder:validation:XValidation:rule="!has(self.removeDefaults) || !self.removeDefaults || self.pipelines.exists(p, !has(p.removed) || !p.removed)",message="at least one pipeline with a bundle must be provided when removeDefaults is true"
// +kubebuilder:validation:XValidation:rule="!has(self.removeDefaults) || !self.removeDefaults || !has(self.defaultPipelineName) || self.pipelines.exists(p, p.name == self.defaultPipelineName && (!has(p.removed) || !p.removed))",message="defaultPipelineName must reference a pipeline in the pipelines list when removeDefaults is true"
// +kubebuilder:validation:XValidation:rule="!has(self.defaultPipelineName) || !self.pipelines.exists(p, p.name == self.defaultPipelineName && has(p.removed) && p.removed)",message="defaultPipelineName must not reference a pipeline that is being removed"
type PipelineConfigSpec struct {
	// RemoveDefaults disables all operator-provided default pipelines.
	// When true, only user-specified pipelines in the Pipelines list are included.
	// +optional
	RemoveDefaults bool `json:"removeDefaults,omitempty"`

	// DefaultPipelineName specifies which pipeline to use as the default.
	// The referenced pipeline must exist in the final merged pipeline list.
	// When not set, the operator-provided default is preserved.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	DefaultPipelineName string `json:"defaultPipelineName,omitempty"`

	// Pipelines specifies user-provided pipeline overrides or additions.
	// Entries with matching names override operator defaults.
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +listType=atomic
	Pipelines []PipelineSpec `json:"pipelines,omitempty"`
}

// PipelineSpec defines a single pipeline entry in the build-pipeline-config ConfigMap.
// +kubebuilder:validation:XValidation:rule="!has(self.bundle) || !has(self.removed) || !self.removed",message="bundle must not be set when removed is true"
// +kubebuilder:validation:XValidation:rule="(has(self.removed) && self.removed) || (has(self.bundle) && size(self.bundle) > 0)",message="bundle is required when removed is not true"
type PipelineSpec struct {
	// Name is the pipeline identifier. Must match a default pipeline name to override it.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// Bundle is the Tekton bundle reference for this pipeline.
	// +optional
	Bundle string `json:"bundle,omitempty"`

	// Description is a human-readable description of this pipeline.
	// When overriding a default pipeline, this replaces the default description.
	// +optional
	Description string `json:"description,omitempty"`

	// Removed excludes this pipeline from the final configuration.
	// Use to remove a specific operator-provided default pipeline.
	// When true, bundle must not be set.
	// +optional
	Removed bool `json:"removed,omitempty"`
}

// PipelineConfigData represents the structure of the config.yaml data in the
// build-pipeline-config ConfigMap. This is the on-disk format consumed by the
// build-service, not a CRD type.
type PipelineConfigData struct {
	DefaultPipelineName string              `json:"default-pipeline-name,omitempty"`
	Pipelines           []PipelineEntryData `json:"pipelines"`
}

// PipelineEntryData represents a single pipeline entry in the config.yaml.
type PipelineEntryData struct {
	Name        string `json:"name"`
	Bundle      string `json:"bundle"`
	Description string `json:"description,omitempty"`
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

	// +kubebuilder:default:={}
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
