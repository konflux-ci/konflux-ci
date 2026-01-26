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

package certmanager

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
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

var _ = Describe("KonfluxCertManager Controller", Ordered, func() {
	// "When the cert-manager namespace does not exist" runs first so the namespace
	// has never been created by another test's BeforeEach.
	Context("When the cert-manager namespace does not exist", func() {
		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{Name: CRName}

		It("should fail apply and report error when createClusterIssuer is enabled", func() {
			By("creating the custom resource with createClusterIssuer enabled")
			enabled := true
			resource := &konfluxv1alpha1.KonfluxCertManager{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxCertManagerSpec{
					CreateClusterIssuer: &enabled,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			reconciler := &KonfluxCertManagerReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			By("reconciling fails because the cert-manager namespace does not exist")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cert-manager"))
			Expect(err.Error()).To(ContainSubstring("not found"))

			By("status reflects the apply failure")
			updated := &konfluxv1alpha1.KonfluxCertManager{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(condition.ReasonApplyFailed))
			Expect(readyCond.Message).To(ContainSubstring("apply manifests"))
		})
	})

	Context("When reconciling a resource", func() {
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: CRName,
		}

		var reconciler *KonfluxCertManagerReconciler

		BeforeEach(func() {
			// Simulate cert-manager being installed: the cert-manager namespace must exist
			// (the controller no longer creates it; whoever installs cert-manager creates it).
			ns := &corev1.Namespace{}
			ns.Name = "cert-manager"
			getErr := k8sClient.Get(ctx, types.NamespacedName{Name: ns.Name}, ns)
			if errors.IsNotFound(getErr) {
				Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			} else {
				Expect(getErr).NotTo(HaveOccurred())
			}

			// Ensure cleanup of any existing resource from previous test runs
			resource := &konfluxv1alpha1.KonfluxCertManager{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup existing resource from previous test")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				// Wait for the resource to be fully deleted
				Eventually(func() bool {
					err := k8sClient.Get(ctx, typeNamespacedName, &konfluxv1alpha1.KonfluxCertManager{})
					return errors.IsNotFound(err)
				}, 10*time.Second, 250*time.Millisecond).Should(BeTrue(), "Resource should be deleted before test starts")
			}

			reconciler = &KonfluxCertManagerReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}
		})

		AfterEach(func() {
			// Cleanup the resource instance
			resource := &konfluxv1alpha1.KonfluxCertManager{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance KonfluxCertManager")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				// Wait for the resource to be fully deleted
				Eventually(func() bool {
					err := k8sClient.Get(ctx, typeNamespacedName, &konfluxv1alpha1.KonfluxCertManager{})
					return errors.IsNotFound(err)
				}, 10*time.Second, 250*time.Millisecond).Should(BeTrue(), "Resource should be deleted")
			}
		})

		It("should successfully reconcile with createClusterIssuer enabled (default)", func() {
			By("creating the custom resource with createClusterIssuer unset (defaults to true)")
			resource := &konfluxv1alpha1.KonfluxCertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxCertManagerSpec{
					// CreateClusterIssuer is nil, should default to true
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the status is ready")
			updatedResource := &konfluxv1alpha1.KonfluxCertManager{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedResource)).To(Succeed())

			// Check for Ready condition
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
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Message).To(ContainSubstring("Component ready"))
		})

		It("should successfully reconcile with createClusterIssuer explicitly enabled", func() {
			By("creating the custom resource with createClusterIssuer=true")
			enabled := true
			resource := &konfluxv1alpha1.KonfluxCertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxCertManagerSpec{
					CreateClusterIssuer: &enabled,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the status is ready")
			updatedResource := &konfluxv1alpha1.KonfluxCertManager{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedResource)).To(Succeed())

			conditions := updatedResource.Status.Conditions
			Expect(conditions).NotTo(BeEmpty())

			var readyCondition *metav1.Condition
			for i := range conditions {
				if conditions[i].Type == condition.TypeReady {
					readyCondition = &conditions[i]
					break
				}
			}
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should successfully reconcile with createClusterIssuer disabled", func() {
			By("creating the custom resource with createClusterIssuer=false")
			disabled := false
			resource := &konfluxv1alpha1.KonfluxCertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxCertManagerSpec{
					CreateClusterIssuer: &disabled,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the status is ready (no deployments to track)")
			updatedResource := &konfluxv1alpha1.KonfluxCertManager{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedResource)).To(Succeed())

			conditions := updatedResource.Status.Conditions
			Expect(conditions).NotTo(BeEmpty())

			var readyCondition *metav1.Condition
			for i := range conditions {
				if conditions[i].Type == condition.TypeReady {
					readyCondition = &conditions[i]
					break
				}
			}
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCondition.Message).To(ContainSubstring("Component ready"))
		})
	})

	Context("tracking.IsNoKindMatchError helper function", func() {
		It("should correctly identify NoKindMatchError", func() {
			noKindErr := &meta.NoKindMatchError{
				GroupKind: schema.GroupKind{Group: "cert-manager.io", Kind: "ClusterIssuer"},
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
				GroupKind: schema.GroupKind{Group: "cert-manager.io", Kind: "Certificate"},
			}
			wrappedErr := fmt.Errorf("failed to list resources: %w", noKindErr)
			Expect(tracking.IsNoKindMatchError(wrappedErr)).To(BeTrue())
		})
	})
})
