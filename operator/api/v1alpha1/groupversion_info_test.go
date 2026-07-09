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
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddToScheme(t *testing.T) {
	g := gomega.NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(AddToScheme(scheme)).To(gomega.Succeed())

	for _, kind := range []string{
		"Konflux",
		"KonfluxList",
		"KonfluxApplicationAPI",
		"KonfluxApplicationAPIList",
		"KonfluxBuildService",
		"KonfluxBuildServiceList",
		"KonfluxCertManager",
		"KonfluxCertManagerList",
		"KonfluxCLI",
		"KonfluxCLIList",
		"KonfluxDefaultTenant",
		"KonfluxDefaultTenantList",
		"KonfluxEnterpriseContract",
		"KonfluxEnterpriseContractList",
		"KonfluxImageController",
		"KonfluxImageControllerList",
		"KonfluxInfo",
		"KonfluxInfoList",
		"KonfluxIntegrationService",
		"KonfluxIntegrationServiceList",
		"KonfluxInternalRegistry",
		"KonfluxInternalRegistryList",
		"KonfluxNamespaceLister",
		"KonfluxNamespaceListerList",
		"KonfluxRBAC",
		"KonfluxRBACList",
		"KonfluxReleaseService",
		"KonfluxReleaseServiceList",
		"KonfluxSegmentBridge",
		"KonfluxSegmentBridgeList",
		"KonfluxUI",
		"KonfluxUIList",
	} {
		gvk := GroupVersion.WithKind(kind)
		obj, err := scheme.New(gvk)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "expected %s to be registered", gvk)
		g.Expect(obj).NotTo(gomega.BeNil())
	}
}
