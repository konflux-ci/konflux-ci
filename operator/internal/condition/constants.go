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

package condition

// Condition type constants.
// These are the standard condition types used across all Konflux CRs.
const (
	// TypeReady indicates the overall readiness of a resource.
	TypeReady = "Ready"
)

// Condition reason constants.
// These reasons provide standardized explanations for condition states.
const (
	// ReasonAllComponentsReady indicates all components are ready.
	ReasonAllComponentsReady = "AllComponentsReady"

	// ReasonComponentsNotReady indicates one or more components are not ready.
	ReasonComponentsNotReady = "ComponentsNotReady"

	// ReasonDeploymentReady indicates a deployment is ready.
	ReasonDeploymentReady = "DeploymentReady"

	// ReasonDeploymentNotReady indicates a deployment is not ready.
	ReasonDeploymentNotReady = "DeploymentNotReady"

	// ReasonApplyFailed indicates that applying resources failed.
	ReasonApplyFailed = "ApplyFailed"

	// ReasonCleanupFailed indicates that cleanup of orphaned resources failed.
	ReasonCleanupFailed = "CleanupFailed"

	// ReasonStatusUpdateFailed indicates that fetching deployment status failed.
	ReasonStatusUpdateFailed = "StatusUpdateFailed"

	// ReasonNamespaceCreationFailed indicates that namespace creation failed.
	ReasonNamespaceCreationFailed = "NamespaceCreationFailed"

	// ReasonEndpointDeterminationFailed indicates that endpoint URL determination failed.
	ReasonEndpointDeterminationFailed = "EndpointDeterminationFailed"

	// ReasonConfigMapFailed indicates that ConfigMap reconciliation failed.
	ReasonConfigMapFailed = "ConfigMapFailed"

	// ReasonIngressReconcileFailed indicates that Ingress reconciliation failed.
	ReasonIngressReconcileFailed = "IngressReconcileFailed"

	// ReasonSecretCreationFailed indicates that secret creation failed.
	ReasonSecretCreationFailed = "SecretCreationFailed"

	// ReasonOAuthFailed indicates that OAuth resource reconciliation failed.
	ReasonOAuthFailed = "OAuthFailed"

	// ReasonSubCRStatusFailed indicates that fetching sub-CR status failed.
	ReasonSubCRStatusFailed = "SubCRStatusFailed"

	// ReasonCertManagerNotInstalled indicates that cert-manager CRDs are not installed.
	ReasonCertManagerNotInstalled = "CertManagerNotInstalled"

	// ReasonCertManagerInstallationCheckFailed indicates that checking cert-manager availability failed.
	ReasonCertManagerInstallationCheckFailed = "CertManagerInstallationCheckFailed"

	// ReasonCertManagerInstalled indicates that cert-manager CRDs are installed.
	ReasonCertManagerInstalled = "CertManagerInstalled"
)
