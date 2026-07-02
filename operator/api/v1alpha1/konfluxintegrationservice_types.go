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

	// PipelineTimeout is the overall pipeline run timeout (e.g. "6h", "1h30m", "90m").
	// Maps to the PIPELINE_TIMEOUT env var on the controller-manager container.
	// Takes precedence over any PIPELINE_TIMEOUT entry in integrationControllerManager.manager.env.
	// When omitted, the upstream integration-service default applies.
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]+h)?([0-9]+m)?([0-9]+s)?$`
	// +kubebuilder:validation:MinLength=2
	PipelineTimeout string `json:"pipelineTimeout,omitempty"`

	// TasksTimeout is the timeout for tasks within a pipeline run (e.g. "4h", "90m").
	// Maps to the TASKS_TIMEOUT env var on the controller-manager container.
	// Takes precedence over any TASKS_TIMEOUT entry in integrationControllerManager.manager.env.
	// When omitted, the upstream integration-service default applies.
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]+h)?([0-9]+m)?([0-9]+s)?$`
	// +kubebuilder:validation:MinLength=2
	TasksTimeout string `json:"tasksTimeout,omitempty"`

	// FinallyTimeout is the timeout for finally tasks (e.g. "2h", "30m").
	// Maps to the FINALLY_TIMEOUT env var on the controller-manager container.
	// Takes precedence over any FINALLY_TIMEOUT entry in integrationControllerManager.manager.env.
	// When omitted, the upstream integration-service default applies.
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]+h)?([0-9]+m)?([0-9]+s)?$`
	// +kubebuilder:validation:MinLength=2
	FinallyTimeout string `json:"finallyTimeout,omitempty"`

	// SnapshotGarbageCollector defines customizations for the snapshot GC CronJob container
	// (resources, env vars).
	// +optional
	SnapshotGarbageCollector *ContainerSpec `json:"snapshotGarbageCollector,omitempty"`

	// PRSnapshotsToKeep is the number of snapshots to retain per component for PR-triggered
	// pipeline runs. Maps to the PR_SNAPSHOTS_TO_KEEP env var on the GC container.
	// Takes precedence over any PR_SNAPSHOTS_TO_KEEP entry in snapshotGarbageCollector.env.
	// When omitted, the upstream integration-service default applies.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-9]+$`
	PRSnapshotsToKeep string `json:"prSnapshotsToKeep,omitempty"`

	// NonPRSnapshotsToKeep is the number of snapshots to retain per component for non-PR
	// pipeline runs. Maps to the NON_PR_SNAPSHOTS_TO_KEEP env var on the GC container.
	// Takes precedence over any NON_PR_SNAPSHOTS_TO_KEEP entry in snapshotGarbageCollector.env.
	// When omitted, the upstream integration-service default applies.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-9]+$`
	NonPRSnapshotsToKeep string `json:"nonPRSnapshotsToKeep,omitempty"`

	// MinSnapshotsToKeepPerComponent is the minimum number of snapshots to retain per component,
	// regardless of PR/non-PR classification. Injected as the MIN_SNAPSHOTS_TO_KEEP_PER_COMPONENT
	// env var on the GC container.
	// Takes precedence over any MIN_SNAPSHOTS_TO_KEEP_PER_COMPONENT entry in snapshotGarbageCollector.env.
	// When omitted, the upstream integration-service default applies.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-9]+$`
	MinSnapshotsToKeepPerComponent string `json:"minSnapshotsToKeepPerComponent,omitempty"`
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
