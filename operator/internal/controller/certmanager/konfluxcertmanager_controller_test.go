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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/internalregistry"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
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

// newBundle returns an unstructured Bundle object suitable for k8sClient.Get calls.
func newBundle() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(bundleGVK)
	obj.SetName(trustedCABundleName)
	return obj
}

// seedRegistryOwnedTrustedCABundle creates the trusted-ca Bundle the way the
// InternalRegistry controller used to: embedded spec, registry ownership
// labels, and a controller ownerRef pointing at registry. registry must
// already exist so its UID can be used in the owner reference.
func seedRegistryOwnedTrustedCABundle(ctx context.Context, registry *konfluxv1alpha1.KonfluxInternalRegistry) {
	objects, err := objectStore.GetForComponent(manifests.CertManager)
	Expect(err).NotTo(HaveOccurred())

	var bundle client.Object
	for _, obj := range objects {
		if isTrustManagerResource(obj) {
			bundle = obj
			break
		}
	}
	Expect(bundle).NotTo(BeNil(), "cert-manager manifests should include a trust-manager Bundle")

	labels := bundle.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[constant.KonfluxOwnerLabel] = registry.GetName()
	labels[constant.KonfluxComponentLabel] = string(manifests.Registry)
	bundle.SetLabels(labels)

	Expect(controllerutil.SetControllerReference(registry, bundle, k8sClient.Scheme())).To(Succeed())
	Expect(k8sClient.Create(ctx, bundle)).To(Succeed())
}

func bundleCRDMissingErr() *meta.NoKindMatchError {
	return &meta.NoKindMatchError{
		GroupKind: schema.GroupKind{Group: trustManagerGroup, Kind: "Bundle"},
	}
}

// registryOwnedTrustedCABundle returns a Bundle with a leftover InternalRegistry
// controller ownerRef, for fake-client tests of releaseForeignBundleController.
func registryOwnedTrustedCABundle() *unstructured.Unstructured {
	objects, err := objectStore.GetForComponent(manifests.CertManager)
	Expect(err).NotTo(HaveOccurred())
	for _, obj := range objects {
		if !isTrustManagerResource(obj) {
			continue
		}
		u, ok := obj.(*unstructured.Unstructured)
		Expect(ok).To(BeTrue(), "trust-manager manifest should be unstructured")
		ctrl := true
		u.SetUID("bundle-uid")
		u.SetResourceVersion("1")
		u.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: "konflux.konflux-ci.dev/v1alpha1",
			Kind:       "KonfluxInternalRegistry",
			Name:       internalregistry.CRName,
			UID:        "registry-uid",
			Controller: &ctrl,
		}})
		return u
	}
	Fail("cert-manager manifests should include a trust-manager Bundle")
	return nil
}

var _ = Describe("KonfluxCertManager Controller", Ordered, func() {
	// "When the cert-manager namespace does not exist" runs first so the namespace
	// has never been created by another test's BeforeEach.
	Context("When the cert-manager namespace does not exist", func() {
		It("should fail apply and report error when createClusterIssuer is enabled", func() {
			startManager(createNonOpenShiftClusterInfo())

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

		// waitForBundleCondition blocks until ClusterCABundleDistributed is set
		// with the expected status and reason.
		waitForBundleCondition := func(g Gomega, expectedStatus metav1.ConditionStatus, expectedReason string) {
			updated := &konfluxv1alpha1.KonfluxCertManager{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
			bundleCond := meta.FindStatusCondition(updated.Status.Conditions, constant.ConditionTypeClusterCABundleDistributed)
			g.Expect(bundleCond).NotTo(BeNil(), "ClusterCABundleDistributed condition should be present")
			g.Expect(bundleCond.Status).To(Equal(expectedStatus))
			g.Expect(bundleCond.Reason).To(Equal(expectedReason))
		}

		// certManagerChildren lists all cluster-scoped/namespaced children that envtest's
		// missing GC won't cascade-delete when the parent CR is removed.
		certManagerChildren := func() []client.Object {
			return []client.Object{
				newClusterIssuer(bootstrapIssuerName),
				newClusterIssuer(issuerName),
				newCertificate(certificateName, certManagerNamespace),
				newBundle(),
			}
		}

		Context("with createClusterIssuer unset (defaults to enabled)", func() {
			It("should successfully reconcile the resource and create ClusterIssuers", func(ctx context.Context) {
				startManager(createNonOpenShiftClusterInfo())
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
				startManager(createNonOpenShiftClusterInfo())
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
				startManager(createNonOpenShiftClusterInfo())
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
				Expect(errors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
				err = k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, newClusterIssuer(issuerName))
				Expect(errors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
			})
		})

		Context("with createClusterIssuer disabled but default bundle on non-OpenShift", func() {
			It("should skip ClusterIssuers but still create the Bundle", func(ctx context.Context) {
				startManager(createNonOpenShiftClusterInfo())
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
				Expect(errors.IsNotFound(err)).To(BeTrue(), "bootstrap issuer should not exist")
				err = k8sClient.Get(ctx, types.NamespacedName{Name: issuerName}, newClusterIssuer(issuerName))
				Expect(errors.IsNotFound(err)).To(BeTrue(), "issuer should not exist")

				By("verifying the Bundle was created (default on non-OpenShift)")
				Eventually(func(g Gomega) {
					bundle := newBundle()
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, bundle)).To(Succeed())
					labels, _, _ := unstructured.NestedStringMap(bundle.Object, "metadata", "labels")
					g.Expect(labels).To(HaveKey(constant.KonfluxOwnerLabel))
					g.Expect(labels).To(HaveKey(constant.KonfluxComponentLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying BundleDistributed condition")
				Eventually(func(g Gomega) {
					waitForBundleCondition(g, metav1.ConditionTrue, condition.ReasonBundleDistributed)
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})
		})

		Context("ClusterCABundleDistributed condition", func() {
			It("should report BundleDistributed when distribution is enabled (default on non-OpenShift)", func(ctx context.Context) {
				startManager(createNonOpenShiftClusterInfo())
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				Eventually(func(g Gomega) {
					waitForBundleCondition(g, metav1.ConditionTrue, condition.ReasonBundleDistributed)
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the Bundle object was created with ownership labels")
				bundle := &unstructured.Unstructured{}
				bundle.SetGroupVersionKind(bundleGVK)
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, bundle)).To(Succeed())
				labels, _, _ := unstructured.NestedStringMap(bundle.Object, "metadata", "labels")
				Expect(labels).To(HaveKey(constant.KonfluxOwnerLabel))
				Expect(labels).To(HaveKey(constant.KonfluxComponentLabel))
			})

			It("should report BundleDistributionDisabled when explicitly disabled", func(ctx context.Context) {
				startManager(createNonOpenShiftClusterInfo())
				disabled := false
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec: konfluxv1alpha1.KonfluxCertManagerSpec{
						DistributeClusterCABundle: &disabled,
					},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				Eventually(func(g Gomega) {
					waitForBundleCondition(g, metav1.ConditionFalse, condition.ReasonBundleDistributionDisabled)
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the Bundle object was NOT created")
				bundle := &unstructured.Unstructured{}
				bundle.SetGroupVersionKind(bundleGVK)
				err := k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, bundle)
				Expect(errors.IsNotFound(err)).To(BeTrue(), "Bundle should not exist when distribution is disabled, got: %v", err)
			})

			It("should delete the Bundle when distribution is toggled off", func(ctx context.Context) {
				startManager(createNonOpenShiftClusterInfo())
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("waiting for the Bundle to be created")
				Eventually(func(g Gomega) {
					waitForBundleCondition(g, metav1.ConditionTrue, condition.ReasonBundleDistributed)
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, newBundle())).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("disabling cluster CA bundle distribution")
				Eventually(func(g Gomega) {
					updated := &konfluxv1alpha1.KonfluxCertManager{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
					disabled := false
					updated.Spec.DistributeClusterCABundle = &disabled
					g.Expect(k8sClient.Update(ctx, updated)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the Bundle is removed")
				Eventually(func(g Gomega) {
					waitForBundleCondition(g, metav1.ConditionFalse, condition.ReasonBundleDistributionDisabled)
					err := k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, newBundle())
					g.Expect(errors.IsNotFound(err)).To(BeTrue(), "Bundle should be deleted after toggling off, got: %v", err)
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})
		})

		Context("OpenShift platform defaults", func() {
			It("should default to BundleDistributionDisabled when distributeClusterCABundle is nil on OpenShift", func(ctx context.Context) {
				startManager(createOpenShiftClusterInfo())
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				Eventually(func(g Gomega) {
					waitForBundleCondition(g, metav1.ConditionFalse, condition.ReasonBundleDistributionDisabled)
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the Bundle object was NOT created")
				bundle := newBundle()
				err := k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, bundle)
				Expect(errors.IsNotFound(err)).To(BeTrue(), "Bundle should not exist on OpenShift with nil field, got: %v", err)
			})

			It("should create Bundle when distributeClusterCABundle is explicitly true on OpenShift", func(ctx context.Context) {
				startManager(createOpenShiftClusterInfo())
				enabled := true
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec: konfluxv1alpha1.KonfluxCertManagerSpec{
						DistributeClusterCABundle: &enabled,
					},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				Eventually(func(g Gomega) {
					waitForBundleCondition(g, metav1.ConditionTrue, condition.ReasonBundleDistributed)
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the Bundle object was created with ownership labels")
				bundle := newBundle()
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, bundle)).To(Succeed())
				labels, _, _ := unstructured.NestedStringMap(bundle.Object, "metadata", "labels")
				Expect(labels).To(HaveKey(constant.KonfluxOwnerLabel))
				Expect(labels).To(HaveKey(constant.KonfluxComponentLabel))
			})

			It("should report BundleDistributionDisabled when explicitly false on OpenShift", func(ctx context.Context) {
				startManager(createOpenShiftClusterInfo())
				disabled := false
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec: konfluxv1alpha1.KonfluxCertManagerSpec{
						DistributeClusterCABundle: &disabled,
					},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				Eventually(func(g Gomega) {
					waitForBundleCondition(g, metav1.ConditionFalse, condition.ReasonBundleDistributionDisabled)
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the Bundle object was NOT created")
				bundle := newBundle()
				err := k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, bundle)
				Expect(errors.IsNotFound(err)).To(BeTrue(), "Bundle should not exist when explicitly disabled on OpenShift, got: %v", err)
			})
		})

		Context("upgrade from InternalRegistry ownership", func() {
			It("should take over a Bundle previously owned by KonfluxInternalRegistry", func(ctx context.Context) {
				By("creating the InternalRegistry CR that previously owned the Bundle")
				registry := &konfluxv1alpha1.KonfluxInternalRegistry{
					ObjectMeta: metav1.ObjectMeta{Name: internalregistry.CRName},
				}
				Expect(k8sClient.Create(ctx, registry)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, registry)
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName}, registry)).To(Succeed())

				By("seeding trusted-ca with a controller ownerRef to InternalRegistry")
				seedRegistryOwnedTrustedCABundle(ctx, registry)

				startManager(createNonOpenShiftClusterInfo())
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("verifying CertManager becomes the sole controller owner")
				Eventually(func(g Gomega) {
					updated := &konfluxv1alpha1.KonfluxCertManager{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
					readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
					g.Expect(readyCond).NotTo(BeNil())
					g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue),
						"expected Ready=True after Bundle takeover, got status=%s reason=%s message=%s",
						readyCond.Status, readyCond.Reason, readyCond.Message)
					waitForBundleCondition(g, metav1.ConditionTrue, condition.ReasonBundleDistributed)

					obj := newBundle()
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, obj)).To(Succeed())

					var certManagerControllers, registryControllers int
					for _, ref := range obj.GetOwnerReferences() {
						if ref.Controller == nil || !*ref.Controller {
							continue
						}
						switch ref.Kind {
						case "KonfluxCertManager":
							if ref.Name == CRName {
								certManagerControllers++
							}
						case "KonfluxInternalRegistry":
							registryControllers++
						}
					}
					g.Expect(certManagerControllers).To(Equal(1), "Bundle should have KonfluxCertManager as the controller owner")
					g.Expect(registryControllers).To(Equal(0), "Bundle should not retain KonfluxInternalRegistry as a controller owner")

					labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
					g.Expect(labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
					g.Expect(labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.CertManager)))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})

			It("should delete a leftover InternalRegistry-owned Bundle when distribution defaults off on OpenShift", func(ctx context.Context) {
				By("creating the InternalRegistry CR that previously owned the Bundle")
				registry := &konfluxv1alpha1.KonfluxInternalRegistry{
					ObjectMeta: metav1.ObjectMeta{Name: internalregistry.CRName},
				}
				Expect(k8sClient.Create(ctx, registry)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, registry)
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName}, registry)).To(Succeed())

				By("seeding trusted-ca with a controller ownerRef to InternalRegistry")
				seedRegistryOwnedTrustedCABundle(ctx, registry)

				startManager(createOpenShiftClusterInfo())
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("verifying the leftover Bundle is deleted")
				Eventually(func(g Gomega) {
					waitForBundleCondition(g, metav1.ConditionFalse, condition.ReasonBundleDistributionDisabled)
					err := k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, newBundle())
					g.Expect(errors.IsNotFound(err)).To(BeTrue(), "leftover Bundle should be deleted when distribution defaults off, got: %v", err)
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})

			It("should delete a leftover InternalRegistry-owned Bundle when distribution is explicitly disabled", func(ctx context.Context) {
				By("creating the InternalRegistry CR that previously owned the Bundle")
				registry := &konfluxv1alpha1.KonfluxInternalRegistry{
					ObjectMeta: metav1.ObjectMeta{Name: internalregistry.CRName},
				}
				Expect(k8sClient.Create(ctx, registry)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, registry)
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName}, registry)).To(Succeed())

				By("seeding trusted-ca with a controller ownerRef to InternalRegistry")
				seedRegistryOwnedTrustedCABundle(ctx, registry)

				startManager(createNonOpenShiftClusterInfo())
				disabled := false
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec:       konfluxv1alpha1.KonfluxCertManagerSpec{DistributeClusterCABundle: &disabled},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("verifying the leftover Bundle is deleted")
				Eventually(func(g Gomega) {
					waitForBundleCondition(g, metav1.ConditionFalse, condition.ReasonBundleDistributionDisabled)
					err := k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, newBundle())
					g.Expect(errors.IsNotFound(err)).To(BeTrue(), "leftover Bundle should be deleted when distribution is disabled, got: %v", err)
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})
		})

		Context("Self-healing", func() {
			It("recreates ClusterIssuer when deleted", func(ctx context.Context) {
				startManager(createNonOpenShiftClusterInfo())
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
					g.Expect(labels).To(HaveKey(constant.KonfluxComponentLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})

			It("recreates Certificate when deleted", func(ctx context.Context) {
				startManager(createNonOpenShiftClusterInfo())
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
					g.Expect(labels).To(HaveKey(constant.KonfluxComponentLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})

			It("recreates Bundle when deleted", func(ctx context.Context) {
				startManager(createNonOpenShiftClusterInfo())
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				By("waiting for initial Bundle creation")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, newBundle())).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("deleting the Bundle")
				Expect(k8sClient.Delete(ctx, newBundle())).To(Succeed())

				By("verifying the Bundle is recreated with ownership labels")
				Eventually(func(g Gomega) {
					obj := newBundle()
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, obj)).To(Succeed())
					labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
					g.Expect(labels).To(HaveKey(constant.KonfluxOwnerLabel))
					g.Expect(labels).To(HaveKey(constant.KonfluxComponentLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			})

		})

		Context("Drift correction", func() {
			It("restores ClusterIssuer labels when stripped", func(ctx context.Context) {
				startManager(createNonOpenShiftClusterInfo())
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
					g.Expect(labels).To(HaveKey(constant.KonfluxComponentLabel))
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
				startManager(createNonOpenShiftClusterInfo())
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
					g.Expect(labels).To(HaveKey(constant.KonfluxComponentLabel))
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
				startManager(createNonOpenShiftClusterInfo())
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
				startManager(createNonOpenShiftClusterInfo())
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
				startManager(createNonOpenShiftClusterInfo())
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

			It("restores Bundle spec when modified", func(ctx context.Context) {
				startManager(createNonOpenShiftClusterInfo())
				cm := &konfluxv1alpha1.KonfluxCertManager{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cm)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, cm, certManagerChildren()...)

				const originalKey = "ca-bundle.crt"

				By("waiting for initial Bundle creation")
				Eventually(func(g Gomega) {
					obj := newBundle()
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, obj)).To(Succeed())
					key, _, _ := unstructured.NestedString(obj.Object, "spec", "target", "configMap", "key")
					g.Expect(key).To(Equal(originalKey))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("modifying the Bundle target ConfigMap key")
				Eventually(func(g Gomega) {
					obj := newBundle()
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, obj)).To(Succeed())
					g.Expect(unstructured.SetNestedField(obj.Object, "tampered.crt", "spec", "target", "configMap", "key")).To(Succeed())
					g.Expect(k8sClient.Update(ctx, obj)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the Bundle target ConfigMap key is restored")
				Eventually(func(g Gomega) {
					obj := newBundle()
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, obj)).To(Succeed())
					key, _, _ := unstructured.NestedString(obj.Object, "spec", "target", "configMap", "key")
					g.Expect(key).To(Equal(originalKey))
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

	Context("applyTrustBundle unit tests", func() {
		var (
			owner      *konfluxv1alpha1.KonfluxCertManager
			reconciler *KonfluxCertManagerReconciler
		)

		// newTrackingClient creates a tracking.Client wrapping the given fake client
		// with the standard ownership config for certmanager tests.
		newTrackingClient := func(fakeClient client.Client) *tracking.Client {
			return tracking.NewClientWithOwnership(fakeClient, tracking.OwnershipConfig{
				Owner:             owner,
				OwnerLabelKey:     constant.KonfluxOwnerLabel,
				ComponentLabelKey: constant.KonfluxComponentLabel,
				Component:         string(manifests.CertManager),
				FieldManager:      FieldManager,
			})
		}

		BeforeEach(func() {
			owner = &konfluxv1alpha1.KonfluxCertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
					UID:  "test-uid",
				},
			}
			reconciler = &KonfluxCertManagerReconciler{
				Scheme:      scheme.Scheme,
				ObjectStore: objectStore,
			}
		})

		It("should return BundleNotDistributed condition and requeue when trust-manager CRD is missing", func() {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Patch: func(
						_ context.Context, _ client.WithWatch, obj client.Object, _ client.Patch, _ ...client.PatchOption,
					) error {
						if obj.GetObjectKind().GroupVersionKind().Group == trustManagerGroup {
							return &meta.NoKindMatchError{
								GroupKind: schema.GroupKind{Group: trustManagerGroup, Kind: "Bundle"},
							}
						}
						return fmt.Errorf("unexpected patch on %s", obj.GetObjectKind().GroupVersionKind())
					},
				}).
				Build()

			cond, requeueAfter, err := reconciler.applyTrustBundle(ctx, newTrackingClient(fakeClient), true)

			Expect(err).NotTo(HaveOccurred())
			Expect(requeueAfter).To(Equal(30 * time.Second))
			Expect(cond.Type).To(Equal(constant.ConditionTypeClusterCABundleDistributed))
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(condition.ReasonBundleNotDistributed))
			Expect(cond.Message).To(ContainSubstring("trust-manager CRD"))
		})

		It("should return error when trust-manager Bundle apply fails with a non-NoKindMatch error", func() {
			applyErr := fmt.Errorf("connection refused")
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Patch: func(
						_ context.Context, _ client.WithWatch, obj client.Object, _ client.Patch, _ ...client.PatchOption,
					) error {
						if obj.GetObjectKind().GroupVersionKind().Group == trustManagerGroup {
							return applyErr
						}
						return fmt.Errorf("unexpected patch on %s", obj.GetObjectKind().GroupVersionKind())
					},
				}).
				Build()

			_, _, err := reconciler.applyTrustBundle(ctx, newTrackingClient(fakeClient), true)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("connection refused"))
			Expect(err.Error()).To(ContainSubstring("failed to apply object"))
		})

		It("should return BundleDistributionDisabled condition when distributeBundle is false", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()

			cond, requeueAfter, err := reconciler.applyTrustBundle(ctx, newTrackingClient(fakeClient), false)

			Expect(err).NotTo(HaveOccurred())
			Expect(requeueAfter).To(BeZero())
			Expect(cond.Type).To(Equal(constant.ConditionTypeClusterCABundleDistributed))
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(condition.ReasonBundleDistributionDisabled))
		})

		It("should delete a leftover Bundle when distributeBundle is false", func() {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(registryOwnedTrustedCABundle()).
				Build()

			cond, requeueAfter, err := reconciler.applyTrustBundle(ctx, newTrackingClient(fakeClient), false)

			Expect(err).NotTo(HaveOccurred())
			Expect(requeueAfter).To(BeZero())
			Expect(cond.Reason).To(Equal(condition.ReasonBundleDistributionDisabled))

			err = fakeClient.Get(ctx, types.NamespacedName{Name: trustedCABundleName}, newBundle())
			Expect(errors.IsNotFound(err)).To(BeTrue(), "leftover Bundle should be deleted, got: %v", err)
		})

		It("should ignore a missing trust-manager CRD when deleting a leftover Bundle", func() {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Delete: func(
						_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption,
					) error {
						if isTrustManagerResource(obj) {
							return bundleCRDMissingErr()
						}
						return fmt.Errorf("unexpected delete on %s", obj.GetObjectKind().GroupVersionKind())
					},
				}).
				Build()

			cond, requeueAfter, err := reconciler.applyTrustBundle(ctx, newTrackingClient(fakeClient), false)

			Expect(err).NotTo(HaveOccurred())
			Expect(requeueAfter).To(BeZero())
			Expect(cond.Reason).To(Equal(condition.ReasonBundleDistributionDisabled))
		})

		It("should return error when deleting a leftover Bundle fails", func() {
			deleteErr := fmt.Errorf("delete denied")
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Delete: func(
						_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption,
					) error {
						if isTrustManagerResource(obj) {
							return deleteErr
						}
						return fmt.Errorf("unexpected delete on %s", obj.GetObjectKind().GroupVersionKind())
					},
				}).
				Build()

			_, requeueAfter, err := reconciler.applyTrustBundle(ctx, newTrackingClient(fakeClient), false)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to delete trust-manager Bundle"))
			Expect(err.Error()).To(ContainSubstring("delete denied"))
			Expect(requeueAfter).To(BeZero())
		})

		It("should return BundleNotDistributed when Get hits a missing trust-manager CRD", func() {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(
						ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, _ ...client.GetOption,
					) error {
						if isTrustManagerResource(obj) {
							return bundleCRDMissingErr()
						}
						return c.Get(ctx, key, obj)
					},
				}).
				Build()

			cond, requeueAfter, err := reconciler.applyTrustBundle(ctx, newTrackingClient(fakeClient), true)

			Expect(err).NotTo(HaveOccurred())
			Expect(requeueAfter).To(Equal(trustManagerCRDRetryInterval))
			Expect(cond.Type).To(Equal(constant.ConditionTypeClusterCABundleDistributed))
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(condition.ReasonBundleNotDistributed))
			Expect(cond.Message).To(ContainSubstring("trust-manager CRD"))
		})

		It("should return error when Get fails while releasing a foreign Bundle ownerRef", func() {
			getErr := fmt.Errorf("get failed")
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(
						ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, _ ...client.GetOption,
					) error {
						if isTrustManagerResource(obj) {
							return getErr
						}
						return c.Get(ctx, key, obj)
					},
				}).
				Build()

			_, requeueAfter, err := reconciler.applyTrustBundle(ctx, newTrackingClient(fakeClient), true)

			Expect(err).To(MatchError(getErr))
			Expect(requeueAfter).To(BeZero())
		})

		It("should return error when Patch fails while releasing a foreign Bundle ownerRef", func() {
			patchErr := fmt.Errorf("patch denied")
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(registryOwnedTrustedCABundle()).
				WithInterceptorFuncs(interceptor.Funcs{
					Patch: func(
						_ context.Context, _ client.WithWatch, obj client.Object, _ client.Patch, _ ...client.PatchOption,
					) error {
						if isTrustManagerResource(obj) {
							return patchErr
						}
						return fmt.Errorf("unexpected patch on %s", obj.GetObjectKind().GroupVersionKind())
					},
				}).
				Build()

			_, requeueAfter, err := reconciler.applyTrustBundle(ctx, newTrackingClient(fakeClient), true)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to release foreign controller ownerRef"))
			Expect(err.Error()).To(ContainSubstring("patch denied"))
			Expect(requeueAfter).To(BeZero())
		})

		It("should return error when CertManager manifests are missing", func() {
			reconciler.ObjectStore = &manifests.ObjectStore{}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
			tc := newTrackingClient(fakeClient)

			_, requeueAfter, err := reconciler.applyTrustBundle(ctx, tc, true)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get parsed manifests for CertManager"))
			Expect(requeueAfter).To(BeZero())

			err = reconciler.applyPKIManifests(ctx, tc)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get parsed manifests for CertManager"))
		})
	})

	Context("Reconcile unit tests", func() {
		It("should requeue when the trust-manager CRD is missing", func() {
			s := runtime.NewScheme()
			Expect(konfluxv1alpha1.AddToScheme(s)).To(Succeed())
			Expect(appsv1.AddToScheme(s)).To(Succeed())
			Expect(corev1.AddToScheme(s)).To(Succeed())

			disabled := false
			cm := &konfluxv1alpha1.KonfluxCertManager{
				ObjectMeta: metav1.ObjectMeta{
					Name:       CRName,
					UID:        "test-uid",
					Generation: 1,
				},
				Spec: konfluxv1alpha1.KonfluxCertManagerSpec{CreateClusterIssuer: &disabled},
			}

			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithStatusSubresource(cm).
				WithObjects(cm).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(
						ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, _ ...client.GetOption,
					) error {
						if isTrustManagerResource(obj) {
							return bundleCRDMissingErr()
						}
						return c.Get(ctx, key, obj)
					},
					List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
						return nil
					},
				}).
				Build()

			r := &KonfluxCertManagerReconciler{
				Client:      cl,
				Scheme:      s,
				ObjectStore: objectStore,
			}

			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: CRName}})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(trustManagerCRDRetryInterval))

			updated := &konfluxv1alpha1.KonfluxCertManager{}
			Expect(cl.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
			bundleCond := meta.FindStatusCondition(updated.Status.Conditions, constant.ConditionTypeClusterCABundleDistributed)
			Expect(bundleCond).NotTo(BeNil())
			Expect(bundleCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(bundleCond.Reason).To(Equal(condition.ReasonBundleNotDistributed))
		})
	})
})
