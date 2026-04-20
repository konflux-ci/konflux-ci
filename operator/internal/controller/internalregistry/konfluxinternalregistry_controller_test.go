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

package internalregistry

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const internalRegistryNamespace = "kind-registry"

var _ = Describe("KonfluxInternalRegistry Controller", func() {
	Context("When reconciling a resource", Ordered, func() {
		BeforeAll(func() {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())

			// Wait for Deployment existence as proof of successful reconciliation.
			// Ready=True is not achievable in envtest (no kubelet → pods never start).
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "registry",
					Namespace: internalRegistryNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})

		AfterAll(func() {
			testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxInternalRegistry{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
		})

		It("should successfully reconcile the resource", func() {
			updated := &konfluxv1alpha1.KonfluxInternalRegistry{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
			Expect(updated.Status.Conditions).NotTo(BeEmpty())
			readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
			Expect(readyCond).NotTo(BeNil(), "Ready condition should be present")
		})

		It("should set ownership labels on applied resources", func() {
			namespace := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: internalRegistryNamespace}, namespace)).To(Succeed())

			labels := namespace.GetLabels()
			Expect(labels).NotTo(BeNil())
			Expect(labels[constant.KonfluxOwnerLabel]).To(Equal(CRName))
			Expect(labels[constant.KonfluxComponentLabel]).To(Equal("registry"))

			ownerRefs := namespace.GetOwnerReferences()
			Expect(ownerRefs).NotTo(BeEmpty())
			found := false
			for _, ref := range ownerRefs {
				if ref.Name == CRName && ref.Kind == "KonfluxInternalRegistry" {
					found = true
					Expect(ref.Controller).NotTo(BeNil())
					Expect(*ref.Controller).To(BeTrue())
					break
				}
			}
			Expect(found).To(BeTrue(), "Owner reference should be set")
		})
	})

	Context("tracking.IsNoKindMatchError helper function", func() {
		It("should correctly identify NoKindMatchError", func() {
			noKindErr := &meta.NoKindMatchError{
				GroupKind: schema.GroupKind{Group: "cert-manager.io", Kind: "Certificate"},
			}
			Expect(tracking.IsNoKindMatchError(noKindErr)).To(BeTrue())

			otherErr := fmt.Errorf("some other error")
			Expect(tracking.IsNoKindMatchError(otherErr)).To(BeFalse())
		})

		It("should return false for wrapped errors that are not NoKindMatchError", func() {
			wrappedErr := fmt.Errorf("wrapped: %w", fmt.Errorf("inner error"))
			Expect(tracking.IsNoKindMatchError(wrappedErr)).To(BeFalse())
		})

		It("should return true for wrapped NoKindMatchError", func() {
			noKindErr := &meta.NoKindMatchError{
				GroupKind: schema.GroupKind{Group: "trust.cert-manager.io", Kind: "Bundle"},
			}
			wrappedErr := fmt.Errorf("failed to list resources: %w", noKindErr)
			Expect(tracking.IsNoKindMatchError(wrappedErr)).To(BeTrue())
		})
	})
})
