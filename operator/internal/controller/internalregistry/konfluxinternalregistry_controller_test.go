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
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

var _ = Describe("KonfluxInternalRegistry Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: CRName,
		}

		var reconciler *KonfluxInternalRegistryReconciler

		BeforeEach(func() {
			// Ensure cleanup of any existing resource from previous test runs
			resource := &konfluxv1alpha1.KonfluxInternalRegistry{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup existing resource from previous test")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				// Wait for the resource to be fully deleted
				Eventually(func() bool {
					err := k8sClient.Get(ctx, typeNamespacedName, &konfluxv1alpha1.KonfluxInternalRegistry{})
					return errors.IsNotFound(err)
				}, 10*time.Second, 250*time.Millisecond).Should(BeTrue(), "Resource should be deleted before test starts")
			}

			reconciler = &KonfluxInternalRegistryReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}
		})

		AfterEach(func() {
			// Cleanup the resource instance
			resource := &konfluxv1alpha1.KonfluxInternalRegistry{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance KonfluxInternalRegistry")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				// Wait for the resource to be fully deleted
				Eventually(func() bool {
					err := k8sClient.Get(ctx, typeNamespacedName, &konfluxv1alpha1.KonfluxInternalRegistry{})
					return errors.IsNotFound(err)
				}, 10*time.Second, 250*time.Millisecond).Should(BeTrue(), "Resource should be deleted")
			}
		})

		It("should successfully reconcile the resource", func() {
			By("creating the custom resource")
			resource := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxInternalRegistrySpec{},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the status condition exists")
			updatedResource := &konfluxv1alpha1.KonfluxInternalRegistry{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedResource)).To(Succeed())

			// Check for Ready condition - in test environment deployments may not be ready
			// because Certificate CRD is not installed, so we just verify the condition exists
			conditions := updatedResource.Status.Conditions
			Expect(conditions).NotTo(BeEmpty())

			var readyCondition *metav1.Condition
			for i := range conditions {
				if conditions[i].Type == condition.TypeReady {
					readyCondition = &conditions[i]
					break
				}
			}
			Expect(readyCondition).NotTo(BeNil(), "Ready condition should be present")
			// In test environment, deployment may not be ready due to missing Certificate Secret
			// We verify reconcile completed without error, condition is set
		})

		It("should set ownership labels on applied resources", func() {
			By("creating the custom resource")
			resource := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxInternalRegistrySpec{},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ownership labels are set on the namespace")
			namespace := &corev1.Namespace{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "kind-registry"}, namespace)
			}, 10*time.Second, 250*time.Millisecond).Should(Succeed())

			labels := namespace.GetLabels()
			Expect(labels).NotTo(BeNil())
			Expect(labels[constant.KonfluxOwnerLabel]).To(Equal(CRName))
			Expect(labels[constant.KonfluxComponentLabel]).To(Equal("registry"))

			By("Verifying owner reference is set on the namespace")
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
