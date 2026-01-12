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

package namespacelister

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("KonfluxNamespaceLister Controller", func() {
	Context("When reconciling a resource", func() {

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      CRName,
			Namespace: "default",
		}
		konfluxnamespacelister := &konfluxv1alpha1.KonfluxNamespaceLister{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KonfluxNamespaceLister")
			err := k8sClient.Get(ctx, typeNamespacedName, konfluxnamespacelister)
			if err != nil && errors.IsNotFound(err) {
				resource := &konfluxv1alpha1.KonfluxNamespaceLister{
					ObjectMeta: metav1.ObjectMeta{
						Name:      CRName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &konfluxv1alpha1.KonfluxNamespaceLister{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KonfluxNamespaceLister")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KonfluxNamespaceListerReconciler{
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
})

var _ = Describe("applyNamespaceListerCustomizations", func() {
	var deployment *appsv1.Deployment

	BeforeEach(func() {
		replicas := int32(1)
		deployment = &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "namespace-lister",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("50m"),
										corev1.ResourceMemory: resource.MustParse("64Mi"),
									},
								},
							},
						},
					},
				},
			},
		}
	})

	It("should not modify deployment with empty spec", func() {
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{}
		err := applyNamespaceListerCustomizations(deployment, spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
	})

	It("should not modify deployment with nil deployment spec", func() {
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			NamespaceLister: nil,
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
	})

	It("should apply replicas override", func() {
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
				Replicas: 3,
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))
	})

	It("should apply resources override", func() {
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
				NamespaceLister: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		Expect(err).NotTo(HaveOccurred())

		container := deployment.Spec.Template.Spec.Containers[0]
		Expect(container.Resources.Requests.Cpu().String()).To(Equal("100m"))
		Expect(container.Resources.Requests.Memory().String()).To(Equal("128Mi"))
		Expect(container.Resources.Limits.Cpu().String()).To(Equal("500m"))
		Expect(container.Resources.Limits.Memory().String()).To(Equal("512Mi"))
	})

	It("should apply both replicas and resources", func() {
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
				Replicas: 2,
				NamespaceLister: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))
		Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String()).To(Equal("256Mi"))
	})
})
