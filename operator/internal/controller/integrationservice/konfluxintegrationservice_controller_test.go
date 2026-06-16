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

package integrationservice

import (
	"context"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

const (
	integrationServiceNamespace = "integration-service"
	validatingWebhookName       = "integration-service-validating-webhook-configuration"
	mutatingWebhookName         = "integration-service-mutating-webhook-configuration"
	servingCertificateName      = "serving-cert"
	selfsignedIssuerName        = "selfsigned-issuer"
)

var _ = Describe("KonfluxIntegrationService Controller", func() {
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxIntegrationService{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
			})

			// Wait for the Deployment rather than Ready=True: UpdateComponentStatuses
			// gates Ready=True on ReadyReplicas == Replicas, which never happens in
			// envtest (no kubelet → pods never start).
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      controllerManagerDeploymentName,
					Namespace: integrationServiceNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should inject snapshot GC retention env vars onto the CronJob when typed fields are set", func(ctx context.Context) {
			prToKeep := "5"
			nonPRToKeep := "10"
			minToKeep := "2"

			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxIntegrationServiceSpec{
					PRSnapshotsToKeep:              prToKeep,
					NonPRSnapshotsToKeep:           nonPRToKeep,
					MinSnapshotsToKeepPerComponent: minToKeep,
				},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxIntegrationService{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
			})

			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      snapshotGCCronJobName,
					Namespace: integrationServiceNamespace,
				}, cj)).To(Succeed())

				container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
				g.Expect(container).NotTo(BeNil())

				prVar := findEnvVar(container.Env, envPRSnapshotsToKeep)
				g.Expect(prVar).NotTo(BeNil())
				g.Expect(prVar.Value).To(Equal(prToKeep))

				nonPRVar := findEnvVar(container.Env, envNonPRSnapshotsToKeep)
				g.Expect(nonPRVar).NotTo(BeNil())
				g.Expect(nonPRVar.Value).To(Equal(nonPRToKeep))

				minVar := findEnvVar(container.Env, envMinSnapshotsToKeepPerComponent)
				g.Expect(minVar).NotTo(BeNil())
				g.Expect(minVar.Value).To(Equal(minToKeep))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Self-healing", func() {
		It("recreates Deployment when deleted", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			deploymentNN := types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			saNN := types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: integrationServiceNamespace,
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

		It("recreates ValidatingWebhookConfiguration when deleted", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			By("waiting for initial ValidatingWebhookConfiguration creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: validatingWebhookName},
					&admissionregistrationv1.ValidatingWebhookConfiguration{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: validatingWebhookName},
			})

			By("deleting the ValidatingWebhookConfiguration")
			Expect(k8sClient.Delete(ctx, &admissionregistrationv1.ValidatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: validatingWebhookName},
			})).To(Succeed())

			By("verifying the ValidatingWebhookConfiguration is recreated with ownership labels")
			Eventually(func(g Gomega) {
				vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: validatingWebhookName}, vwc)).To(Succeed())
				g.Expect(vwc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates MutatingWebhookConfiguration when deleted", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			By("waiting for initial MutatingWebhookConfiguration creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mutatingWebhookName},
					&admissionregistrationv1.MutatingWebhookConfiguration{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, &admissionregistrationv1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: mutatingWebhookName},
			})

			By("deleting the MutatingWebhookConfiguration")
			Expect(k8sClient.Delete(ctx, &admissionregistrationv1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: mutatingWebhookName},
			})).To(Succeed())

			By("verifying the MutatingWebhookConfiguration is recreated with ownership labels")
			Eventually(func(g Gomega) {
				mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mutatingWebhookName}, mwc)).To(Succeed())
				g.Expect(mwc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates Certificate when deleted", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			certNN := types.NamespacedName{
				Name:      servingCertificateName,
				Namespace: integrationServiceNamespace,
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

		It("recreates Issuer when deleted", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			issuerNN := types.NamespacedName{
				Name:      selfsignedIssuerName,
				Namespace: integrationServiceNamespace,
			}

			By("waiting for initial Issuer creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, issuerNN, &certmanagerv1.Issuer{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Issuer")
			Expect(k8sClient.Delete(ctx, &certmanagerv1.Issuer{
				ObjectMeta: metav1.ObjectMeta{Name: issuerNN.Name, Namespace: issuerNN.Namespace},
			})).To(Succeed())

			By("verifying the Issuer is recreated with ownership labels")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				g.Expect(issuer.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})
})
