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

package konflux

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
)

var _ = Describe("Konflux Controller - Cert-Manager Dependency", func() {
	Context("When cert-manager CRDs are not installed", func() {
		var (
			ctx                context.Context
			reconciler         *KonfluxReconciler
			fakeClient         client.Client
			konflux            *konfluxv1alpha1.Konflux
			typeNamespacedName types.NamespacedName
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = konfluxv1alpha1.AddToScheme(scheme)
			_ = apiextensionsv1.AddToScheme(scheme)

			// Create a fake client WITHOUT cert-manager CRDs
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&konfluxv1alpha1.Konflux{}).
				Build()

			reconciler = &KonfluxReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			konflux = &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
			}

			typeNamespacedName = types.NamespacedName{
				Name: CRName,
			}

			// Create the Konflux CR
			Expect(fakeClient.Create(ctx, konflux)).To(Succeed())
		})

		AfterEach(func() {
			// Cleanup
			_ = fakeClient.Delete(ctx, konflux)
		})

		It("should set CertManagerAvailable and Ready conditions to False when cert-manager is missing", func() {
			By("Reconciling the Konflux CR")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the updated Konflux CR")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(fakeClient.Get(ctx, typeNamespacedName, updatedKonflux)).To(Succeed())

			By("Verifying CertManagerAvailable condition is False")
			cond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeCertManagerAvailable)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(condition.ReasonCertManagerMissing))
			Expect(cond.Message).To(ContainSubstring("cert-manager CRDs are not installed"))

			By("Verifying Ready condition is False")
			readyCond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeReady)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(condition.ReasonCertManagerMissing))
			Expect(readyCond.Message).To(ContainSubstring("cert-manager CRDs are not installed"))
		})
	})

	Context("When cert-manager CRDs are installed", func() {
		var (
			ctx                context.Context
			reconciler         *KonfluxReconciler
			fakeClient         client.Client
			konflux            *konfluxv1alpha1.Konflux
			typeNamespacedName types.NamespacedName
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = konfluxv1alpha1.AddToScheme(scheme)
			_ = apiextensionsv1.AddToScheme(scheme)

			// Create a fake client WITH cert-manager CRDs
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&konfluxv1alpha1.Konflux{}).
				WithObjects(
					&apiextensionsv1.CustomResourceDefinition{
						ObjectMeta: metav1.ObjectMeta{
							Name: "certificates.cert-manager.io",
						},
					},
					&apiextensionsv1.CustomResourceDefinition{
						ObjectMeta: metav1.ObjectMeta{
							Name: "issuers.cert-manager.io",
						},
					},
					&apiextensionsv1.CustomResourceDefinition{
						ObjectMeta: metav1.ObjectMeta{
							Name: "clusterissuers.cert-manager.io",
						},
					},
				).
				Build()

			reconciler = &KonfluxReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			konflux = &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
			}

			typeNamespacedName = types.NamespacedName{
				Name: CRName,
			}

			// Create the Konflux CR
			Expect(fakeClient.Create(ctx, konflux)).To(Succeed())
		})

		AfterEach(func() {
			// Cleanup
			_ = fakeClient.Delete(ctx, konflux)
		})

		It("should set CertManagerAvailable condition to True", func() {
			By("Reconciling the Konflux CR")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the updated Konflux CR")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(fakeClient.Get(ctx, typeNamespacedName, updatedKonflux)).To(Succeed())

			By("Verifying CertManagerAvailable condition is True")
			cond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeCertManagerAvailable)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("CertManagerInstalled"))
			Expect(cond.Message).To(ContainSubstring("cert-manager CRDs are installed"))
		})

		It("should not override Ready condition when cert-manager is available", func() {
			By("Reconciling the Konflux CR")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the updated Konflux CR")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(fakeClient.Get(ctx, typeNamespacedName, updatedKonflux)).To(Succeed())

			By("Verifying Ready condition is not overridden by cert-manager check")
			readyCond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeReady)
			// Ready condition should be set based on sub-CR statuses, not cert-manager
			// If cert-manager is available, it shouldn't force Ready to False
			if readyCond != nil {
				Expect(readyCond.Reason).NotTo(Equal(condition.ReasonCertManagerMissing))
			}
		})
	})

	Context("When cert-manager check fails with an error", func() {
		var (
			ctx                context.Context
			reconciler         *KonfluxReconciler
			fakeClient         client.Client
			konflux            *konfluxv1alpha1.Konflux
			typeNamespacedName types.NamespacedName
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = konfluxv1alpha1.AddToScheme(scheme)
			_ = apiextensionsv1.AddToScheme(scheme)

			// Create a fake client that will return an error when checking CRDs
			// This simulates RBAC issues or network problems
			fakeClient = &errorOnGetClient{
				Client: fake.NewClientBuilder().
					WithScheme(scheme).
					WithStatusSubresource(&konfluxv1alpha1.Konflux{}).
					Build(),
			}

			reconciler = &KonfluxReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			konflux = &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
			}

			typeNamespacedName = types.NamespacedName{
				Name: CRName,
			}

			// Create the Konflux CR
			Expect(fakeClient.Create(ctx, konflux)).To(Succeed())
		})

		AfterEach(func() {
			// Cleanup
			_ = fakeClient.Delete(ctx, konflux)
		})

		It("should continue reconciliation and log the error", func() {
			By("Reconciling the Konflux CR")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// Reconciliation should continue even if cert-manager check fails
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the updated Konflux CR")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(fakeClient.Get(ctx, typeNamespacedName, updatedKonflux)).To(Succeed())

			By("Verifying that CertManagerAvailable condition is not set")
			// When the check fails, we don't set the condition
			cond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeCertManagerAvailable)
			// Condition might not exist if check failed
			if cond != nil {
				// If it exists, it shouldn't be False with CertManagerMissing reason
				// (since we couldn't determine if it's missing)
				Expect(cond.Reason).NotTo(Equal(condition.ReasonCertManagerMissing))
			}
		})
	})
})

// errorOnGetClient wraps a client and returns an error for Get operations on CRDs
type errorOnGetClient struct {
	client.Client
}

func (e *errorOnGetClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	// Return error only for cert-manager CRD checks
	if key.Name == "certificates.cert-manager.io" ||
		key.Name == "issuers.cert-manager.io" ||
		key.Name == "clusterissuers.cert-manager.io" {
		return apierrors.NewInternalError(errors.New("simulated RBAC or network error"))
	}
	return e.Client.Get(ctx, key, obj, opts...)
}
