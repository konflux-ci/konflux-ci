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

// Package v1alpha1 contains API Schema definitions for the konflux v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=konflux.konflux-ci.dev
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "konflux.konflux-ci.dev", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// addKnownTypes registers all v1alpha1 types with the scheme.
// When adding a new CR, register its type and list type here and in TestAddToScheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&Konflux{}, &KonfluxList{},
		&KonfluxApplicationAPI{}, &KonfluxApplicationAPIList{},
		&KonfluxBuildService{}, &KonfluxBuildServiceList{},
		&KonfluxCertManager{}, &KonfluxCertManagerList{},
		&KonfluxCLI{}, &KonfluxCLIList{},
		&KonfluxDefaultTenant{}, &KonfluxDefaultTenantList{},
		&KonfluxEnterpriseContract{}, &KonfluxEnterpriseContractList{},
		&KonfluxImageController{}, &KonfluxImageControllerList{},
		&KonfluxInfo{}, &KonfluxInfoList{},
		&KonfluxIntegrationService{}, &KonfluxIntegrationServiceList{},
		&KonfluxInternalRegistry{}, &KonfluxInternalRegistryList{},
		&KonfluxNamespaceLister{}, &KonfluxNamespaceListerList{},
		&KonfluxRBAC{}, &KonfluxRBACList{},
		&KonfluxReleaseService{}, &KonfluxReleaseServiceList{},
		&KonfluxSegmentBridge{}, &KonfluxSegmentBridgeList{},
		&KonfluxUI{}, &KonfluxUIList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
