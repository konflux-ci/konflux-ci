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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

const imageControllerNamespace = "image-controller"

var _ = Describe("KonfluxImageController Controller", func() {
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxImageController{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
			})

			// Wait for the Deployment rather than Ready=True: UpdateComponentStatuses
			// gates Ready=True on ReadyReplicas == Replicas, which never happens in
			// envtest (no kubelet → pods never start).
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      controllerManagerDeploymentName,
					Namespace: imageControllerNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})
	})

	Context("Quay CA Bundle", func() {
		var imageController *konfluxv1alpha1.KonfluxImageController

		// waitForDeployment polls until the Deployment exists and returns it.
		getDeployment := func(g Gomega) *appsv1.Deployment {
			dep := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: imageControllerNamespace,
			}, dep)).To(Succeed())
			return dep
		}

		BeforeEach(func() {
			imageController = &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())

			// Same reasoning as BeforeAll above: wait for Deployment existence, not Ready=True.
			Eventually(getDeployment).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Not(BeNil()))
		})

		AfterEach(func() {
			testutil.DeleteAndWait(ctx, k8sClient, imageController)
		})

		It("should NOT set QUAY_ADDITIONAL_CA when QuayCABundle is not configured", func() {
			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				managerContainer := testutil.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(managerContainer).NotTo(BeNil())

				for _, e := range managerContainer.Env {
					g.Expect(e.Name).NotTo(Equal(quayAdditionalCAEnvVar), "QUAY_ADDITIONAL_CA should not be set")
				}

				var caVolume *corev1.Volume
				for i := range dep.Spec.Template.Spec.Volumes {
					if dep.Spec.Template.Spec.Volumes[i].Name == quayCABundleVolumeName {
						caVolume = &dep.Spec.Template.Spec.Volumes[i]
						break
					}
				}
				g.Expect(caVolume).NotTo(BeNil(), "quay-ca-bundle volume should exist from base manifests")
				g.Expect(caVolume.ConfigMap).NotTo(BeNil())
				g.Expect(caVolume.ConfigMap.Name).To(Equal(defaultQuayCAConfigMapName))
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})

		It("should set QUAY_ADDITIONAL_CA when QuayCABundle is configured", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, imageController)).To(Succeed())
			imageController.Spec.QuayCABundle = &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "quay-ca.crt",
			}
			Expect(k8sClient.Update(ctx, imageController)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				managerContainer := testutil.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(managerContainer).NotTo(BeNil())
				var found bool
				for _, e := range managerContainer.Env {
					if e.Name == quayAdditionalCAEnvVar {
						found = true
						g.Expect(e.Value).To(Equal("/etc/ssl/certs/quay-ca/quay-ca.crt"))
						break
					}
				}
				g.Expect(found).To(BeTrue(), "QUAY_ADDITIONAL_CA should be set")
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})

		It("should update ConfigMap volume name when custom ConfigMap is specified", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, imageController)).To(Succeed())
			imageController.Spec.QuayCABundle = &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "my-custom-ca-bundle",
				Key:           "ca.crt",
			}
			Expect(k8sClient.Update(ctx, imageController)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)

				var caVolume *corev1.Volume
				for i := range dep.Spec.Template.Spec.Volumes {
					if dep.Spec.Template.Spec.Volumes[i].Name == quayCABundleVolumeName {
						caVolume = &dep.Spec.Template.Spec.Volumes[i]
						break
					}
				}
				g.Expect(caVolume).NotTo(BeNil(), "quay-ca-bundle volume should exist")
				g.Expect(caVolume.ConfigMap).NotTo(BeNil())
				g.Expect(caVolume.ConfigMap.Name).To(Equal("my-custom-ca-bundle"))

				managerContainer := testutil.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(managerContainer).NotTo(BeNil())
				var found bool
				for _, e := range managerContainer.Env {
					if e.Name == quayAdditionalCAEnvVar {
						found = true
						g.Expect(e.Value).To(Equal("/etc/ssl/certs/quay-ca/ca.crt"))
						break
					}
				}
				g.Expect(found).To(BeTrue(), "QUAY_ADDITIONAL_CA should be set with custom key")
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})

		It("should remove QUAY_ADDITIONAL_CA when QuayCABundle is removed", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, imageController)).To(Succeed())
			imageController.Spec.QuayCABundle = &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "quay-ca.crt",
			}
			Expect(k8sClient.Update(ctx, imageController)).To(Succeed())

			// Wait for QUAY_ADDITIONAL_CA to appear first.
			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				managerContainer := testutil.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(managerContainer).NotTo(BeNil())
				var found bool
				for _, e := range managerContainer.Env {
					if e.Name == quayAdditionalCAEnvVar {
						found = true
						break
					}
				}
				g.Expect(found).To(BeTrue(), "QUAY_ADDITIONAL_CA should be set before removal")
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, imageController)).To(Succeed())
			imageController.Spec.QuayCABundle = nil
			Expect(k8sClient.Update(ctx, imageController)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				managerContainer := testutil.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(managerContainer).NotTo(BeNil())
				for _, e := range managerContainer.Env {
					g.Expect(e.Name).NotTo(Equal(quayAdditionalCAEnvVar), "QUAY_ADDITIONAL_CA should not be present after removal")
				}
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})
	})
})
