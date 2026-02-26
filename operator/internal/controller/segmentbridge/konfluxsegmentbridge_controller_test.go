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

package segmentbridge

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

var _ = Describe("KonfluxSegmentBridge Controller", func() {
	Context("When reconciling a resource", func() {

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: CRName,
		}
		konfluxsegmentbridge := &konfluxv1alpha1.KonfluxSegmentBridge{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KonfluxSegmentBridge")
			err := k8sClient.Get(ctx, typeNamespacedName, konfluxsegmentbridge)
			if err != nil && errors.IsNotFound(err) {
				resource := &konfluxv1alpha1.KonfluxSegmentBridge{
					ObjectMeta: metav1.ObjectMeta{
						Name: CRName,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KonfluxSegmentBridge")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Cleanup the segment-bridge-config secret if it exists")
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)
			if err == nil {
				_ = k8sClient.Delete(ctx, secret)
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should skip Secret creation when no write key is configured", func() {
			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "Secret should not be created when no key is set")
		})

		It("should create Secret with both SEGMENT_WRITE_KEY and SEGMENT_BATCH_API from inline CR fields", func() {
			By("Updating the CR with inline segment config")
			resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.SegmentKey = "test-write-key"
			resource.Spec.SegmentAPIURL = "https://console.redhat.com/connections/api/v1"
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed())

			Expect(string(secret.Data["SEGMENT_WRITE_KEY"])).To(Equal("test-write-key"))
			Expect(string(secret.Data["SEGMENT_BATCH_API"])).To(Equal("https://console.redhat.com/connections/api/v1/batch"))
		})

		It("should use default URL when only segmentKey is set", func() {
			By("Updating the CR with only a segment key")
			resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.SegmentKey = "default-key"
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed())

			Expect(string(secret.Data["SEGMENT_WRITE_KEY"])).To(Equal("default-key"))
			Expect(string(secret.Data["SEGMENT_BATCH_API"])).To(Equal(
				konfluxv1alpha1.DefaultSegmentAPIURL + "/batch"))
		})

	})
})
