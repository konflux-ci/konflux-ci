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

package constant

// Shared constants used across all controllers
const (
	// KonfluxOwnerLabel is the label used to identify resources owned by the Konflux operator.
	KonfluxOwnerLabel = "konflux.konflux-ci.dev/owner"
	// KonfluxComponentLabel is the label used to identify which component a resource belongs to.
	KonfluxComponentLabel = "konflux.konflux-ci.dev/component"
	// ConditionTypeReady is the condition type for overall readiness
	ConditionTypeReady = "Ready"
	// ConditionTypeCertManagerAvailable is the condition type for cert-manager availability
	ConditionTypeCertManagerAvailable = "CertManagerAvailable"
	// CertManagerGroup is the API group for cert-manager resources
	CertManagerGroup = "cert-manager.io"
	// KyvernoGroup is the API group for Kyverno resources
	KyvernoGroup = "kyverno.io"
)
