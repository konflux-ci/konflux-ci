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

package imagecontroller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

var _ = Describe("KonfluxImageController Controller", func() {
	Context("When reconciling a resource", func() {

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: CRName,
		}
		konfluximagecontroller := &konfluxv1alpha1.KonfluxImageController{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KonfluxImageController")
			err := k8sClient.Get(ctx, typeNamespacedName, konfluximagecontroller)
			if err != nil && errors.IsNotFound(err) {
				resource := &konfluxv1alpha1.KonfluxImageController{
					ObjectMeta: metav1.ObjectMeta{
						Name: CRName,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &konfluxv1alpha1.KonfluxImageController{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KonfluxImageController")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KonfluxImageControllerReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Quay CA Bundle", func() {
		var (
			ctx                context.Context
			imageController    *konfluxv1alpha1.KonfluxImageController
			reconciler         *KonfluxImageControllerReconciler
			typeNamespacedName types.NamespacedName
		)

		reconcileImageController := func(ctx context.Context) {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		getManagerDeployment := func(ctx context.Context) *appsv1.Deployment {
			deployment := &appsv1.Deployment{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: "image-controller",
			}, deployment)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			return deployment
		}

		BeforeEach(func() {
			ctx = context.Background()
			typeNamespacedName = types.NamespacedName{
				Name: CRName,
			}

			reconciler = &KonfluxImageControllerReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			imageController = &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxImageControllerSpec{},
			}

			err := k8sClient.Get(ctx, typeNamespacedName, &konfluxv1alpha1.KonfluxImageController{})
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			}
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, imageController)
		})

		It("should NOT set QUAY_ADDITIONAL_CA when QuayCABundle is not configured", func() {
			By("reconciling without QuayCABundle")
			reconcileImageController(ctx)

			By("verifying deployment has no QUAY_ADDITIONAL_CA env var")
			deployment := getManagerDeployment(ctx)
			managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
			Expect(managerContainer).NotTo(BeNil())

			var envVar *string
			for _, e := range managerContainer.Env {
				if e.Name == quayAdditionalCAEnvVar {
					envVar = &e.Value
					break
				}
			}
			Expect(envVar).To(BeNil(), "QUAY_ADDITIONAL_CA should not be set when QuayCABundle is not configured")

			By("verifying quay-ca-bundle volume still exists from base manifests")
			var caVolume bool
			for _, v := range deployment.Spec.Template.Spec.Volumes {
				if v.Name == quayCABundleVolumeName {
					caVolume = true
					Expect(v.ConfigMap).NotTo(BeNil())
					Expect(v.ConfigMap.Name).To(Equal(defaultQuayCAConfigMapName))
					break
				}
			}
			Expect(caVolume).To(BeTrue(), "quay-ca-bundle volume should exist from base manifests")
		})

		It("should set QUAY_ADDITIONAL_CA when QuayCABundle is configured", func() {
			By("updating the CR with QuayCABundle spec")
			err := k8sClient.Get(ctx, typeNamespacedName, imageController)
			Expect(err).NotTo(HaveOccurred())
			imageController.Spec.QuayCABundle = &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "quay-ca.crt",
			}
			Expect(k8sClient.Update(ctx, imageController)).To(Succeed())

			By("reconciling the resource")
			reconcileImageController(ctx)

			By("verifying QUAY_ADDITIONAL_CA is set on the manager container")
			deployment := getManagerDeployment(ctx)
			managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
			Expect(managerContainer).NotTo(BeNil())

			var foundEnv bool
			for _, e := range managerContainer.Env {
				if e.Name == quayAdditionalCAEnvVar {
					foundEnv = true
					Expect(e.Value).To(Equal("/etc/ssl/certs/quay-ca/quay-ca.crt"))
					break
				}
			}
			Expect(foundEnv).To(BeTrue(), "QUAY_ADDITIONAL_CA should be set")
		})

		It("should update ConfigMap volume name when custom ConfigMap is specified", func() {
			By("creating CR with custom ConfigMap name")
			err := k8sClient.Get(ctx, typeNamespacedName, imageController)
			Expect(err).NotTo(HaveOccurred())
			imageController.Spec.QuayCABundle = &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "my-custom-ca-bundle",
				Key:           "ca.crt",
			}
			Expect(k8sClient.Update(ctx, imageController)).To(Succeed())

			By("reconciling the resource")
			reconcileImageController(ctx)

			By("verifying the ConfigMap volume name is updated")
			deployment := getManagerDeployment(ctx)
			var found bool
			for _, v := range deployment.Spec.Template.Spec.Volumes {
				if v.Name == quayCABundleVolumeName {
					found = true
					Expect(v.ConfigMap).NotTo(BeNil())
					Expect(v.ConfigMap.Name).To(Equal("my-custom-ca-bundle"))
					break
				}
			}
			Expect(found).To(BeTrue(), "quay-ca-bundle volume should exist")

			By("verifying QUAY_ADDITIONAL_CA uses the correct key")
			managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
			Expect(managerContainer).NotTo(BeNil())
			var foundEnv bool
			for _, e := range managerContainer.Env {
				if e.Name == quayAdditionalCAEnvVar {
					foundEnv = true
					Expect(e.Value).To(Equal("/etc/ssl/certs/quay-ca/ca.crt"))
					break
				}
			}
			Expect(foundEnv).To(BeTrue(), "QUAY_ADDITIONAL_CA should be set")
		})

		It("should remove QUAY_ADDITIONAL_CA when QuayCABundle is removed", func() {
			By("creating CR with QuayCABundle")
			err := k8sClient.Get(ctx, typeNamespacedName, imageController)
			Expect(err).NotTo(HaveOccurred())
			imageController.Spec.QuayCABundle = &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "quay-ca.crt",
			}
			Expect(k8sClient.Update(ctx, imageController)).To(Succeed())

			By("reconciling with QuayCABundle")
			reconcileImageController(ctx)

			By("removing QuayCABundle from the CR")
			err = k8sClient.Get(ctx, typeNamespacedName, imageController)
			Expect(err).NotTo(HaveOccurred())
			imageController.Spec.QuayCABundle = nil
			Expect(k8sClient.Update(ctx, imageController)).To(Succeed())

			By("reconciling without QuayCABundle")
			reconcileImageController(ctx)

			By("verifying QUAY_ADDITIONAL_CA is no longer set")
			deployment := getManagerDeployment(ctx)
			managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
			Expect(managerContainer).NotTo(BeNil())

			for _, e := range managerContainer.Env {
				Expect(e.Name).NotTo(Equal(quayAdditionalCAEnvVar),
					"QUAY_ADDITIONAL_CA should not be present after removing QuayCABundle")
			}
		})
	})
})
