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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// KonfluxReleaseServiceConfigSpec defines user-configurable release-service settings on the Konflux CR.
type KonfluxReleaseServiceConfigSpec struct {
	// Debug enables debug mode in the ReleaseServiceConfig.
	// When true, the release-service operates in debug mode.
	// +optional
	Debug bool `json:"debug,omitempty"`

	// EmptyDirOverrides defines pipeline patterns that should use emptyDir volumes
	// instead of PVCs. Applied to the ReleaseServiceConfig CR managed by the operator.
	// +optional
	EmptyDirOverrides []EmptyDirOverride `json:"emptyDirOverrides,omitempty"`

	// RetryablePipelines is a list of pipelines that are safe to automatically retry on failure.
	// Applied to the ReleaseServiceConfig CR managed by the operator.
	// +optional
	RetryablePipelines []RetryablePipeline `json:"retryablePipelines,omitempty"`

	// ReleaseControllerManager defines customizations for the controller-manager deployment.
	// +optional
	ReleaseControllerManager *ControllerManagerDeploymentSpec `json:"releaseControllerManager,omitempty"`
}

// RetryablePipeline is a pipeline that is safe to automatically retry on failure.
type RetryablePipeline struct {
	// URL is the URL of the git repository.
	URL string `json:"url"`
	// Revision is the git revision where the Pipeline definition can be found.
	Revision string `json:"revision"`
	// PathInRepo is the path within the git repository where the Pipeline definition can be found.
	PathInRepo string `json:"pathInRepo"`
	// RetryPolicy defines how and when the pipeline is retried after a failure.
	RetryPolicy RetryPolicy `json:"retryPolicy"`
}

// RetryPolicy defines how and when the pipeline is retried after a failure.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retries allowed for a pipeline.
	// +kubebuilder:validation:Minimum=0
	MaxRetries int `json:"maxRetries"`
	// DisableOn defines conditions that disable automatic retries.
	// +optional
	DisableOn *RetryDisableOn `json:"disableOn,omitempty"`
	// Mitigations defines adjustment strategies to apply on retry based on failure type.
	// +optional
	Mitigations *RetryMitigations `json:"mitigations,omitempty"`
}

// RetryDisableOn defines conditions that disable automatic retries.
type RetryDisableOn struct {
	// Tags is a list of values which disable retries when present in the ReleasePlan or
	// ReleasePlanAdmission mapping data.
	// +optional
	Tags []string `json:"tags,omitempty"`
}

// RetryMitigations defines adjustment strategies to apply on retry based on failure type.
type RetryMitigations struct {
	// OOMKill defines adjustment for out-of-memory failures.
	// +optional
	OOMKill *OOMKillMitigation `json:"oomKill,omitempty"`
	// Timeout defines adjustment for timeout failures.
	// +optional
	Timeout *TimeoutMitigation `json:"timeout,omitempty"`
}

// OOMKillMitigation defines adjustment for out-of-memory failures.
type OOMKillMitigation struct {
	// Multiplier is a factor used to multiply memory limits on each retry.
	// +kubebuilder:validation:Pattern=`^[0-9]+(\.[0-9]+)?$`
	Multiplier string `json:"multiplier"`
	// MaxComputeResources sets an upper limit on resources across retries.
	// +optional
	MaxComputeResources *ComputeResources `json:"maxComputeResources,omitempty"`
}

// ComputeResources defines limits and requests for compute resources.
type ComputeResources struct {
	// Limits describes the maximum amount of compute resources allowed.
	// +optional
	Limits corev1.ResourceList `json:"limits,omitempty"`
	// Requests describes the minimum amount of compute resources required.
	// +optional
	Requests corev1.ResourceList `json:"requests,omitempty"`
}

// TimeoutMitigation defines adjustment for timeout failures.
type TimeoutMitigation struct {
	// Pipeline defines the timeout adjustment for PipelineRun timeouts.
	// +optional
	Pipeline *TimeoutAdjustment `json:"pipeline,omitempty"`
	// Task defines the timeout adjustment for TaskRun timeouts.
	// +optional
	Task *TimeoutAdjustment `json:"task,omitempty"`
}

// TimeoutAdjustment defines how to adjust a timeout on each retry.
type TimeoutAdjustment struct {
	// Increment is the duration to add to the timeout on each retry.
	Increment string `json:"increment"`
	// MaxTimeout is the maximum time allowed across retries.
	// +optional
	MaxTimeout string `json:"maxTimeout,omitempty"`
}

// KonfluxReleaseServiceSpec defines the desired state of KonfluxReleaseService.
type KonfluxReleaseServiceSpec struct {
	KonfluxReleaseServiceConfigSpec `json:",inline"`

	// ComponentMetrics controls Prometheus scrape resources for this component.
	// Set by the Konflux reconciler from spec.componentMetrics on the Konflux CR.
	// +optional
	ComponentMetrics *ComponentMetricsConfig `json:"componentMetrics,omitempty"`
}

// EmptyDirOverride defines a pipeline pattern that should use emptyDir volumes.
type EmptyDirOverride struct {
	// URL is a regex pattern matching the pipeline repository URL.
	URL string `json:"url"`
	// Revision is a regex pattern matching the pipeline revision.
	Revision string `json:"revision"`
	// PathInRepo is the path to the pipeline file within the repository.
	PathInRepo string `json:"pathInRepo"`
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
