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

package cli

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
)

var _ = Describe("KonfluxCLI Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: CRName,
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KonfluxCLI")
			resource := &konfluxv1alpha1.KonfluxCLI{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err != nil && errors.IsNotFound(err) {
				resource = &konfluxv1alpha1.KonfluxCLI{
					ObjectMeta: metav1.ObjectMeta{
						Name: CRName,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &konfluxv1alpha1.KonfluxCLI{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance KonfluxCLI")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

				Eventually(func() bool {
					err := k8sClient.Get(ctx, typeNamespacedName, &konfluxv1alpha1.KonfluxCLI{})
					return errors.IsNotFound(err)
				}, 10*time.Second, 250*time.Millisecond).Should(BeTrue(), "Resource should be deleted")
			}
		})

		It("should successfully reconcile the resource", func() {
			reconciler := &KonfluxCLIReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should set Ready=True status after reconciliation", func() {
			reconciler := &KonfluxCLIReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the status is ready")
			updated := &konfluxv1alpha1.KonfluxCLI{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())

			readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
			Expect(readyCond).NotTo(BeNil(), "Ready condition should be present")
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Message).To(ContainSubstring("Component ready"))
		})

		It("should create the CLI namespace and ConfigMaps", func() {
			reconciler := &KonfluxCLIReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the konflux-cli namespace was created")
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "konflux-cli"}, ns)).To(Succeed())

			By("Verifying the owner label is set on the namespace")
			Expect(ns.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))

			By("Verifying the create-tenant ConfigMap was created")
			createTenantCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "create-tenant",
				Namespace: "konflux-cli",
			}, createTenantCM)).To(Succeed())
			Expect(createTenantCM.Data).To(HaveKey("create-tenant.sh"))

			By("Verifying the setup-release ConfigMap was created")
			setupReleaseCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "setup-release",
				Namespace: "konflux-cli",
			}, setupReleaseCM)).To(Succeed())
			Expect(setupReleaseCM.Data).To(HaveKey("setup-release.sh"))

			By("Verifying the setup-component ConfigMap was created")
			setupComponentCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "setup-component",
				Namespace: "konflux-cli",
			}, setupComponentCM)).To(Succeed())
			Expect(setupComponentCM.Data).To(HaveKey("setup-component.sh"))
		})

		It("should return no error when the CR does not exist", func() {
			reconciler := &KonfluxCLIReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			By("Reconciling a non-existent resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "nonexistent"},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
