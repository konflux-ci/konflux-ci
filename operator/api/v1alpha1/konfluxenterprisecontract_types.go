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

// KonfluxEnterpriseContractSpec defines the desired state of KonfluxEnterpriseContract.
type KonfluxEnterpriseContractSpec struct {
	// ECDefaults allows overriding the ec-defaults ConfigMap values used by the release service.
	// When set, these values take precedence over the embedded defaults.
	// +optional
	ECDefaults *ECDefaultsConfig `json:"ecDefaults,omitempty"`
}

// ECDefaultsConfig holds overrides for the ec-defaults ConfigMap in the enterprise-contract-service namespace.
// The release-service reads this ConfigMap to inject verify-conforma task parameters into release PipelineRuns.
type ECDefaultsConfig struct {
	// VerifyECTaskBundle overrides the verify_ec_task_bundle value in the ec-defaults ConfigMap.
	// This is the OCI bundle reference for the Enterprise Contract verification task.
	// +optional
	VerifyECTaskBundle string `json:"verifyECTaskBundle,omitempty"`

	// VerifyECTaskGitURL overrides the verify_ec_task_git_url value in the ec-defaults ConfigMap.
	// This is the Git repository URL containing the Enterprise Contract verification task.
	// +optional
	VerifyECTaskGitURL string `json:"verifyECTaskGitURL,omitempty"`

	// VerifyECTaskGitRevision overrides the verify_ec_task_git_revision value in the ec-defaults ConfigMap.
	// This is the Git revision (commit SHA, branch, or tag) of the verification task.
	// +optional
	VerifyECTaskGitRevision string `json:"verifyECTaskGitRevision,omitempty"`

	// VerifyECTaskGitPathInRepo overrides the verify_ec_task_git_pathInRepo value in the ec-defaults ConfigMap.
	// This is the path within the Git repository to the verification task YAML.
	// +optional
	VerifyECTaskGitPathInRepo string `json:"verifyECTaskGitPathInRepo,omitempty"`
}

// KonfluxEnterpriseContractStatus defines the observed state of KonfluxEnterpriseContract.
type KonfluxEnterpriseContractStatus struct {
	// Conditions represent the latest available observations of the KonfluxEnterpriseContract state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-enterprise-contract'",message="KonfluxEnterpriseContract CR must be named 'konflux-enterprise-contract'. Only one instance is allowed per cluster."

// KonfluxEnterpriseContract is the Schema for the konfluxenterprisecontracts API.
type KonfluxEnterpriseContract struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default:={}
	Spec   KonfluxEnterpriseContractSpec   `json:"spec"`
	Status KonfluxEnterpriseContractStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxEnterpriseContractList contains a list of KonfluxEnterpriseContract.
type KonfluxEnterpriseContractList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxEnterpriseContract `json:"items"`
}

// GetConditions returns the conditions from the KonfluxEnterpriseContract status.
func (k *KonfluxEnterpriseContract) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxEnterpriseContract status.
func (k *KonfluxEnterpriseContract) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KonfluxEnterpriseContract{}, &KonfluxEnterpriseContractList{})
}
