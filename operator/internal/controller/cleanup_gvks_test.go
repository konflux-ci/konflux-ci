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

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

// crdGVK is the GVK for CustomResourceDefinitions, which are excluded from cleanup.
// CRDs are cluster-scoped and have different lifecycle management requirements.
var crdGVK = schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}

// Ginkgo table test that verifies cleanup GVK lists cover all manifest resource types.
// CRDs are excluded since they are cluster-scoped and managed separately.
// This runs within the test suite where objectStore is initialized.
var _ = DescribeTable("Cleanup GVKs should cover all resource types in manifests (excluding CRDs)",
	func(component manifests.Component, cleanupGVKs []schema.GroupVersionKind) {
		objects, err := objectStore.GetForComponent(component)
		Expect(err).NotTo(HaveOccurred(), "Failed to get objects for component %s", component)

		cleanupSet := sets.New(cleanupGVKs...)

		// Collect unique GVKs from manifests, excluding CRDs
		var manifestGVKs []schema.GroupVersionKind
		for _, obj := range objects {
			gvk := obj.GetObjectKind().GroupVersionKind()
			if gvk != crdGVK {
				manifestGVKs = append(manifestGVKs, gvk)
			}
		}

		// Skip components that only contain CRDs (e.g., application-api)
		if len(manifestGVKs) == 0 {
			Skip("Component only contains CRDs, which are excluded from cleanup")
		}

		manifestSet := sets.New(manifestGVKs...)

		// Find GVKs in manifests but not in cleanup list
		missingGVKs := manifestSet.Difference(cleanupSet)

		Expect(missingGVKs.Len()).To(Equal(0),
			"Cleanup GVKs for %s are missing the following types found in manifests: %v\n"+
				"Add these GVKs to the cleanup list to ensure orphaned resources are properly cleaned up.",
			component, missingGVKs.UnsortedList())
	},
	Entry("application-api", manifests.ApplicationAPI, applicationAPICleanupGVKs),
	Entry("build-service", manifests.BuildService, buildServiceCleanupGVKs),
	Entry("integration", manifests.Integration, integrationServiceCleanupGVKs),
	Entry("release", manifests.Release, releaseServiceCleanupGVKs),
	Entry("rbac", manifests.RBAC, rbacCleanupGVKs),
	Entry("namespace-lister", manifests.NamespaceLister, namespaceListerCleanupGVKs),
	Entry("enterprise-contract", manifests.EnterpriseContract, enterpriseContractCleanupGVKs),
	Entry("image-controller", manifests.ImageController, imageControllerCleanupGVKs),
	Entry("info", manifests.Info, infoCleanupGVKs),
	Entry("cert-manager", manifests.CertManager, certManagerCleanupGVKs),
	Entry("registry", manifests.Registry, internalRegistryCleanupGVKs),
)
