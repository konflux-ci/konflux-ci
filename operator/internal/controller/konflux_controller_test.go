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
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

var _ = Describe("Konflux Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "konflux"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		konflux := &konfluxv1alpha1.Konflux{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Konflux")
			err := k8sClient.Get(ctx, typeNamespacedName, konflux)
			if err != nil && errors.IsNotFound(err) {
				resource := &konfluxv1alpha1.Konflux{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &konfluxv1alpha1.Konflux{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance Konflux")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KonfluxReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})

	Context("Konflux Name Validation (CEL)", func() {
		const requiredKonfluxName = "konflux"

		AfterEach(func(ctx context.Context) {
			// Clean up any Konflux instances created during tests
			konfluxList := &konfluxv1alpha1.KonfluxList{}
			if err := k8sClient.List(ctx, konfluxList); err == nil {
				for _, item := range konfluxList.Items {
					if err := k8sClient.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
						_, _ = fmt.Fprintf(
							GinkgoWriter,
							"Failed to delete Konflux %q: %v\n",
							item.GetName(),
							err,
						)
					}
				}
			}
		})

		It("Should allow creation with the required name 'konflux'", func(ctx context.Context) {
			By("creating a Konflux instance with the required name")
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: requiredKonfluxName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{},
			}
			err := k8sClient.Create(ctx, konflux)
			Expect(err).NotTo(HaveOccurred(), "Creation with required name should be allowed")

			By("verifying the instance was created")
			created := &konfluxv1alpha1.Konflux{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: requiredKonfluxName}, created)
			Expect(err).NotTo(HaveOccurred())
			Expect(created.GetName()).To(Equal(requiredKonfluxName))
		})

		It("Should deny creation with a different name", func(ctx context.Context) {
			By("attempting to create a Konflux instance with a different name")
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-konflux",
				},
				Spec: konfluxv1alpha1.KonfluxSpec{},
			}
			err := k8sClient.Create(ctx, konflux)
			Expect(err).To(HaveOccurred(), "Creation with different name should be rejected")
			Expect(err.Error()).To(ContainSubstring("konflux"), "Error message should mention 'konflux'")
		})

		It("Should allow updates to the instance with the required name", func(ctx context.Context) {
			By("creating a Konflux instance with the required name")
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: requiredKonfluxName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{},
			}
			err := k8sClient.Create(ctx, konflux)
			Expect(err).NotTo(HaveOccurred(), "Failed to create Konflux instance")

			By("updating the instance")
			// Get the latest version
			updated := &konfluxv1alpha1.Konflux{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: requiredKonfluxName}, updated)
			Expect(err).NotTo(HaveOccurred())

			// Add a label
			if updated.Labels == nil {
				updated.Labels = make(map[string]string)
			}
			updated.Labels["test"] = "value"
			err = k8sClient.Update(ctx, updated)
			Expect(err).NotTo(HaveOccurred(), "Updates should be allowed")
		})
	})
})
