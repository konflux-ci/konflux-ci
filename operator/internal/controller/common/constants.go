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

// Package common provides shared utilities for all Konflux reconcilers.
package common

const (
	// KonfluxOwnerLabel is the label used to identify resources owned by the Konflux operator.
	// Resources are owned either directly by the Konflux CR (e.g., ApplicationAPI CRDs) or
	// indirectly via component-specific CRs (e.g., deployments owned by KonfluxImageController CR).
	KonfluxOwnerLabel = "konflux.konflux-ci.dev/owner"
	// KonfluxComponentLabel is the label used to identify which component a resource belongs to.
	KonfluxComponentLabel = "konflux.konflux-ci.dev/component"
	// KonfluxCRName is the singleton name for the Konflux CR.
	KonfluxCRName = "konflux"
	// ConditionTypeReady is the condition type for overall readiness
	ConditionTypeReady = "Ready"
	// KonfluxBuildServiceCRName is the name for the KonfluxBuildService CR.
	KonfluxBuildServiceCRName = "konflux-build-service"
	// KonfluxIntegrationServiceCRName is the name for the KonfluxIntegrationService CR.
	KonfluxIntegrationServiceCRName = "konflux-integration-service"
	// KonfluxReleaseServiceCRName is the name for the KonfluxReleaseService CR.
	KonfluxReleaseServiceCRName = "konflux-release-service"
	// KonfluxUICRName is the namespace for UI resources
	KonfluxUICRName = "konflux-ui"
	// KonfluxRBACCRName is the name for the KonfluxRBAC CR.
	KonfluxRBACCRName = "konflux-rbac"
	// KonfluxNamespaceListerCRName is the name for the KonfluxNamespaceLister CR.
	KonfluxNamespaceListerCRName = "konflux-namespace-lister"
	// KonfluxEnterpriseContractCRName is the name for the KonfluxEnterpriseContract CR.
	KonfluxEnterpriseContractCRName = "konflux-enterprise-contract"
	// KonfluxImageControllerCRName is the name for the KonfluxImageController CR.
	KonfluxImageControllerCRName = "konflux-image-controller"
	// KonfluxApplicationAPICRName is the name for the KonfluxApplicationAPI CR.
	KonfluxApplicationAPICRName = "konflux-application-api"
	// CertManagerGroup is the API group for cert-manager resources
	CertManagerGroup = "cert-manager.io"
	// KyvernoGroup is the API group for Kyverno resources
	KyvernoGroup = "kyverno.io"
)

