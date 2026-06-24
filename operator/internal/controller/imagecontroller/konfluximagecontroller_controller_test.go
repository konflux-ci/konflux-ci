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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

const (
	imageControllerNamespace      = "image-controller"
	metricsServiceName            = "image-controller-controller-manager-metrics-service"
	prunerConfigMapName           = "image-controller-image-pruner-configmap-hgm7kmgb6k"
	leaderElectionRoleName        = "image-controller-leader-election-role"
	leaderElectionRoleBindingName = "image-controller-leader-election-rolebinding"
	managerClusterRoleName        = "image-controller-manager-role"
	managerClusterRoleBindingName = "image-controller-manager-rolebinding"
)

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
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Self-healing", func() {
		It("recreates Deployment when deleted", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			deploymentNN := types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: imageControllerNamespace,
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
				g.Expect(dep.Labels).To(HaveKeyWithValue("control-plane", "controller-manager"))
				manager := testutil.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(manager).NotTo(BeNil(), "manager container should exist")
				g.Expect(manager.Image).NotTo(BeEmpty(), "manager container image should be set")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ServiceAccount when deleted", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			saNN := types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: imageControllerNamespace,
			}

			By("waiting for initial ServiceAccount creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, saNN, &corev1.ServiceAccount{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ServiceAccount")
			Expect(k8sClient.Delete(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: saNN.Name, Namespace: saNN.Namespace},
			})).To(Succeed())

			By("verifying the ServiceAccount is recreated with correct labels")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(sa.Labels).To(HaveKeyWithValue("app.kubernetes.io/name", "image-controller"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates CronJob when deleted", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			cjNN := types.NamespacedName{
				Name:      imagePrunerCronJobName,
				Namespace: imageControllerNamespace,
			}

			By("waiting for initial CronJob creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, cjNN, &batchv1.CronJob{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the CronJob")
			Expect(k8sClient.Delete(ctx, &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{Name: cjNN.Name, Namespace: cjNN.Namespace},
			})).To(Succeed())

			By("verifying the CronJob is recreated with ownership labels")
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				g.Expect(cj.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates Service when deleted", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: imageControllerNamespace,
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

		It("recreates ConfigMap when deleted", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			cmNN := types.NamespacedName{
				Name:      prunerConfigMapName,
				Namespace: imageControllerNamespace,
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

		It("recreates Role when deleted", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			roleNN := types.NamespacedName{
				Name:      leaderElectionRoleName,
				Namespace: imageControllerNamespace,
			}

			By("waiting for initial Role creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, roleNN, &rbacv1.Role{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Role")
			Expect(k8sClient.Delete(ctx, &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: roleNN.Name, Namespace: roleNN.Namespace},
			})).To(Succeed())

			By("verifying the Role is recreated with ownership labels")
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				g.Expect(role.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates RoleBinding when deleted", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			rbNN := types.NamespacedName{
				Name:      leaderElectionRoleBindingName,
				Namespace: imageControllerNamespace,
			}

			By("waiting for initial RoleBinding creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, rbNN, &rbacv1.RoleBinding{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the RoleBinding")
			Expect(k8sClient.Delete(ctx, &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: rbNN.Name, Namespace: rbNN.Namespace},
			})).To(Succeed())

			By("verifying the RoleBinding is recreated with ownership labels")
			Eventually(func(g Gomega) {
				rb := &rbacv1.RoleBinding{}
				g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
				g.Expect(rb.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ClusterRole when deleted", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, imageController, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleName}})

			crNN := types.NamespacedName{Name: managerClusterRoleName}

			By("waiting for initial ClusterRole creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crNN, &rbacv1.ClusterRole{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ClusterRole")
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: crNN.Name},
			})).To(Succeed())

			By("verifying the ClusterRole is recreated with ownership labels")
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				g.Expect(cr.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ClusterRoleBinding when deleted", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, imageController, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleBindingName}})

			crbNN := types.NamespacedName{Name: managerClusterRoleBindingName}

			By("waiting for initial ClusterRoleBinding creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crbNN, &rbacv1.ClusterRoleBinding{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ClusterRoleBinding")
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: crbNN.Name},
			})).To(Succeed())

			By("verifying the ClusterRoleBinding is recreated with ownership labels")
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				g.Expect(crb.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Drift correction", func() {
		It("restores Deployment image when modified", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			deploymentNN := types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: imageControllerNamespace,
			}

			By("waiting for initial Deployment creation")
			var originalImage string
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				manager := testutil.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(manager).NotTo(BeNil())
				originalImage = manager.Image
				g.Expect(originalImage).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Deployment image")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				manager := testutil.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(manager).NotTo(BeNil())
				manager.Image = "tampered-image:latest"
				g.Expect(k8sClient.Update(ctx, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Deployment image is restored")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				m := testutil.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(m).NotTo(BeNil())
				g.Expect(m.Image).To(Equal(originalImage))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ServiceAccount labels when stripped", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			saNN := types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: imageControllerNamespace,
			}

			By("waiting for initial ServiceAccount creation with ownership labels")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the ServiceAccount")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				delete(sa.Labels, constant.KonfluxOwnerLabel)
				delete(sa.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, sa)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ServiceAccount labels are restored")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores CronJob image when modified", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			cjNN := types.NamespacedName{
				Name:      imagePrunerCronJobName,
				Namespace: imageControllerNamespace,
			}

			By("waiting for initial CronJob creation")
			var originalImage string
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, imagePrunerContainerName)
				g.Expect(container).NotTo(BeNil())
				originalImage = container.Image
				g.Expect(originalImage).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the CronJob image")
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, imagePrunerContainerName)
				g.Expect(container).NotTo(BeNil())
				container.Image = "tampered-image:latest"
				g.Expect(k8sClient.Update(ctx, cj)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the CronJob image is restored")
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, imagePrunerContainerName)
				g.Expect(container).NotTo(BeNil())
				g.Expect(container.Image).To(Equal(originalImage))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Service labels when stripped", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: imageControllerNamespace,
			}

			By("waiting for initial Service creation with ownership labels")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Service")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				delete(svc.Labels, constant.KonfluxOwnerLabel)
				delete(svc.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, svc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Service labels are restored")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Service spec when modified", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: imageControllerNamespace,
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

		It("restores ConfigMap data when modified", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			cmNN := types.NamespacedName{
				Name:      prunerConfigMapName,
				Namespace: imageControllerNamespace,
			}

			By("waiting for initial ConfigMap creation")
			var originalKey string
			var originalValue string
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				g.Expect(cm.Data).NotTo(BeEmpty())
				for k, v := range cm.Data {
					originalKey = k
					originalValue = v
					break
				}
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying an existing ConfigMap data key")
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				cm.Data[originalKey] = "tampered-content"
				g.Expect(k8sClient.Update(ctx, cm)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ConfigMap data is restored")
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				g.Expect(cm.Data).To(HaveKeyWithValue(originalKey, originalValue))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Namespace labels when stripped", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			nsNN := types.NamespacedName{Name: imageControllerNamespace}

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

		It("restores Role rules when modified", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			roleNN := types.NamespacedName{
				Name:      leaderElectionRoleName,
				Namespace: imageControllerNamespace,
			}

			By("waiting for initial Role creation")
			var originalRules []rbacv1.PolicyRule
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				g.Expect(role.Rules).NotTo(BeEmpty())
				originalRules = role.Rules
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Role rules")
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				role.Rules = []rbacv1.PolicyRule{{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"delete"},
				}}
				g.Expect(k8sClient.Update(ctx, role)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Role rules are restored")
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				g.Expect(role.Rules).To(Equal(originalRules))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores RoleBinding subjects when modified", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			rbNN := types.NamespacedName{
				Name:      leaderElectionRoleBindingName,
				Namespace: imageControllerNamespace,
			}

			By("waiting for initial RoleBinding creation")
			var originalSubjects []rbacv1.Subject
			Eventually(func(g Gomega) {
				rb := &rbacv1.RoleBinding{}
				g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
				g.Expect(rb.Subjects).NotTo(BeEmpty())
				originalSubjects = rb.Subjects
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the RoleBinding subjects")
			Eventually(func(g Gomega) {
				rb := &rbacv1.RoleBinding{}
				g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
				rb.Subjects = []rbacv1.Subject{{
					Kind:     "User",
					Name:     "tampered-user",
					APIGroup: "rbac.authorization.k8s.io",
				}}
				g.Expect(k8sClient.Update(ctx, rb)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the RoleBinding subjects are restored")
			Eventually(func(g Gomega) {
				rb := &rbacv1.RoleBinding{}
				g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
				g.Expect(rb.Subjects).To(Equal(originalSubjects))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ClusterRole rules when modified", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, imageController, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleName}})

			crNN := types.NamespacedName{Name: managerClusterRoleName}

			By("waiting for initial ClusterRole creation")
			var originalRules []rbacv1.PolicyRule
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				g.Expect(cr.Rules).NotTo(BeEmpty())
				originalRules = cr.Rules
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ClusterRole rules")
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				cr.Rules = []rbacv1.PolicyRule{{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"delete"},
				}}
				g.Expect(k8sClient.Update(ctx, cr)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ClusterRole rules are restored")
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				g.Expect(cr.Rules).To(Equal(originalRules))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ClusterRoleBinding subjects when modified", func(ctx context.Context) {
			imageController := &konfluxv1alpha1.KonfluxImageController{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, imageController, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleBindingName}})

			crbNN := types.NamespacedName{Name: managerClusterRoleBindingName}

			By("waiting for initial ClusterRoleBinding creation")
			var originalSubjects []rbacv1.Subject
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				g.Expect(crb.Subjects).NotTo(BeEmpty())
				originalSubjects = crb.Subjects
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ClusterRoleBinding subjects")
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				crb.Subjects = []rbacv1.Subject{{
					Kind:      "ServiceAccount",
					Name:      "tampered-sa",
					Namespace: "default",
				}}
				g.Expect(k8sClient.Update(ctx, crb)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ClusterRoleBinding subjects are restored")
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				g.Expect(crb.Subjects).To(Equal(originalSubjects))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
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
			Eventually(getDeployment).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Not(BeNil()))
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
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
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
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
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
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
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
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

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
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})
})
