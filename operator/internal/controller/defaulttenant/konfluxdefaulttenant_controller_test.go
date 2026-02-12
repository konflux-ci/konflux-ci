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

package defaulttenant

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
)

var _ = Describe("KonfluxDefaultTenant Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: CRName,
		}

		var reconciler *KonfluxDefaultTenantReconciler

		BeforeEach(func() {
			// Ensure cleanup of any existing resource from previous test runs
			resource := &konfluxv1alpha1.KonfluxDefaultTenant{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup existing resource from previous test")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				// Wait for the resource to be fully deleted
				Eventually(func() bool {
					err := k8sClient.Get(ctx, typeNamespacedName, &konfluxv1alpha1.KonfluxDefaultTenant{})
					return errors.IsNotFound(err)
				}, 10*time.Second, 250*time.Millisecond).Should(BeTrue(), "Resource should be deleted before test starts")
			}

			reconciler = &KonfluxDefaultTenantReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}
		})

		AfterEach(func() {
			// Cleanup the resource instance
			resource := &konfluxv1alpha1.KonfluxDefaultTenant{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance KonfluxDefaultTenant")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				// Wait for the resource to be fully deleted
				Eventually(func() bool {
					err := k8sClient.Get(ctx, typeNamespacedName, &konfluxv1alpha1.KonfluxDefaultTenant{})
					return errors.IsNotFound(err)
				}, 10*time.Second, 250*time.Millisecond).Should(BeTrue(), "Resource should be deleted")
			}

			// Cleanup created namespace
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "default-tenant"}, ns); err == nil {
				Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
			By("creating the custom resource")
			resource := &konfluxv1alpha1.KonfluxDefaultTenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the status is ready")
			updatedResource := &konfluxv1alpha1.KonfluxDefaultTenant{}
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

		It("should create the default-tenant namespace", func() {
			By("creating the custom resource")
			resource := &konfluxv1alpha1.KonfluxDefaultTenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the default-tenant namespace was created")
			ns := &corev1.Namespace{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "default-tenant"}, ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(ns.Labels).To(HaveKeyWithValue("konflux-ci.dev/type", "tenant"))
		})

		It("should create the konflux-integration-runner ServiceAccount", func() {
			By("creating the custom resource")
			resource := &konfluxv1alpha1.KonfluxDefaultTenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the konflux-integration-runner ServiceAccount was created")
			sa := &corev1.ServiceAccount{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "konflux-integration-runner", Namespace: "default-tenant"}, sa)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create the RoleBindings", func() {
			By("creating the custom resource")
			resource := &konfluxv1alpha1.KonfluxDefaultTenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the konflux-integration-runner RoleBinding was created")
			rb := &rbacv1.RoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "konflux-integration-runner", Namespace: "default-tenant"}, rb)
			Expect(err).NotTo(HaveOccurred())
			Expect(rb.RoleRef.Name).To(Equal("konflux-integration-runner"))

			By("Verifying the authenticated-konflux-maintainer RoleBinding was created")
			rbAuth := &rbacv1.RoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "authenticated-konflux-maintainer", Namespace: "default-tenant"}, rbAuth)
			Expect(err).NotTo(HaveOccurred())
			Expect(rbAuth.RoleRef.Name).To(Equal("konflux-maintainer-user-actions"))
			// Verify it grants access to all authenticated users
			Expect(rbAuth.Subjects).To(HaveLen(1))
			Expect(rbAuth.Subjects[0].Kind).To(Equal("Group"))
			Expect(rbAuth.Subjects[0].Name).To(Equal("system:authenticated"))
		})
	})
})
