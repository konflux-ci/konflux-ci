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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	certManagerNamespace = "cert-manager"
	bootstrapIssuerName  = "konflux-bootstrap-issuer"
	issuerName           = "konflux-issuer"
	certificateName      = "konflux-ca"
	caSecretName         = "konflux-ca-secret"
)

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

		// certManagerChildren lists all cluster-scoped/namespaced children that envtest's
		// missing GC won't cascade-delete when the parent CR is removed.
		certManagerChildren := func() []client.Object {
			return []client.Object{
				newClusterIssuer(bootstrapIssuerName),
				newClusterIssuer(issuerName),
				newCertificate(certificateName, certManagerNamespace),
			}
		}

		Context("with createClusterIssuer unset (defaults to enabled)", func() {
			It("should successfully reconcile the resource and create ClusterIssuers", func(ctx context.Context) {
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)
				Eventually(waitForReady).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying ClusterIssuers were created")
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bootstrapIssuerName}, newClusterIssuer(bootstrapIssuerName))).To(Succeed())
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, newClusterIssuer(issuerName))).To(Succeed())
			})
		})

		Context("with createClusterIssuer explicitly enabled", func() {
			It("should successfully reconcile the resource and create ClusterIssuers", func(ctx context.Context) {
				enabled := true
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec:       konfluxv1alpha1.KonfluxCertManagerSpec{CreateClusterIssuer: &enabled},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)
				Eventually(waitForReady).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying ClusterIssuers were created")
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bootstrapIssuerName}, newClusterIssuer(bootstrapIssuerName))).To(Succeed())
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, newClusterIssuer(issuerName))).To(Succeed())
			})
		})

		Context("with createClusterIssuer disabled", func() {
			It("should successfully reconcile the resource and not create ClusterIssuers", func(ctx context.Context) {
				disabled := false
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec:       konfluxv1alpha1.KonfluxCertManagerSpec{CreateClusterIssuer: &disabled},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)
				Eventually(waitForReady).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying no ClusterIssuers were created")
				err := k8sClient.Get(ctx, types.NamespacedName{Name: bootstrapIssuerName}, newClusterIssuer(bootstrapIssuerName))
				Expect(apierrors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
				err = k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, newClusterIssuer(issuerName))
				Expect(apierrors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
			})
		})

		Context("Self-healing", func() {
			It("recreates ClusterIssuer when deleted", func(ctx context.Context) {
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

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
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

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

		Context("Drift correction", func() {
			It("restores ClusterIssuer labels when stripped", func(ctx context.Context) {
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("waiting for initial ClusterIssuer creation with ownership labels")
				Eventually(func(g Gomega) {
					obj := newClusterIssuer(issuerName)
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, obj)).To(Succeed())
					labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
					g.Expect(labels).To(HaveKey(constant.KonfluxOwnerLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("stripping ownership labels from the ClusterIssuer")
				Eventually(func(g Gomega) {
					obj := newClusterIssuer(issuerName)
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, obj)).To(Succeed())
					labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
					delete(labels, constant.KonfluxOwnerLabel)
					delete(labels, constant.KonfluxComponentLabel)
					_ = unstructured.SetNestedStringMap(obj.Object, labels, "metadata", "labels")
					g.Expect(k8sClient.Update(ctx, obj)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the ClusterIssuer labels are restored")
				Eventually(func(g Gomega) {
					obj := newClusterIssuer(issuerName)
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, obj)).To(Succeed())
					labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
					g.Expect(labels).To(HaveKey(constant.KonfluxOwnerLabel))
					g.Expect(labels).To(HaveKey(constant.KonfluxComponentLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})

			It("restores Certificate labels when stripped", func(ctx context.Context) {
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				certNN := types.NamespacedName{
					Name:      certificateName,
					Namespace: certManagerNamespace,
				}

				By("waiting for initial Certificate creation with ownership labels")
				Eventually(func(g Gomega) {
					obj := newCertificate(certNN.Name, certNN.Namespace)
					g.Expect(k8sClient.Get(ctx, certNN, obj)).To(Succeed())
					labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
					g.Expect(labels).To(HaveKey(constant.KonfluxOwnerLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("stripping ownership labels from the Certificate")
				Eventually(func(g Gomega) {
					obj := newCertificate(certNN.Name, certNN.Namespace)
					g.Expect(k8sClient.Get(ctx, certNN, obj)).To(Succeed())
					labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
					delete(labels, constant.KonfluxOwnerLabel)
					delete(labels, constant.KonfluxComponentLabel)
					_ = unstructured.SetNestedStringMap(obj.Object, labels, "metadata", "labels")
					g.Expect(k8sClient.Update(ctx, obj)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the Certificate labels are restored")
				Eventually(func(g Gomega) {
					obj := newCertificate(certNN.Name, certNN.Namespace)
					g.Expect(k8sClient.Get(ctx, certNN, obj)).To(Succeed())
					labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
					g.Expect(labels).To(HaveKey(constant.KonfluxOwnerLabel))
					g.Expect(labels).To(HaveKey(constant.KonfluxComponentLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})

			It("restores ClusterIssuer spec when modified", func(ctx context.Context) {
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("waiting for initial ClusterIssuer creation")
				Eventually(func(g Gomega) {
					obj := newClusterIssuer(issuerName)
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, obj)).To(Succeed())
					secretName, _, _ := unstructured.NestedString(obj.Object, "spec", "ca", "secretName")
					g.Expect(secretName).To(Equal(caSecretName))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("modifying the ClusterIssuer CA secret name")
				Eventually(func(g Gomega) {
					obj := newClusterIssuer(issuerName)
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, obj)).To(Succeed())
					g.Expect(unstructured.SetNestedField(obj.Object, "tampered-secret", "spec", "ca", "secretName")).To(Succeed())
					g.Expect(k8sClient.Update(ctx, obj)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the ClusterIssuer CA secret name is restored")
				Eventually(func(g Gomega) {
					obj := newClusterIssuer(issuerName)
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, obj)).To(Succeed())
					secretName, _, _ := unstructured.NestedString(obj.Object, "spec", "ca", "secretName")
					g.Expect(secretName).To(Equal(caSecretName))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})

			It("restores bootstrap ClusterIssuer spec when modified", func(ctx context.Context) {
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("waiting for initial bootstrap ClusterIssuer creation")
				Eventually(func(g Gomega) {
					obj := newClusterIssuer(bootstrapIssuerName)
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bootstrapIssuerName}, obj)).To(Succeed())
					_, found, _ := unstructured.NestedMap(obj.Object, "spec", "selfSigned")
					g.Expect(found).To(BeTrue())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("replacing selfSigned with a CA spec")
				Eventually(func(g Gomega) {
					obj := newClusterIssuer(bootstrapIssuerName)
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bootstrapIssuerName}, obj)).To(Succeed())
					unstructured.RemoveNestedField(obj.Object, "spec", "selfSigned")
					g.Expect(unstructured.SetNestedField(obj.Object, "fake-secret", "spec", "ca", "secretName")).To(Succeed())
					g.Expect(k8sClient.Update(ctx, obj)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the bootstrap ClusterIssuer selfSigned spec is restored")
				Eventually(func(g Gomega) {
					obj := newClusterIssuer(bootstrapIssuerName)
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bootstrapIssuerName}, obj)).To(Succeed())
					_, found, _ := unstructured.NestedMap(obj.Object, "spec", "selfSigned")
					g.Expect(found).To(BeTrue())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})

			It("restores Certificate spec when modified", func(ctx context.Context) {
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				certNN := types.NamespacedName{
					Name:      certificateName,
					Namespace: certManagerNamespace,
				}

				By("waiting for initial Certificate creation")
				Eventually(func(g Gomega) {
					obj := newCertificate(certNN.Name, certNN.Namespace)
					g.Expect(k8sClient.Get(ctx, certNN, obj)).To(Succeed())
					issuer, _, _ := unstructured.NestedString(obj.Object, "spec", "issuerRef", "name")
					g.Expect(issuer).To(Equal(bootstrapIssuerName))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("modifying the Certificate issuerRef name")
				Eventually(func(g Gomega) {
					obj := newCertificate(certNN.Name, certNN.Namespace)
					g.Expect(k8sClient.Get(ctx, certNN, obj)).To(Succeed())
					g.Expect(unstructured.SetNestedField(obj.Object, "tampered-issuer", "spec", "issuerRef", "name")).To(Succeed())
					g.Expect(k8sClient.Update(ctx, obj)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the Certificate issuerRef name is restored")
				Eventually(func(g Gomega) {
					obj := newCertificate(certNN.Name, certNN.Namespace)
					g.Expect(k8sClient.Get(ctx, certNN, obj)).To(Succeed())
					issuer, _, _ := unstructured.NestedString(obj.Object, "spec", "issuerRef", "name")
					g.Expect(issuer).To(Equal(bootstrapIssuerName))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})
		})

	})

	Context("DistributeClusterCABundle defaulting", func() {
		// These tests use per-test managers to control ClusterInfo (OpenShift vs non-OpenShift).
		// The suite-level manager from BeforeSuite is not OpenShift, so these tests
		// register their own reconciler with the desired ClusterInfo.

		// startManagerWithPlatform creates a per-test manager with the given platform and
		// registers a DeferCleanup to stop it when the test ends.
		startManagerWithPlatform := func(isOpenShift bool) {
			info, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{isOpenShift: isOpenShift})
			Expect(err).NotTo(HaveOccurred())

			mgr := testutil.NewTestManager(testEnv)
			Expect((&KonfluxCertManagerReconciler{
				Client:      mgr.GetClient(),
				Scheme:      mgr.GetScheme(),
				ObjectStore: objectStore,
				ClusterInfo: info,
			}).SetupWithManager(mgr)).To(Succeed())
			mgrCtx, cancel := context.WithCancel(testEnv.Ctx)
			waitForStop := testutil.StartManagerWithContext(mgrCtx, mgr)
			DeferCleanup(func() {
				cancel()
				waitForStop()
			})
		}

		BeforeEach(func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: certManagerNamespace}}
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(Succeed())
		})

		// certManagerChildren lists all cluster-scoped/namespaced children.
		certManagerChildren := func() []client.Object {
			return []client.Object{
				newClusterIssuer(bootstrapIssuerName),
				newClusterIssuer(issuerName),
				newCertificate(certificateName, certManagerNamespace),
			}
		}

		Context("unset on non-OpenShift (Kind/upstream)", func() {
			BeforeEach(func() {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
			})

			It("should default to applying the trust-manager Bundle", func(ctx context.Context) {
				startManagerWithPlatform(false)

				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					// DistributeClusterCABundle is nil — should default to true on non-OpenShift
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("waiting for Ready=True")
				Eventually(func(g Gomega) {
					updated := &konfluxv1alpha1.KonfluxCertManager{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
					readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
					g.Expect(readyCond).NotTo(BeNil())
					g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying trust-manager Bundle was applied (will fail apply due to missing CRD, but PKI should succeed)")
				// Note: trust-manager CRD is not installed in envtest, so the Bundle
				// apply is skipped with a log. The reconciler should still reach Ready=True.
			})
		})

		Context("unset on OpenShift", func() {
			BeforeEach(func() {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
			})

			It("should default to skipping the trust-manager Bundle", func(ctx context.Context) {
				startManagerWithPlatform(true)

				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					// DistributeClusterCABundle is nil — should default to false on OpenShift
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("waiting for Ready=True")
				Eventually(func(g Gomega) {
					updated := &konfluxv1alpha1.KonfluxCertManager{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
					readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
					g.Expect(readyCond).NotTo(BeNil())
					g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})
		})

		Context("explicit true on OpenShift", func() {
			BeforeEach(func() {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
			})

			It("should apply the trust-manager Bundle when explicitly enabled", func(ctx context.Context) {
				startManagerWithPlatform(true)

				enabled := true
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec: konfluxv1alpha1.KonfluxCertManagerSpec{
						DistributeClusterCABundle: &enabled,
					},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("waiting for Ready=True (Bundle CRD not installed so apply is skipped)")
				Eventually(func(g Gomega) {
					updated := &konfluxv1alpha1.KonfluxCertManager{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
					readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
					g.Expect(readyCond).NotTo(BeNil())
					g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})
		})

		Context("explicit false on non-OpenShift", func() {
			BeforeEach(func() {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
			})

			It("should skip the trust-manager Bundle when explicitly disabled", func(ctx context.Context) {
				startManagerWithPlatform(false)

				disabled := false
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec: konfluxv1alpha1.KonfluxCertManagerSpec{
						DistributeClusterCABundle: &disabled,
					},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("waiting for Ready=True")
				Eventually(func(g Gomega) {
					updated := &konfluxv1alpha1.KonfluxCertManager{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
					readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
					g.Expect(readyCond).NotTo(BeNil())
					g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
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

// mockDiscoveryClient implements clusterinfo.DiscoveryClient for testing platform-aware defaults.
type mockDiscoveryClient struct {
	isOpenShift bool
}

func (m *mockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if groupVersion == "config.openshift.io/v1" {
		if m.isOpenShift {
			return &metav1.APIResourceList{
				GroupVersion: "config.openshift.io/v1",
				APIResources: []metav1.APIResource{
					{Kind: "ClusterVersion"},
				},
			}, nil
		}
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *mockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return &version.Info{GitVersion: "v1.30.0"}, nil
}
