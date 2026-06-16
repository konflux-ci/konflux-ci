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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	certManagerNamespace = "cert-manager"
	bootstrapIssuerName  = "konflux-bootstrap-issuer"
	issuerName           = "konflux-issuer"
	certificateName      = "konflux-ca"
)

// clusterIssuerGVK is the GVK for cert-manager ClusterIssuer resources.
var clusterIssuerGVK = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "ClusterIssuer"}

// certificateGVK is the GVK for cert-manager Certificate resources.
var certificateGVK = schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"}

// newClusterIssuer returns an unstructured object suitable for k8sClient.Get calls.
func newClusterIssuer(name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(clusterIssuerGVK)
	obj.SetName(name)
	return obj
}

// newCertificate returns an unstructured object suitable for k8sClient.Get calls.
func newCertificate(name, namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(certificateGVK)
	obj.SetName(name)
	obj.SetNamespace(namespace)
	return obj
}

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
				g.Expect(readyCond.Message).To(ContainSubstring("cert-manager"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
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

		// Clean up after each spec. DeleteAndWait is a no-op when the object is already absent,
		// so all deletions are safe to run unconditionally.
		// ClusterIssuers are cleaned up explicitly because envtest has no GC controller, so
		// OwnerReferences do not trigger cascading deletion.
		AfterEach(func(ctx context.Context) {
			testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxCertManager{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
			testutil.DeleteAndWait(ctx, k8sClient, newClusterIssuer(bootstrapIssuerName))
			testutil.DeleteAndWait(ctx, k8sClient, newClusterIssuer(issuerName))
			testutil.DeleteAndWait(ctx, k8sClient, newCertificate(certificateName, certManagerNamespace))
		})

		Context("with createClusterIssuer unset (defaults to enabled)", func() {
			It("should successfully reconcile the resource and create ClusterIssuers", func(ctx context.Context) {
				Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})).To(Succeed())
				Eventually(waitForReady).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying ClusterIssuers were created")
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bootstrapIssuerName}, newClusterIssuer(bootstrapIssuerName))).To(Succeed())
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, newClusterIssuer(issuerName))).To(Succeed())
			})
		})

		Context("with createClusterIssuer explicitly enabled", func() {
			It("should successfully reconcile the resource and create ClusterIssuers", func(ctx context.Context) {
				enabled := true
				Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec:       konfluxv1alpha1.KonfluxCertManagerSpec{CreateClusterIssuer: &enabled},
				})).To(Succeed())
				Eventually(waitForReady).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying ClusterIssuers were created")
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bootstrapIssuerName}, newClusterIssuer(bootstrapIssuerName))).To(Succeed())
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, newClusterIssuer(issuerName))).To(Succeed())
			})
		})

		Context("with createClusterIssuer disabled", func() {
			It("should successfully reconcile the resource and not create ClusterIssuers", func(ctx context.Context) {
				disabled := false
				Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec:       konfluxv1alpha1.KonfluxCertManagerSpec{CreateClusterIssuer: &disabled},
				})).To(Succeed())
				Eventually(waitForReady).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying no ClusterIssuers were created")
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bootstrapIssuerName}, newClusterIssuer(bootstrapIssuerName))).
					To(MatchError(ContainSubstring("not found")))
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, newClusterIssuer(issuerName))).
					To(MatchError(ContainSubstring("not found")))
			})
		})

		Context("Self-healing", func() {
			It("recreates ClusterIssuer when deleted", func(ctx context.Context) {
				Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})).To(Succeed())

				By("waiting for initial ClusterIssuer creation")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, newClusterIssuer(issuerName))).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("deleting the ClusterIssuer")
				Expect(k8sClient.Delete(ctx, newClusterIssuer(issuerName))).To(Succeed())

				By("verifying the ClusterIssuer is recreated")
				Eventually(func(g Gomega) {
					obj := newClusterIssuer(issuerName)
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, obj)).To(Succeed())
					labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
					g.Expect(labels).To(HaveKey(constant.KonfluxOwnerLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})

			It("recreates Certificate when deleted", func(ctx context.Context) {
				Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})).To(Succeed())

				certNN := types.NamespacedName{
					Name:      certificateName,
					Namespace: certManagerNamespace,
				}

				By("waiting for initial Certificate creation")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, certNN, newCertificate(certNN.Name, certNN.Namespace))).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("deleting the Certificate")
				Expect(k8sClient.Delete(ctx, newCertificate(certNN.Name, certNN.Namespace))).To(Succeed())

				By("verifying the Certificate is recreated with ownership labels")
				Eventually(func(g Gomega) {
					obj := newCertificate(certNN.Name, certNN.Namespace)
					g.Expect(k8sClient.Get(ctx, certNN, obj)).To(Succeed())
					labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
					g.Expect(labels).To(HaveKey(constant.KonfluxOwnerLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})

		})

	})

	Context("tracking.IsNoKindMatchError helper function", func() {
		It("should correctly identify NoKindMatchError", func() {
			noKindErr := &meta.NoKindMatchError{
				GroupKind: clusterIssuerGVK.GroupKind(),
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
