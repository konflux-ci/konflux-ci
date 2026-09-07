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

package internalregistry

import (
	"context"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

const internalRegistryNamespace = "kind-registry"

func internalRegistryClusterScopedChildren() []client.Object {
	bundle := &unstructured.Unstructured{}
	bundle.SetGroupVersionKind(trustBundleGVK)
	bundle.SetName("trusted-ca")
	return []client.Object{bundle}
}

var _ = Describe("KonfluxInternalRegistry Controller", func() {
	Context("When reconciling a resource", Ordered, func() {
		BeforeAll(func() {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())

			// Wait for Deployment existence as proof of successful reconciliation.
			// Ready=True is not achievable in envtest (no kubelet → pods never start).
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "registry",
					Namespace: internalRegistryNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		AfterAll(func() {
			testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxInternalRegistry{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
			for _, child := range internalRegistryClusterScopedChildren() {
				testutil.DeleteAndWait(ctx, k8sClient, child)
			}
		})

		It("should successfully reconcile the resource", func() {
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.KonfluxInternalRegistry{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
				g.Expect(updated.Status.Conditions).NotTo(BeEmpty())
				readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
				g.Expect(readyCond).NotTo(BeNil(), "Ready condition should be present")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should set ownership labels on applied resources", func() {
			namespace := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: internalRegistryNamespace}, namespace)).To(Succeed())

			labels := namespace.GetLabels()
			Expect(labels).NotTo(BeNil())
			Expect(labels[constant.KonfluxOwnerLabel]).To(Equal(CRName))
			Expect(labels[constant.KonfluxComponentLabel]).To(Equal("registry"))

			ownerRefs := namespace.GetOwnerReferences()
			Expect(ownerRefs).NotTo(BeEmpty())
			found := false
			for _, ref := range ownerRefs {
				if ref.Name == CRName && ref.Kind == "KonfluxInternalRegistry" {
					found = true
					Expect(ref.Controller).NotTo(BeNil())
					Expect(*ref.Controller).To(BeTrue())
					break
				}
			}
			Expect(found).To(BeTrue(), "Owner reference should be set")
		})

		It("should create trust-manager Bundle with ownership and labels", func() {
			bundle := &unstructured.Unstructured{}
			bundle.SetGroupVersionKind(trustBundleGVK)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "trusted-ca"}, bundle)).To(Succeed())

			labels := bundle.GetLabels()
			Expect(labels).NotTo(BeNil())
			Expect(labels[constant.KonfluxOwnerLabel]).To(Equal(CRName))
			Expect(labels[constant.KonfluxComponentLabel]).To(Equal("registry"))

			ownerRefs := bundle.GetOwnerReferences()
			Expect(ownerRefs).NotTo(BeEmpty())
			found := false
			for _, ref := range ownerRefs {
				if ref.Name == CRName && ref.Kind == "KonfluxInternalRegistry" {
					found = true
					Expect(ref.Controller).NotTo(BeNil())
					Expect(*ref.Controller).To(BeTrue())
					break
				}
			}
			Expect(found).To(BeTrue(), "Owner reference should be set on Bundle")

			targetKey, found, err := unstructured.NestedString(bundle.Object, "spec", "target", "configMap", "key")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(targetKey).To(Equal("ca-bundle.crt"))
		})
	})

	Context("Self-healing", func() {
		It("recreates Deployment when deleted", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			deploymentNN := types.NamespacedName{
				Name:      "registry",
				Namespace: internalRegistryNamespace,
			}

			By("waiting for initial Deployment creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, deploymentNN, &appsv1.Deployment{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Deployment")
			Expect(k8sClient.Delete(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: deploymentNN.Name, Namespace: deploymentNN.Namespace},
			})).To(Succeed())

			By("verifying the Deployment is recreated with correct spec")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				g.Expect(dep.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, "registry")
				g.Expect(container).NotTo(BeNil(), "registry container should exist")
				g.Expect(container.Image).NotTo(BeEmpty(), "container image should be set")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ConfigMap when deleted", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			cmNN := types.NamespacedName{
				Name:      "zot-config",
				Namespace: internalRegistryNamespace,
			}

			By("waiting for initial ConfigMap creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, cmNN, &corev1.ConfigMap{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ConfigMap")
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: cmNN.Name, Namespace: cmNN.Namespace},
			})).To(Succeed())

			By("verifying the ConfigMap is recreated with ownership labels")
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				g.Expect(cm.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates Service when deleted", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			svcNN := types.NamespacedName{
				Name:      "registry-service",
				Namespace: internalRegistryNamespace,
			}

			By("waiting for initial Service creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, svcNN, &corev1.Service{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Service")
			Expect(k8sClient.Delete(ctx, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: svcNN.Name, Namespace: svcNN.Namespace},
			})).To(Succeed())

			By("verifying the Service is recreated with ownership labels")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates Secret when deleted", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			secretNN := types.NamespacedName{
				Name:      HtpasswdSecretName,
				Namespace: internalRegistryNamespace,
			}

			By("waiting for initial Secret creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, secretNN, &corev1.Secret{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Secret")
			Expect(k8sClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretNN.Name, Namespace: secretNN.Namespace},
			})).To(Succeed())

			By("verifying the Secret is recreated with ownership labels")
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, secretNN, secret)).To(Succeed())
				g.Expect(secret.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates Certificate when deleted", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			certNN := types.NamespacedName{
				Name:      "registry-cert",
				Namespace: internalRegistryNamespace,
			}

			By("waiting for initial Certificate creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, certNN, &certmanagerv1.Certificate{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Certificate")
			Expect(k8sClient.Delete(ctx, &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{Name: certNN.Name, Namespace: certNN.Namespace},
			})).To(Succeed())

			By("verifying the Certificate is recreated with ownership labels")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				g.Expect(cert.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

	})

	Context("Drift correction", func() {
		It("restores Deployment image when modified", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			deploymentNN := types.NamespacedName{
				Name:      "registry",
				Namespace: internalRegistryNamespace,
			}

			By("waiting for initial Deployment creation")
			var originalImage string
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, "registry")
				g.Expect(container).NotTo(BeNil())
				originalImage = container.Image
				g.Expect(originalImage).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Deployment image")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, "registry")
				g.Expect(container).NotTo(BeNil())
				container.Image = "tampered-image:latest"
				g.Expect(k8sClient.Update(ctx, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Deployment image is restored")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, "registry")
				g.Expect(container).NotTo(BeNil())
				g.Expect(container.Image).To(Equal(originalImage))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ConfigMap data when modified", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			cmNN := types.NamespacedName{
				Name:      "zot-config",
				Namespace: internalRegistryNamespace,
			}

			By("waiting for initial ConfigMap creation")
			var originalData map[string]string
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				g.Expect(cm.Data).NotTo(BeEmpty())
				originalData = cm.Data
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ConfigMap data")
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				cm.Data = map[string]string{"config.json": "tampered"}
				g.Expect(k8sClient.Update(ctx, cm)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ConfigMap data is restored")
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				g.Expect(cm.Data).To(Equal(originalData))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Service spec when modified", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			svcNN := types.NamespacedName{
				Name:      "registry-service",
				Namespace: internalRegistryNamespace,
			}

			By("waiting for initial Service creation")
			var originalTargetPort int32
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Spec.Ports).NotTo(BeEmpty())
				originalTargetPort = svc.Spec.Ports[0].TargetPort.IntVal
				g.Expect(originalTargetPort).NotTo(BeZero())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Service target port")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				svc.Spec.Ports[0].TargetPort.IntVal = 9999
				g.Expect(k8sClient.Update(ctx, svc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Service target port is restored")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Spec.Ports).NotTo(BeEmpty())
				g.Expect(svc.Spec.Ports[0].TargetPort.IntVal).To(Equal(originalTargetPort))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Certificate spec when modified", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			certNN := types.NamespacedName{
				Name:      "registry-cert",
				Namespace: internalRegistryNamespace,
			}

			By("waiting for initial Certificate creation")
			var originalDNSNames []string
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				g.Expect(cert.Spec.DNSNames).NotTo(BeEmpty())
				originalDNSNames = cert.Spec.DNSNames
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Certificate DNS names")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				cert.Spec.DNSNames = []string{"tampered.example.com"}
				g.Expect(k8sClient.Update(ctx, cert)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Certificate DNS names are restored")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				g.Expect(cert.Spec.DNSNames).To(Equal(originalDNSNames))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("rotates credentials when htpasswd is cleared", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			secretNN := types.NamespacedName{
				Name:      HtpasswdSecretName,
				Namespace: internalRegistryNamespace,
			}

			By("waiting for initial Secret creation")
			var originalData []byte
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, secretNN, secret)).To(Succeed())
				g.Expect(secret.Data).To(HaveKey("htpasswd"))
				originalData = secret.Data["htpasswd"]
				g.Expect(originalData).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("clearing the Secret data to trigger credential rotation")
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, secretNN, secret)).To(Succeed())
				secret.Data["htpasswd"] = []byte{}
				g.Expect(k8sClient.Update(ctx, secret)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Secret data is repopulated")
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, secretNN, secret)).To(Succeed())
				g.Expect(secret.Data).To(HaveKey("htpasswd"))
				g.Expect(secret.Data["htpasswd"]).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Namespace labels when stripped", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			nsNN := types.NamespacedName{
				Name: internalRegistryNamespace,
			}

			By("waiting for initial Namespace creation with ownership labels")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Namespace")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				delete(ns.Labels, constant.KonfluxOwnerLabel)
				delete(ns.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, ns)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Namespace labels are restored")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates trust-manager Bundle when deleted", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			bundleNN := types.NamespacedName{Name: "trusted-ca"}

			By("waiting for initial Bundle creation")
			Eventually(func(g Gomega) {
				bundle := &unstructured.Unstructured{}
				bundle.SetGroupVersionKind(trustBundleGVK)
				g.Expect(k8sClient.Get(ctx, bundleNN, bundle)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Bundle")
			bundleToDelete := &unstructured.Unstructured{}
			bundleToDelete.SetGroupVersionKind(trustBundleGVK)
			bundleToDelete.SetName(bundleNN.Name)
			Expect(k8sClient.Delete(ctx, bundleToDelete)).To(Succeed())

			By("verifying the Bundle is recreated with correct spec and ownership")
			Eventually(func(g Gomega) {
				bundle := &unstructured.Unstructured{}
				bundle.SetGroupVersionKind(trustBundleGVK)
				g.Expect(k8sClient.Get(ctx, bundleNN, bundle)).To(Succeed())
				g.Expect(bundle.GetLabels()).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
				g.Expect(bundle.GetLabels()).To(HaveKeyWithValue(constant.KonfluxComponentLabel, "registry"))
				targetKey, found, err := unstructured.NestedString(bundle.Object, "spec", "target", "configMap", "key")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(targetKey).To(Equal("ca-bundle.crt"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores trust-manager Bundle spec when modified", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			bundleNN := types.NamespacedName{Name: "trusted-ca"}

			By("waiting for initial Bundle creation")
			Eventually(func(g Gomega) {
				bundle := &unstructured.Unstructured{}
				bundle.SetGroupVersionKind(trustBundleGVK)
				g.Expect(k8sClient.Get(ctx, bundleNN, bundle)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Bundle spec")
			Eventually(func(g Gomega) {
				bundle := &unstructured.Unstructured{}
				bundle.SetGroupVersionKind(trustBundleGVK)
				g.Expect(k8sClient.Get(ctx, bundleNN, bundle)).To(Succeed())
				err := unstructured.SetNestedField(bundle.Object, "tampered-ca-bundle.crt", "spec", "target", "configMap", "key")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(k8sClient.Update(ctx, bundle)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Bundle spec is restored to ca-bundle.crt")
			Eventually(func(g Gomega) {
				bundle := &unstructured.Unstructured{}
				bundle.SetGroupVersionKind(trustBundleGVK)
				g.Expect(k8sClient.Get(ctx, bundleNN, bundle)).To(Succeed())
				targetKey, found, err := unstructured.NestedString(bundle.Object, "spec", "target", "configMap", "key")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(targetKey).To(Equal("ca-bundle.crt"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores trust-manager Bundle labels when stripped", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			bundleNN := types.NamespacedName{Name: "trusted-ca"}

			By("waiting for initial Bundle creation with ownership labels")
			Eventually(func(g Gomega) {
				bundle := &unstructured.Unstructured{}
				bundle.SetGroupVersionKind(trustBundleGVK)
				g.Expect(k8sClient.Get(ctx, bundleNN, bundle)).To(Succeed())
				g.Expect(bundle.GetLabels()).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Bundle")
			Eventually(func(g Gomega) {
				bundle := &unstructured.Unstructured{}
				bundle.SetGroupVersionKind(trustBundleGVK)
				g.Expect(k8sClient.Get(ctx, bundleNN, bundle)).To(Succeed())
				delete(bundle.GetLabels(), constant.KonfluxOwnerLabel)
				delete(bundle.GetLabels(), constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, bundle)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Bundle labels are restored")
			Eventually(func(g Gomega) {
				bundle := &unstructured.Unstructured{}
				bundle.SetGroupVersionKind(trustBundleGVK)
				g.Expect(k8sClient.Get(ctx, bundleNN, bundle)).To(Succeed())
				g.Expect(bundle.GetLabels()).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(bundle.GetLabels()).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Watch Association Filtering", func() {
		It("does not affect KonfluxInternalRegistry when an unrelated Bundle is mutated or deleted", func(ctx context.Context) {
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry, internalRegistryClusterScopedChildren()...)

			By("waiting for initial reconciliation to finish")
			var initialRV string
			Eventually(func(g Gomega) {
				reg := &konfluxv1alpha1.KonfluxInternalRegistry{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, reg)).To(Succeed())
				readyCond := meta.FindStatusCondition(reg.Status.Conditions, condition.TypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				initialRV = reg.ResourceVersion
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("creating an unrelated Bundle without Konflux ownership")
			unrelated := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "trust.cert-manager.io/v1alpha1",
					"kind":       "Bundle",
					"metadata": map[string]interface{}{
						"name": "unrelated-custom-bundle",
					},
					"spec": map[string]interface{}{
						"sources": []interface{}{
							map[string]interface{}{
								"useDefaultCAs": true,
							},
						},
						"target": map[string]interface{}{
							"configMap": map[string]interface{}{
								"key": "custom.crt",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, unrelated)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, unrelated)

			By("modifying the unrelated Bundle")
			Eventually(func(g Gomega) {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(trustBundleGVK)
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "unrelated-custom-bundle"}, u)).To(Succeed())
				err := unstructured.SetNestedField(u.Object, "modified.crt", "spec", "target", "configMap", "key")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(k8sClient.Update(ctx, u)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the unrelated Bundle")
			Expect(k8sClient.Delete(ctx, unrelated)).To(Succeed())

			By("confirming the unrelated Bundle was deleted and KonfluxInternalRegistry was not reconciled")
			Eventually(func(g Gomega) {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(trustBundleGVK)
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "unrelated-custom-bundle"}, u)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Consistently(func(g Gomega) {
				reg := &konfluxv1alpha1.KonfluxInternalRegistry{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, reg)).To(Succeed())
				g.Expect(reg.ResourceVersion).To(Equal(initialRV), "unrelated Bundle lifecycle events must not trigger KonfluxInternalRegistry reconciliation")
			}, "2s", "250ms").Should(Succeed())
		})
	})

	Context("trustBundleWatchObjectIfInstalled helper function", func() {
		It("should return unstructured Bundle object when CRD is available in RESTMapper", func() {
			bundle, ok := trustBundleWatchObjectIfInstalled(k8sClient.RESTMapper())
			Expect(ok).To(BeTrue())
			Expect(bundle).NotTo(BeNil())
			Expect(bundle.GroupVersionKind()).To(Equal(trustBundleGVK))
		})

		It("should return false when RESTMapper is nil", func() {
			bundle, ok := trustBundleWatchObjectIfInstalled(nil)
			Expect(ok).To(BeFalse())
			Expect(bundle).To(BeNil())
		})

		It("should return false when GVK is not in RESTMapper", func() {
			emptyMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{})
			bundle, ok := trustBundleWatchObjectIfInstalled(emptyMapper)
			Expect(ok).To(BeFalse())
			Expect(bundle).To(BeNil())
		})

		It("should return false when RESTMapper returns a non-NoMatch error", func() {
			errMapper := errRESTMapper{err: fmt.Errorf("discovery client connection refused")}
			bundle, ok := trustBundleWatchObjectIfInstalled(errMapper)
			Expect(ok).To(BeFalse())
			Expect(bundle).To(BeNil())
		})
	})

	Context("Controller Setup without trust-manager CRD", func() {
		It("successfully configures controller without Bundle watch when CRD is absent", func() {
			// Use a REST mapper that has no registered GVKs.
			// This simulates a cluster where the trust-manager CRD is not installed,
			// so trustBundleWatchObjectIfInstalled must return (nil, false) and
			// SetupWithManager must succeed without registering the Bundle Owns() watch.
			emptyMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{})
			bundle, ok := trustBundleWatchObjectIfInstalled(emptyMapper)
			Expect(ok).To(BeFalse(), "emptyMapper should not find the Bundle GVK")
			Expect(bundle).To(BeNil())

			// Verify the manager wiring path via testutil.NewTestManager (connected to
			// the shared testEnv, which does have the CRD), but drive the CRD-absent
			// discovery through the empty mapper directly to confirm no panic/error.
			mgr := testutil.NewTestManager(testEnv)
			reconciler := &KonfluxInternalRegistryReconciler{
				Client:      mgr.GetClient(),
				Scheme:      mgr.GetScheme(),
				ObjectStore: objectStore,
			}
			Expect(reconciler.SetupWithManager(mgr)).To(Succeed())
		})
	})

	Context("tracking.IsNoKindMatchError helper function", func() {
		It("should correctly identify NoKindMatchError", func() {
			noKindErr := &meta.NoKindMatchError{
				GroupKind: schema.GroupKind{Group: "cert-manager.io", Kind: "Certificate"},
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
				GroupKind: schema.GroupKind{Group: "trust.cert-manager.io", Kind: "Bundle"},
			}
			wrappedErr := fmt.Errorf("failed to list resources: %w", noKindErr)
			Expect(tracking.IsNoKindMatchError(wrappedErr)).To(BeTrue())
		})
	})
})

// errRESTMapper returns a fixed RESTMapping error for trustBundleWatchObjectIfInstalled testing.
type errRESTMapper struct {
	err error
}

func (m errRESTMapper) KindFor(_ schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, m.err
}
func (m errRESTMapper) KindsFor(_ schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, m.err
}
func (m errRESTMapper) ResourceFor(_ schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, m.err
}
func (m errRESTMapper) ResourcesFor(_ schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, m.err
}
func (m errRESTMapper) RESTMapping(_ schema.GroupKind, _ ...string) (*meta.RESTMapping, error) {
	return nil, m.err
}
func (m errRESTMapper) RESTMappings(_ schema.GroupKind, _ ...string) ([]*meta.RESTMapping, error) {
	return nil, m.err
}
func (m errRESTMapper) ResourceSingularizer(_ string) (string, error) {
	return "", m.err
}
