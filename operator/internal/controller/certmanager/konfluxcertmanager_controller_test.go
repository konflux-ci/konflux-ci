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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const certManagerNamespace = "cert-manager"

var _ = Describe("KonfluxCertManager Controller", Ordered, func() {
	// "When the cert-manager namespace does not exist" runs first so the namespace
	// has never been created by another test's BeforeEach.
	Context("When the cert-manager namespace does not exist", func() {
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
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, resource)
			})

			By("waiting for the controller to report the apply failure in status")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.KonfluxCertManager{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
				readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(readyCond.Reason).To(Equal(condition.ReasonApplyFailed))
				g.Expect(readyCond.Message).To(ContainSubstring("apply manifests"))
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})
	})

	Context("When reconciling a resource", func() {
		// Simulate cert-manager being installed: the cert-manager namespace must exist
		// before the controller can apply manifests. Run before each spec (idempotent).
		BeforeEach(func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: certManagerNamespace}}
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(Succeed())
		})

		// waitForReady is a shared helper that blocks until the CR reaches Ready=True.
		waitForReady := func(g Gomega) {
			updated := &konfluxv1alpha1.KonfluxCertManager{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
			readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
			g.Expect(readyCond).NotTo(BeNil())
			g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
		}

		Context("with createClusterIssuer unset (defaults to enabled)", func() {
			It("should successfully reconcile the resource", func(ctx context.Context) {
				Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})).To(Succeed())
				DeferCleanup(func(ctx context.Context) {
					testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxCertManager{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
				})
				Eventually(waitForReady).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
			})
		})

		Context("with createClusterIssuer explicitly enabled", func() {
			It("should successfully reconcile the resource", func(ctx context.Context) {
				enabled := true
				Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec:       konfluxv1alpha1.KonfluxCertManagerSpec{CreateClusterIssuer: &enabled},
				})).To(Succeed())
				DeferCleanup(func(ctx context.Context) {
					testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxCertManager{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
				})
				Eventually(waitForReady).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
			})
		})

		Context("with createClusterIssuer disabled", func() {
			It("should successfully reconcile the resource", func(ctx context.Context) {
				disabled := false
				Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec:       konfluxv1alpha1.KonfluxCertManagerSpec{CreateClusterIssuer: &disabled},
				})).To(Succeed())
				DeferCleanup(func(ctx context.Context) {
					testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxCertManager{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
				})
				Eventually(waitForReady).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
			})
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
