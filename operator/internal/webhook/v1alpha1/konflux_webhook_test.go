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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

var _ = Describe("Konflux Webhook", func() {
	var (
		obj       *konfluxv1alpha1.Konflux
		oldObj    *konfluxv1alpha1.Konflux
		validator *KonfluxCustomValidator
		ctx       context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		obj = &konfluxv1alpha1.Konflux{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-konflux",
			},
			Spec: konfluxv1alpha1.KonfluxSpec{},
		}
		oldObj = &konfluxv1alpha1.Konflux{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-konflux",
			},
			Spec: konfluxv1alpha1.KonfluxSpec{},
		}
		validator = &KonfluxCustomValidator{Client: k8sClient}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
	})

	AfterEach(func() {
		// Clean up any Konflux instances created during tests
		konfluxList := &konfluxv1alpha1.KonfluxList{}
		if err := k8sClient.List(ctx, konfluxList); err == nil {
			for _, item := range konfluxList.Items {
				err := k8sClient.Delete(ctx, &item)
				// Allow NotFound errors (resource already deleted) but fail on other errors
				Expect(err == nil || errors.IsNotFound(err)).To(BeTrue(),
					"Delete should succeed or resource should not be found")
			}
		}
	})

	Context("When creating Konflux under Validating Webhook", func() {
		It("Should allow creation of the first instance", func() {
			By("creating the first Konflux instance")
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred(), "First instance should be allowed")
			Expect(warnings).To(BeEmpty())
		})

		It("Should deny creation of a second instance", func() {
			By("creating the first Konflux instance")
			firstInstance := &konfluxv1alpha1.Konflux{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "konflux.konflux-ci.dev/v1alpha1",
					Kind:       "Konflux",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "first-konflux",
				},
				Spec: konfluxv1alpha1.KonfluxSpec{},
			}

			Expect(k8sClient.Create(ctx, firstInstance)).To(Succeed(),
				"First instance should be created successfully")

			By("attempting to create a second instance")
			secondInstance := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: "second-konflux",
				},
				Spec: konfluxv1alpha1.KonfluxSpec{},
			}
			warnings, err := validator.ValidateCreate(ctx, secondInstance)
			Expect(err).To(HaveOccurred(), "Second instance should be rejected")
			Expect(err.Error()).To(ContainSubstring("only one Konflux instance is allowed per cluster"))
			Expect(err.Error()).To(ContainSubstring("first-konflux"))
			Expect(warnings).To(BeEmpty())

			By("cleaning up the first instance")
			Expect(k8sClient.Delete(ctx, firstInstance)).To(Succeed())
		})
	})

	Context("When updating Konflux under Validating Webhook", func() {
		It("Should allow updates to existing instances", func() {
			By("creating a Konflux instance")

			instance := &konfluxv1alpha1.Konflux{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "konflux.konflux-ci.dev/v1alpha1",
					Kind:       "Konflux",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-konflux",
				},
				Spec: konfluxv1alpha1.KonfluxSpec{},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			By("updating the instance")
			updatedInstance := instance.DeepCopy()
			warnings, err := validator.ValidateUpdate(ctx, instance, updatedInstance)
			Expect(err).NotTo(HaveOccurred(), "Updates should be allowed")
			Expect(warnings).To(BeEmpty())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, instance)).To(Succeed())
		})
	})
})
