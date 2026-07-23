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

	"k8s.io/utils/ptr"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

const (
	namespaceListerNamespace            = "namespace-lister"
	authorizerClusterRoleName           = "namespace-lister-authorizer"
	networkPolicyAllowFromKonfluxUIName = "namespace-lister-allow-from-konfluxui"
	networkPolicyAllowToAPIServerName   = "namespace-lister-allow-to-apiserver"
)

// findEnvValue returns the last value of the named env var.
// Checks the last match to match Kubernetes behavior where later entries override earlier ones.
//
//nolint:unparam
func findEnvValue(envs []corev1.EnvVar, name string) (string, bool) {
	var value string
	var found bool
	for _, env := range envs {
		if env.Name == name {
			value = env.Value
			found = true
		}
	}
	return value, found
}

var _ = Describe("KonfluxNamespaceLister Controller", func() {
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxNamespaceLister{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
			})

			// Wait for the Deployment rather than Ready=True: UpdateComponentStatuses
			// gates Ready=True on ReadyReplicas == Replicas, which never happens in
			// envtest (no kubelet → pods never start).
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      namespaceListerNamespace,
					Namespace: namespaceListerNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Self-healing", func() {
		It("recreates Deployment when deleted", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			deploymentNN := types.NamespacedName{
				Name:      namespaceListerNamespace,
				Namespace: namespaceListerNamespace,
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
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, namespaceListerContainerName)
				g.Expect(container).NotTo(BeNil(), "namespace-lister container should exist")
				g.Expect(container.Image).NotTo(BeEmpty(), "container image should be set")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ServiceAccount when deleted", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			saNN := types.NamespacedName{
				Name:      namespaceListerNamespace,
				Namespace: namespaceListerNamespace,
			}

			By("waiting for initial ServiceAccount creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, saNN, &corev1.ServiceAccount{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ServiceAccount")
			Expect(k8sClient.Delete(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: saNN.Name, Namespace: saNN.Namespace},
			})).To(Succeed())

			By("verifying the ServiceAccount is recreated with ownership labels")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ingress NetworkPolicy when deleted", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			npNN := types.NamespacedName{
				Name:      networkPolicyAllowFromKonfluxUIName,
				Namespace: namespaceListerNamespace,
			}

			By("waiting for initial NetworkPolicy creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, npNN, &networkingv1.NetworkPolicy{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the NetworkPolicy")
			Expect(k8sClient.Delete(ctx, &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: npNN.Name, Namespace: npNN.Namespace},
			})).To(Succeed())

			By("verifying the NetworkPolicy is recreated with ownership labels")
			Eventually(func(g Gomega) {
				np := &networkingv1.NetworkPolicy{}
				g.Expect(k8sClient.Get(ctx, npNN, np)).To(Succeed())
				g.Expect(np.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates egress NetworkPolicy when deleted", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			npNN := types.NamespacedName{
				Name:      networkPolicyAllowToAPIServerName,
				Namespace: namespaceListerNamespace,
			}

			By("waiting for initial NetworkPolicy creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, npNN, &networkingv1.NetworkPolicy{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the NetworkPolicy")
			Expect(k8sClient.Delete(ctx, &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: npNN.Name, Namespace: npNN.Namespace},
			})).To(Succeed())

			By("verifying the NetworkPolicy is recreated with ownership labels")
			Eventually(func(g Gomega) {
				np := &networkingv1.NetworkPolicy{}
				g.Expect(k8sClient.Get(ctx, npNN, np)).To(Succeed())
				g.Expect(np.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates Certificate when deleted", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			certNN := types.NamespacedName{
				Name:      namespaceListerNamespace,
				Namespace: namespaceListerNamespace,
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

		It("recreates Service when deleted", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			svcNN := types.NamespacedName{
				Name:      namespaceListerNamespace,
				Namespace: namespaceListerNamespace,
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

		It("recreates ClusterRole when deleted", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, namespaceLister,
				&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: authorizerClusterRoleName}},
			)

			crNN := types.NamespacedName{Name: authorizerClusterRoleName}

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
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, namespaceLister,
				&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: authorizerClusterRoleName}},
			)

			crbNN := types.NamespacedName{Name: authorizerClusterRoleName}

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
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			deploymentNN := types.NamespacedName{
				Name:      namespaceListerNamespace,
				Namespace: namespaceListerNamespace,
			}

			By("waiting for initial Deployment creation")
			var originalImage string
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, namespaceListerContainerName)
				g.Expect(container).NotTo(BeNil())
				originalImage = container.Image
				g.Expect(originalImage).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Deployment image")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, namespaceListerContainerName)
				g.Expect(container).NotTo(BeNil())
				container.Image = "tampered-image:latest"
				g.Expect(k8sClient.Update(ctx, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Deployment image is restored")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, namespaceListerContainerName)
				g.Expect(container).NotTo(BeNil())
				g.Expect(container.Image).To(Equal(originalImage))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ServiceAccount labels when stripped", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			saNN := types.NamespacedName{
				Name:      namespaceListerNamespace,
				Namespace: namespaceListerNamespace,
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

		It("restores Service labels when stripped", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			svcNN := types.NamespacedName{
				Name:      namespaceListerNamespace,
				Namespace: namespaceListerNamespace,
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
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			svcNN := types.NamespacedName{
				Name:      namespaceListerNamespace,
				Namespace: namespaceListerNamespace,
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

		It("restores Namespace labels when stripped", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			nsNN := types.NamespacedName{Name: namespaceListerNamespace}

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

		It("restores ClusterRole rules when modified", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, namespaceLister,
				&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: authorizerClusterRoleName}},
			)

			crNN := types.NamespacedName{Name: authorizerClusterRoleName}

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
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, namespaceLister,
				&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: authorizerClusterRoleName}},
			)

			crbNN := types.NamespacedName{Name: authorizerClusterRoleName}

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

		It("restores NetworkPolicy labels when stripped", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			npNN := types.NamespacedName{
				Name:      networkPolicyAllowFromKonfluxUIName,
				Namespace: namespaceListerNamespace,
			}

			By("waiting for initial NetworkPolicy creation with ownership labels")
			Eventually(func(g Gomega) {
				np := &networkingv1.NetworkPolicy{}
				g.Expect(k8sClient.Get(ctx, npNN, np)).To(Succeed())
				g.Expect(np.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the NetworkPolicy")
			Eventually(func(g Gomega) {
				np := &networkingv1.NetworkPolicy{}
				g.Expect(k8sClient.Get(ctx, npNN, np)).To(Succeed())
				delete(np.Labels, constant.KonfluxOwnerLabel)
				delete(np.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, np)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the NetworkPolicy labels are restored")
			Eventually(func(g Gomega) {
				np := &networkingv1.NetworkPolicy{}
				g.Expect(k8sClient.Get(ctx, npNN, np)).To(Succeed())
				g.Expect(np.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(np.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Certificate labels when stripped", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			certNN := types.NamespacedName{
				Name:      namespaceListerNamespace,
				Namespace: namespaceListerNamespace,
			}

			By("waiting for initial Certificate creation with ownership labels")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				g.Expect(cert.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Certificate")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				delete(cert.Labels, constant.KonfluxOwnerLabel)
				delete(cert.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, cert)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Certificate labels are restored")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				g.Expect(cert.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(cert.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ingress NetworkPolicy spec when modified", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			npNN := types.NamespacedName{
				Name:      networkPolicyAllowFromKonfluxUIName,
				Namespace: namespaceListerNamespace,
			}

			By("waiting for initial ingress NetworkPolicy creation")
			Eventually(func(g Gomega) {
				np := &networkingv1.NetworkPolicy{}
				g.Expect(k8sClient.Get(ctx, npNN, np)).To(Succeed())
				g.Expect(np.Spec.Ingress).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the NetworkPolicy ingress rules")
			Eventually(func(g Gomega) {
				np := &networkingv1.NetworkPolicy{}
				g.Expect(k8sClient.Get(ctx, npNN, np)).To(Succeed())
				np.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{}
				g.Expect(k8sClient.Update(ctx, np)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the NetworkPolicy ingress rules are restored")
			Eventually(func(g Gomega) {
				np := &networkingv1.NetworkPolicy{}
				g.Expect(k8sClient.Get(ctx, npNN, np)).To(Succeed())
				g.Expect(np.Spec.Ingress).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores egress NetworkPolicy spec when modified", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			npNN := types.NamespacedName{
				Name:      networkPolicyAllowToAPIServerName,
				Namespace: namespaceListerNamespace,
			}

			By("waiting for initial egress NetworkPolicy creation")
			Eventually(func(g Gomega) {
				np := &networkingv1.NetworkPolicy{}
				g.Expect(k8sClient.Get(ctx, npNN, np)).To(Succeed())
				g.Expect(np.Spec.Egress).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the NetworkPolicy egress rules")
			Eventually(func(g Gomega) {
				np := &networkingv1.NetworkPolicy{}
				g.Expect(k8sClient.Get(ctx, npNN, np)).To(Succeed())
				np.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
				g.Expect(k8sClient.Update(ctx, np)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the NetworkPolicy egress rules are restored")
			Eventually(func(g Gomega) {
				np := &networkingv1.NetworkPolicy{}
				g.Expect(k8sClient.Get(ctx, npNN, np)).To(Succeed())
				g.Expect(np.Spec.Egress).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Certificate spec when modified", func(ctx context.Context) {
			namespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, namespaceLister)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, namespaceLister)

			certNN := types.NamespacedName{
				Name:      namespaceListerNamespace,
				Namespace: namespaceListerNamespace,
			}

			By("waiting for initial Certificate creation")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				g.Expect(cert.Spec.DNSNames).NotTo(BeEmpty())
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
				g.Expect(cert.Spec.DNSNames).To(ContainElement(namespaceListerNamespace + "." + namespaceListerNamespace + ".svc"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
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
								Name: namespaceListerContainerName,
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
				Replicas: ptr.To(int32(3)),
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))
	})

	It("should scale namespace-lister to zero", func() {
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
				Replicas: ptr.To(int32(0)),
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(*deployment.Spec.Replicas).To(Equal(int32(0)))
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
				Replicas: ptr.To(int32(2)),
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

	Context("logLevel typed field", func() {
		It("should inject LOG_LEVEL as slog integer when set to info", func() {
			spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
				LogLevel: konfluxv1alpha1.LogLevelInfo,
			}
			err := applyNamespaceListerCustomizations(deployment, spec)
			Expect(err).NotTo(HaveOccurred())

			container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
			Expect(container).NotTo(BeNil())
			val, found := findEnvValue(container.Env, envLogLevel)
			Expect(found).To(BeTrue())
			Expect(val).To(Equal("0"))
		})

		It("should map all enum values to correct slog integers", func() {
			cases := map[konfluxv1alpha1.LogLevel]string{
				konfluxv1alpha1.LogLevelDebug: "-4",
				konfluxv1alpha1.LogLevelInfo:  "0",
				konfluxv1alpha1.LogLevelWarn:  "4",
				konfluxv1alpha1.LogLevelError: "8",
			}
			for level, expected := range cases {
				spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
					LogLevel: level,
				}
				err := applyNamespaceListerCustomizations(deployment, spec)
				Expect(err).NotTo(HaveOccurred())

				container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
				Expect(container).NotTo(BeNil())
				val, found := findEnvValue(container.Env, envLogLevel)
				Expect(found).To(BeTrue())
				Expect(val).To(Equal(expected), "logLevel %q should map to %q", level, expected)
			}
		})

		It("should not inject LOG_LEVEL when omitted", func() {
			spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{}
			err := applyNamespaceListerCustomizations(deployment, spec)
			Expect(err).NotTo(HaveOccurred())

			container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
			Expect(container).NotTo(BeNil())
			_, found := findEnvValue(container.Env, envLogLevel)
			Expect(found).To(BeFalse())
		})

		It("should return an error for unsupported logLevel values", func() {
			spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
				LogLevel: konfluxv1alpha1.LogLevel("trace"),
			}
			err := applyNamespaceListerCustomizations(deployment, spec)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported logLevel"))
		})

		It("should take precedence over same var in ContainerSpec.Env", func() {
			spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
				LogLevel: konfluxv1alpha1.LogLevelDebug,
				NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
					NamespaceLister: &konfluxv1alpha1.ContainerSpec{
						Env: []corev1.EnvVar{
							{Name: envLogLevel, Value: "8"},
						},
					},
				},
			}
			err := applyNamespaceListerCustomizations(deployment, spec)
			Expect(err).NotTo(HaveOccurred())

			container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
			Expect(container).NotTo(BeNil())
			val, found := findEnvValue(container.Env, envLogLevel)
			Expect(found).To(BeTrue())
			Expect(val).To(Equal("-4"), "typed CRD field should take precedence over ContainerSpec.Env")
		})

		It("should let ContainerSpec.Env pass through when typed field is omitted", func() {
			spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
				NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
					NamespaceLister: &konfluxv1alpha1.ContainerSpec{
						Env: []corev1.EnvVar{
							{Name: envLogLevel, Value: "4"},
						},
					},
				},
			}
			err := applyNamespaceListerCustomizations(deployment, spec)
			Expect(err).NotTo(HaveOccurred())

			container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
			Expect(container).NotTo(BeNil())
			val, found := findEnvValue(container.Env, envLogLevel)
			Expect(found).To(BeTrue())
			Expect(val).To(Equal("4"), "ContainerSpec.Env should pass through when typed field is omitted")
		})
	})

	Context("cacheResyncPeriod typed field", func() {
		It("should inject CACHE_RESYNC_PERIOD when set", func() {
			spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
				CacheResyncPeriod: "10m",
			}
			err := applyNamespaceListerCustomizations(deployment, spec)
			Expect(err).NotTo(HaveOccurred())

			container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
			Expect(container).NotTo(BeNil())
			val, found := findEnvValue(container.Env, envCacheResyncPeriod)
			Expect(found).To(BeTrue())
			Expect(val).To(Equal("10m"))
		})

		It("should not inject CACHE_RESYNC_PERIOD when omitted", func() {
			spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{}
			err := applyNamespaceListerCustomizations(deployment, spec)
			Expect(err).NotTo(HaveOccurred())

			container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
			Expect(container).NotTo(BeNil())
			_, found := findEnvValue(container.Env, envCacheResyncPeriod)
			Expect(found).To(BeFalse())
		})

		It("should accept various duration formats", func() {
			for _, dur := range []string{"5s", "1h", "30m", "1h30m"} {
				spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
					CacheResyncPeriod: dur,
				}
				err := applyNamespaceListerCustomizations(deployment, spec)
				Expect(err).NotTo(HaveOccurred())

				container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
				Expect(container).NotTo(BeNil())
				val, found := findEnvValue(container.Env, envCacheResyncPeriod)
				Expect(found).To(BeTrue())
				Expect(val).To(Equal(dur))
			}
		})

		It("should take precedence over same var in ContainerSpec.Env", func() {
			spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
				CacheResyncPeriod: "5m",
				NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
					NamespaceLister: &konfluxv1alpha1.ContainerSpec{
						Env: []corev1.EnvVar{
							{Name: envCacheResyncPeriod, Value: "30m"},
						},
					},
				},
			}
			err := applyNamespaceListerCustomizations(deployment, spec)
			Expect(err).NotTo(HaveOccurred())

			container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
			Expect(container).NotTo(BeNil())
			val, found := findEnvValue(container.Env, envCacheResyncPeriod)
			Expect(found).To(BeTrue())
			Expect(val).To(Equal("5m"), "typed CRD field should take precedence over ContainerSpec.Env")
		})

		It("should let ContainerSpec.Env pass through when typed field is omitted", func() {
			spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
				NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
					NamespaceLister: &konfluxv1alpha1.ContainerSpec{
						Env: []corev1.EnvVar{
							{Name: envCacheResyncPeriod, Value: "30m"},
						},
					},
				},
			}
			err := applyNamespaceListerCustomizations(deployment, spec)
			Expect(err).NotTo(HaveOccurred())

			container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
			Expect(container).NotTo(BeNil())
			val, found := findEnvValue(container.Env, envCacheResyncPeriod)
			Expect(found).To(BeTrue())
			Expect(val).To(Equal("30m"), "ContainerSpec.Env should pass through when typed field is omitted")
		})
	})
})
