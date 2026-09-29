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

	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/common"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/contenthash"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	validatingWebhookName         = "integration-service-validating-webhook-configuration"
	mutatingWebhookName           = "integration-service-mutating-webhook-configuration"
	servingCertificateName        = "serving-cert"
	selfsignedIssuerName          = "selfsigned-issuer"
	metricsServiceName            = "integration-service-controller-manager-metrics-service"
	managerConfigMapName          = "integration-service-manager-config"
	leaderElectionRoleName        = "integration-service-leader-election-role"
	leaderElectionRoleBindingName = "integration-service-leader-election-rolebinding"
	managerClusterRoleName        = "integration-service-manager-role"
	managerClusterRoleBindingName = "integration-service-manager-rolebinding"
)

type isCRDInfo struct {
	name string
	kind string
}

var isManagedCRDs = []isCRDInfo{
	{"componentgroups.appstudio.redhat.com", "ComponentGroup"},
	{"integrationtestscenarios.appstudio.redhat.com", "IntegrationTestScenario"},
	{"nudgeconfigs.appstudio.redhat.com", "NudgeConfig"},
}

func isCRDEntries() []TableEntry {
	entries := make([]TableEntry, len(isManagedCRDs))
	for i, c := range isManagedCRDs {
		entries[i] = Entry(c.kind, c.name, c.kind)
	}
	return entries
}

func isCRDEntriesNameOnly() []TableEntry {
	entries := make([]TableEntry, len(isManagedCRDs))
	for i, c := range isManagedCRDs {
		entries[i] = Entry(c.kind, c.name)
	}
	return entries
}

var _ = Describe("KonfluxIntegrationService Controller", func() {
	// startManager starts a per-test manager and registers a DeferCleanup to stop it.
	// Per-test managers avoid races with scrape-token tests that wire TokenCreator.
	startManager := func() {
		mgrCtx, mgrCancel := context.WithCancel(testEnv.Ctx)
		mgr := testutil.NewTestManager(testEnv)
		Expect((&KonfluxIntegrationServiceReconciler{
			Client:      mgr.GetClient(),
			Scheme:      mgr.GetScheme(),
			ObjectStore: objectStore,
		}).SetupWithManager(mgr)).To(Succeed())
		waitForStop := testutil.StartManagerWithContext(mgrCtx, mgr)
		DeferCleanup(func() {
			mgrCancel()
			waitForStop()
		})
	}

	BeforeEach(func() {
		startManager()
	})

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
				Spec: konfluxv1alpha1.NewKonfluxIntegrationServiceSpec(konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
					PRSnapshotsToKeep:              prToKeep,
					NonPRSnapshotsToKeep:           nonPRToKeep,
					MinSnapshotsToKeepPerComponent: minToKeep,
				}, nil),
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

				container := kubernetes.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
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
				manager := kubernetes.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
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
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService,
				&admissionregistrationv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: validatingWebhookName},
				},
			)

			By("waiting for initial ValidatingWebhookConfiguration creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: validatingWebhookName},
					&admissionregistrationv1.ValidatingWebhookConfiguration{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

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
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService,
				&admissionregistrationv1.MutatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: mutatingWebhookName},
				},
			)

			By("waiting for initial MutatingWebhookConfiguration creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mutatingWebhookName},
					&admissionregistrationv1.MutatingWebhookConfiguration{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

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

		It("recreates CronJob when deleted", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			cjNN := types.NamespacedName{
				Name:      snapshotGCCronJobName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			cmNN := types.NamespacedName{
				Name:      managerConfigMapName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			roleNN := types.NamespacedName{
				Name:      leaderElectionRoleName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			rbNN := types.NamespacedName{
				Name:      leaderElectionRoleBindingName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleName},
			})

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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleBindingName},
			})

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

		DescribeTable("recreates CRD when deleted",
			func(ctx context.Context, crdName string, expectedKind string) {
				integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

				crdNN := types.NamespacedName{Name: crdName}

				By("waiting for CRD with owner labels")
				var originalUID types.UID
				Eventually(func(g Gomega) {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
					g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
					g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.Integration)))
					originalUID = crd.UID
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("deleting the CRD and waiting for it to be gone")
				Expect(k8sClient.Delete(ctx, &apiextensionsv1.CustomResourceDefinition{
					ObjectMeta: metav1.ObjectMeta{Name: crdNN.Name},
				})).To(Succeed())
				Eventually(func(g Gomega) {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					err := k8sClient.Get(ctx, crdNN, crd)
					if err == nil {
						if crd.DeletionTimestamp != nil && len(crd.Finalizers) > 0 {
							crd.Finalizers = nil
							g.Expect(k8sClient.Update(ctx, crd)).To(Succeed())
						}
						g.Expect(crd.UID).NotTo(Equal(originalUID), "old CRD still exists")
						return
					}
					g.Expect(errors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the CRD is recreated with correct spec and labels")
				Eventually(func(g Gomega) {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
					g.Expect(crd.UID).NotTo(Equal(originalUID))
					g.Expect(crd.Spec.Names.Kind).To(Equal(expectedKind))
					g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
					g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.Integration)))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			isCRDEntries(),
		)
	})

	Context("Drift correction", func() {
		It("restores Deployment image when modified", func(ctx context.Context) {
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
			var originalImage string
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				manager := kubernetes.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(manager).NotTo(BeNil())
				originalImage = manager.Image
				g.Expect(originalImage).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Deployment image")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				manager := kubernetes.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(manager).NotTo(BeNil())
				manager.Image = "tampered-image:latest"
				g.Expect(k8sClient.Update(ctx, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Deployment image is restored")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				m := kubernetes.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
				g.Expect(m).NotTo(BeNil())
				g.Expect(m.Image).To(Equal(originalImage))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ServiceAccount labels when stripped", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			saNN := types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			cjNN := types.NamespacedName{
				Name:      snapshotGCCronJobName,
				Namespace: integrationServiceNamespace,
			}

			By("waiting for initial CronJob creation")
			var originalImage string
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				container := kubernetes.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
				g.Expect(container).NotTo(BeNil())
				originalImage = container.Image
				g.Expect(originalImage).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the CronJob image")
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				container := kubernetes.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
				g.Expect(container).NotTo(BeNil())
				container.Image = "tampered-image:latest"
				g.Expect(k8sClient.Update(ctx, cj)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the CronJob image is restored")
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				container := kubernetes.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
				g.Expect(container).NotTo(BeNil())
				g.Expect(container.Image).To(Equal(originalImage))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Service labels when stripped", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			cmNN := types.NamespacedName{
				Name:      managerConfigMapName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			nsNN := types.NamespacedName{Name: integrationServiceNamespace}

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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			roleNN := types.NamespacedName{
				Name:      leaderElectionRoleName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			rbNN := types.NamespacedName{
				Name:      leaderElectionRoleBindingName,
				Namespace: integrationServiceNamespace,
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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleName},
			})

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
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleBindingName},
			})

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

		It("restores Certificate spec when modified", func(ctx context.Context) {
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

		It("restores Issuer labels when stripped", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			issuerNN := types.NamespacedName{
				Name:      selfsignedIssuerName,
				Namespace: integrationServiceNamespace,
			}

			By("waiting for initial Issuer creation with self-signed config")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				g.Expect(issuer.Spec.SelfSigned).NotTo(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Issuer")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				delete(issuer.Labels, constant.KonfluxOwnerLabel)
				delete(issuer.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, issuer)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Issuer labels are restored")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				g.Expect(issuer.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(issuer.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ValidatingWebhookConfiguration labels when stripped", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService,
				&admissionregistrationv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: validatingWebhookName},
				},
			)

			vwcNN := types.NamespacedName{Name: validatingWebhookName}

			By("waiting for initial ValidatingWebhookConfiguration creation with ownership labels")
			Eventually(func(g Gomega) {
				vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, vwcNN, vwc)).To(Succeed())
				g.Expect(vwc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the ValidatingWebhookConfiguration")
			Eventually(func(g Gomega) {
				vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, vwcNN, vwc)).To(Succeed())
				delete(vwc.Labels, constant.KonfluxOwnerLabel)
				delete(vwc.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, vwc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ValidatingWebhookConfiguration labels are restored")
			Eventually(func(g Gomega) {
				vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, vwcNN, vwc)).To(Succeed())
				g.Expect(vwc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(vwc.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores MutatingWebhookConfiguration labels when stripped", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService,
				&admissionregistrationv1.MutatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: mutatingWebhookName},
				},
			)

			mwcNN := types.NamespacedName{Name: mutatingWebhookName}

			By("waiting for initial MutatingWebhookConfiguration creation with ownership labels")
			Eventually(func(g Gomega) {
				mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, mwcNN, mwc)).To(Succeed())
				g.Expect(mwc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the MutatingWebhookConfiguration")
			Eventually(func(g Gomega) {
				mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, mwcNN, mwc)).To(Succeed())
				delete(mwc.Labels, constant.KonfluxOwnerLabel)
				delete(mwc.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, mwc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the MutatingWebhookConfiguration labels are restored")
			Eventually(func(g Gomega) {
				mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, mwcNN, mwc)).To(Succeed())
				g.Expect(mwc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(mwc.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Issuer spec when modified", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

			issuerNN := types.NamespacedName{
				Name:      selfsignedIssuerName,
				Namespace: integrationServiceNamespace,
			}

			By("waiting for initial Issuer creation with self-signed config")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				g.Expect(issuer.Spec.SelfSigned).NotTo(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("replacing selfSigned with a CA spec")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				issuer.Spec.SelfSigned = nil
				issuer.Spec.CA = &certmanagerv1.CAIssuer{SecretName: "tampered-secret"}
				g.Expect(k8sClient.Update(ctx, issuer)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Issuer selfSigned spec is restored")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				g.Expect(issuer.Spec.SelfSigned).NotTo(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ValidatingWebhookConfiguration spec when modified", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService,
				&admissionregistrationv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: validatingWebhookName},
				},
			)

			vwcNN := types.NamespacedName{Name: validatingWebhookName}

			By("waiting for initial ValidatingWebhookConfiguration creation")
			var originalPath string
			Eventually(func(g Gomega) {
				vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, vwcNN, vwc)).To(Succeed())
				g.Expect(vwc.Webhooks).NotTo(BeEmpty())
				g.Expect(vwc.Webhooks[0].ClientConfig.Service).NotTo(BeNil())
				originalPath = *vwc.Webhooks[0].ClientConfig.Service.Path
				g.Expect(originalPath).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the webhook path")
			Eventually(func(g Gomega) {
				vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, vwcNN, vwc)).To(Succeed())
				tampered := "/tampered-path"
				vwc.Webhooks[0].ClientConfig.Service.Path = &tampered
				g.Expect(k8sClient.Update(ctx, vwc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the webhook path is restored")
			Eventually(func(g Gomega) {
				vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, vwcNN, vwc)).To(Succeed())
				g.Expect(vwc.Webhooks).NotTo(BeEmpty())
				g.Expect(vwc.Webhooks[0].ClientConfig.Service).NotTo(BeNil())
				g.Expect(*vwc.Webhooks[0].ClientConfig.Service.Path).To(Equal(originalPath))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores MutatingWebhookConfiguration spec when modified", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService,
				&admissionregistrationv1.MutatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{Name: mutatingWebhookName},
				},
			)

			mwcNN := types.NamespacedName{Name: mutatingWebhookName}

			By("waiting for initial MutatingWebhookConfiguration creation")
			var originalPath string
			Eventually(func(g Gomega) {
				mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, mwcNN, mwc)).To(Succeed())
				g.Expect(mwc.Webhooks).NotTo(BeEmpty())
				g.Expect(mwc.Webhooks[0].ClientConfig.Service).NotTo(BeNil())
				originalPath = *mwc.Webhooks[0].ClientConfig.Service.Path
				g.Expect(originalPath).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the webhook path")
			Eventually(func(g Gomega) {
				mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, mwcNN, mwc)).To(Succeed())
				tampered := "/tampered-path"
				mwc.Webhooks[0].ClientConfig.Service.Path = &tampered
				g.Expect(k8sClient.Update(ctx, mwc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the webhook path is restored")
			Eventually(func(g Gomega) {
				mwc := &admissionregistrationv1.MutatingWebhookConfiguration{}
				g.Expect(k8sClient.Get(ctx, mwcNN, mwc)).To(Succeed())
				g.Expect(mwc.Webhooks).NotTo(BeEmpty())
				g.Expect(mwc.Webhooks[0].ClientConfig.Service).NotTo(BeNil())
				g.Expect(*mwc.Webhooks[0].ClientConfig.Service.Path).To(Equal(originalPath))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		DescribeTable("restores CRD spec when version is disabled",
			func(ctx context.Context, crdName string) {
				integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, integrationService)

				crdNN := types.NamespacedName{Name: crdName}

				By("waiting for CRD creation with served=true")
				Eventually(func(g Gomega) {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
					g.Expect(crd.Spec.Versions).NotTo(BeEmpty())
					g.Expect(crd.Spec.Versions[0].Served).To(BeTrue())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("disabling the served version")
				var afterTamperRV string
				Eventually(func(g Gomega) {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
					crd.Spec.Versions[0].Served = false
					g.Expect(k8sClient.Update(ctx, crd)).To(Succeed())
					afterTamperRV = crd.ResourceVersion
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying SSA restores served=true")
				Eventually(func(g Gomega) {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
					g.Expect(crd.ResourceVersion).NotTo(Equal(afterTamperRV), "controller has not reconciled yet")
					g.Expect(crd.Spec.Versions[0].Served).To(BeTrue())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			isCRDEntriesNameOnly(),
		)
	})

	Context("Component metrics gating via Reconcile", Serial, func() {
		serviceMonitorGVK := schema.GroupVersionKind{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "ServiceMonitor",
		}
		serviceMonitorNN := types.NamespacedName{
			Name:      "integration-service",
			Namespace: integrationServiceNamespace,
		}
		metricsReaderCRName := "integration-service-metrics-reader"
		metricsReaderCRBName := "prometheus-integration-service-metrics-reader"
		metricsScraperSAName := "metrics-scraper"

		metricsScrapeChildren := func() []client.Object {
			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(serviceMonitorGVK)
			sm.SetName(serviceMonitorNN.Name)
			sm.SetNamespace(serviceMonitorNN.Namespace)
			return []client.Object{
				sm,
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: metricsScraperSAName, Namespace: integrationServiceNamespace}},
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: kubernetes.ScrapeTokenSecretName, Namespace: integrationServiceNamespace}},
				&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: metricsReaderCRName}},
				&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: metricsReaderCRBName}},
			}
		}

		getServiceMonitor := func(ctx context.Context) (*unstructured.Unstructured, error) {
			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(serviceMonitorGVK)
			err := k8sClient.Get(ctx, serviceMonitorNN, sm)
			return sm, err
		}

		BeforeEach(func(ctx context.Context) {
			testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})
			for _, child := range metricsScrapeChildren() {
				testutil.DeleteAndWait(ctx, k8sClient, child)
			}
		})

		waitForReconcile := func(ctx context.Context) {
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      controllerManagerDeploymentName,
					Namespace: integrationServiceNamespace,
				}, &appsv1.Deployment{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		}

		It("applies ServiceMonitor and metrics-reader ClusterRole when ComponentMetrics is nil (default enabled)", func(ctx context.Context) {
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService, metricsScrapeChildren()...)

			Eventually(func(g Gomega) {
				_, err := getServiceMonitor(ctx)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: metricsReaderCRName}, cr)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      controllerManagerDeploymentName,
					Namespace: integrationServiceNamespace,
				}, dep)).To(Succeed())
				g.Expect(dep.Spec.Template.Spec.Containers).NotTo(BeEmpty())
				g.Expect(dep.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--metrics-bind-address=:8443"))
				g.Expect(dep.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--metrics-secure=true"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      metricsServiceName,
					Namespace: integrationServiceNamespace,
				}, svc)).To(Succeed())
				g.Expect(svc.Spec.Ports).NotTo(BeEmpty())
				g.Expect(svc.Spec.Ports[0].Name).To(Equal("https"))
				g.Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8443)))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("skips ServiceMonitor and metrics-reader ClusterRole when ComponentMetrics.Enabled is false", func(ctx context.Context) {
			disabled := false
			integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxIntegrationServiceSpec{
					ComponentMetrics: &konfluxv1alpha1.ComponentMetricsConfig{
						Enabled: &disabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService)

			waitForReconcile(ctx)

			// Wait for orphan cleanup from the disabled reconcile (and any stale
			// enabled reconcile racing the CR recreate) before Consistently.
			Eventually(func(g Gomega) {
				_, err := getServiceMonitor(ctx)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: metricsReaderCRName}, cr)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Consistently(func(g Gomega) {
				_, err := getServiceMonitor(ctx)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(3 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

			Consistently(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: metricsReaderCRName}, cr)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(3 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})
	})
})

var _ = Describe("KonfluxIntegrationService Controller - OpenShift trusted-ca", func() {
	startManagerWithClusterInfo := func(clusterInfo *clusterinfo.Info) {
		mgrCtx, mgrCancel := context.WithCancel(testEnv.Ctx)
		mgr := testutil.NewTestManager(testEnv)
		Expect((&KonfluxIntegrationServiceReconciler{
			Client:      mgr.GetClient(),
			Scheme:      mgr.GetScheme(),
			ObjectStore: objectStore,
			ClusterInfo: clusterInfo,
		}).SetupWithManager(mgr)).To(Succeed())
		waitForStop := testutil.StartManagerWithContext(mgrCtx, mgr)
		DeferCleanup(func() {
			mgrCancel()
			waitForStop()
		})
	}

	detectOpenShiftClusterInfo := func() *clusterinfo.Info {
		info, err := clusterinfo.DetectWithClient(&integrationServiceMockDiscoveryClient{
			resources: map[string]*metav1.APIResourceList{
				"config.openshift.io/v1": {
					APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
				},
			},
			serverVersion: &version.Info{GitVersion: "v1.29.0"},
		})
		Expect(err).NotTo(HaveOccurred())
		return info
	}

	trustedCAHasInjectionLabel := func(g Gomega) {
		cm := &corev1.ConfigMap{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      common.TrustedCAConfigMapName,
			Namespace: integrationServiceNamespace,
		}, cm)).To(Succeed())
		g.Expect(cm.Labels).To(HaveKeyWithValue(
			common.OpenShiftInjectTrustedCABundleLabel, "true"))
	}

	trustedCAIsGone := func(g Gomega) {
		cm := &corev1.ConfigMap{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      common.TrustedCAConfigMapName,
			Namespace: integrationServiceNamespace,
		}, cm)
		g.Expect(errors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
	}

	expectVolumeConfigMapName := func(name string) {
		Eventually(func(g Gomega) {
			dep := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: integrationServiceNamespace,
			}, dep)).To(Succeed())
			vol := kubernetes.FindVolume(dep.Spec.Template.Spec.Volumes, common.TrustedCAVolumeName)
			g.Expect(vol).NotTo(BeNil())
			g.Expect(vol.ConfigMap).NotTo(BeNil())
			g.Expect(vol.ConfigMap.Name).To(Equal(name))
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
	}

	expectManagerConfigExists := func() {
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name:      managerConfigMapName,
			Namespace: integrationServiceNamespace,
		}, &corev1.ConfigMap{})).To(Succeed())
	}

	// contentHash nil means the pod template must not carry the annotation.
	expectTrustedCAMount := func(configMapName, key string, optional bool, contentHash *string) {
		Eventually(func(g Gomega) {
			dep := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: integrationServiceNamespace,
			}, dep)).To(Succeed())
			vol := kubernetes.FindVolume(dep.Spec.Template.Spec.Volumes, common.TrustedCAVolumeName)
			g.Expect(vol).NotTo(BeNil())
			g.Expect(vol.ConfigMap).NotTo(BeNil())
			g.Expect(vol.ConfigMap.Name).To(Equal(configMapName))
			g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
			g.Expect(*vol.ConfigMap.Optional).To(Equal(optional))
			g.Expect(vol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
				Key:  key,
				Path: common.TrustedCADefaultFileVolumePath,
			}))
			if contentHash == nil {
				g.Expect(dep.Spec.Template.Annotations).NotTo(HaveKey(common.TrustedCAHashAnnotation))
				return
			}
			g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(common.TrustedCAHashAnnotation, *contentHash))
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
	}

	setConfigMapKey := func(name, key, value string) {
		Eventually(func(g Gomega) {
			got := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: integrationServiceNamespace,
			}, got)).To(Succeed())
			if got.Data == nil {
				got.Data = map[string]string{}
			}
			got.Data[key] = value
			g.Expect(k8sClient.Update(ctx, got)).To(Succeed())
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
	}

	Context("OpenShift trusted-ca ConfigMap", func() {
		var integrationService *konfluxv1alpha1.KonfluxIntegrationService

		trustedCAExists := func() bool {
			cm := &corev1.ConfigMap{}
			return k8sClient.Get(ctx, types.NamespacedName{
				Name:      common.TrustedCAConfigMapName,
				Namespace: integrationServiceNamespace,
			}, cm) == nil
		}

		BeforeEach(func() {
			integrationService = &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
		})

		AfterEach(func() {
			testutil.DeleteAndWait(ctx, k8sClient, integrationService)
			testutil.DeleteAndWait(ctx, k8sClient, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: common.TrustedCAConfigMapName, Namespace: integrationServiceNamespace},
			})
		})

		It("Should create trusted-ca ConfigMap with injection label when running on OpenShift", func() {
			startManagerWithClusterInfo(detectOpenShiftClusterInfo())

			By("verifying the trusted-ca ConfigMap was created with the injection label")
			Eventually(trustedCAHasInjectionLabel).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should stamp the platform bundle hash on startup when trustedCA is omitted", func() {
			const platformPEM = "-----BEGIN CERTIFICATE-----\nplatform-startup\n-----END CERTIFICATE-----\n"

			startManagerWithClusterInfo(detectOpenShiftClusterInfo())

			By("mounting trusted-ca with no checksum until ca-bundle.crt exists")
			Eventually(trustedCAHasInjectionLabel).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			expectTrustedCAMount(common.TrustedCAConfigMapName, common.TrustedCADefaultFileVolumePath, true, nil)

			By("stamping the checksum once the cluster bundle is written")
			setConfigMapKey(common.TrustedCAConfigMapName, common.TrustedCADefaultFileVolumePath, platformPEM)
			platformHash := contenthash.String(platformPEM)
			expectTrustedCAMount(common.TrustedCAConfigMapName, common.TrustedCADefaultFileVolumePath, true, &platformHash)
		})

		It("Should NOT create trusted-ca ConfigMap when NOT running on OpenShift", func() {
			defaultClusterInfo, err := clusterinfo.DetectWithClient(&integrationServiceMockDiscoveryClient{
				resources:     map[string]*metav1.APIResourceList{},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())

			startManagerWithClusterInfo(defaultClusterInfo)

			By("waiting for the controller to apply manifests and create the Deployment")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      controllerManagerDeploymentName,
					Namespace: integrationServiceNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying no trusted-ca ConfigMap was created")
			Expect(trustedCAExists()).To(BeFalse())
		})

		It("Should NOT create trusted-ca ConfigMap when ClusterInfo is nil", func() {
			startManagerWithClusterInfo(nil)

			By("waiting for the controller to apply manifests and create the Deployment")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      controllerManagerDeploymentName,
					Namespace: integrationServiceNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying no trusted-ca ConfigMap was created")
			Expect(trustedCAExists()).To(BeFalse())
		})

		It("Should NOT create trusted-ca ConfigMap when spec.trustedCA is set", func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: integrationServiceNamespace}}
			err := k8sClient.Create(ctx, ns)
			Expect(err == nil || errors.IsAlreadyExists(err)).To(BeTrue(), "unexpected error: %v", err)

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			startManagerWithClusterInfo(detectOpenShiftClusterInfo())

			By("waiting for the first apply to mount the user ConfigMap and stamp its checksum")
			userHash := contenthash.String("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n")
			expectTrustedCAMount("custom-ca-bundle", "tls.pem", false, &userHash)

			By("verifying the operator did not create the platform-injected trusted-ca ConfigMap")
			Consistently(trustedCAIsGone).WithTimeout(3 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("Should mount spec.trustedCA when its name is trusted-ca without creating the platform-injected ConfigMap", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: common.TrustedCAConfigMapName, Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\nuser\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: common.TrustedCAConfigMapName,
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			startManagerWithClusterInfo(detectOpenShiftClusterInfo())

			By("waiting for the controller to mount the user-supplied ConfigMap named trusted-ca")
			expectVolumeConfigMapName(common.TrustedCAConfigMapName)

			By("verifying the operator did not replace the user-supplied trusted-ca with the platform-injected object")
			Consistently(func(g Gomega) {
				got := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      common.TrustedCAConfigMapName,
					Namespace: integrationServiceNamespace,
				}, got)).To(Succeed())
				g.Expect(got.Data).To(HaveKeyWithValue("tls.pem", "-----BEGIN CERTIFICATE-----\nuser\n-----END CERTIFICATE-----\n"))
				g.Expect(got.Labels).NotTo(HaveKey(common.OpenShiftInjectTrustedCABundleLabel))
			}).WithTimeout(3 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("Should delete leftover operator-owned trusted-ca when spec.trustedCA points at another ConfigMap", func() {
			startManagerWithClusterInfo(detectOpenShiftClusterInfo())
			Eventually(trustedCAHasInjectionLabel).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			By("waiting for the leftover platform-injected ConfigMap to be removed")
			Eventually(trustedCAIsGone).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			expectManagerConfigExists()
			expectVolumeConfigMapName("custom-ca-bundle")
		})

		It("Should replace the platform bundle hash when trustedCA points at another ConfigMap", func() {
			const platformPEM = "-----BEGIN CERTIFICATE-----\nplatform\n-----END CERTIFICATE-----\n"
			const userPEM = "-----BEGIN CERTIFICATE-----\nuser-other\n-----END CERTIFICATE-----\n"

			startManagerWithClusterInfo(detectOpenShiftClusterInfo())
			Eventually(trustedCAHasInjectionLabel).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stamping the platform checksum before the field is set")
			setConfigMapKey(common.TrustedCAConfigMapName, common.TrustedCADefaultFileVolumePath, platformPEM)
			platformHash := contenthash.String(platformPEM)
			expectTrustedCAMount(common.TrustedCAConfigMapName, common.TrustedCADefaultFileVolumePath, true, &platformHash)

			userCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": userPEM},
			}
			Expect(k8sClient.Create(ctx, userCM)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, userCM)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			By("mounting the user ConfigMap and replacing the platform checksum")
			userHash := contenthash.String(userPEM)
			expectTrustedCAMount("custom-ca-bundle", "tls.pem", false, &userHash)
			Eventually(trustedCAIsGone).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should delete leftover operator-owned trusted-ca when spec.trustedCA.name is trusted-ca", func() {
			const platformPEM = "-----BEGIN CERTIFICATE-----\nplatform\n-----END CERTIFICATE-----\n"
			const userPEM = "-----BEGIN CERTIFICATE-----\nuser\n-----END CERTIFICATE-----\n"

			startManagerWithClusterInfo(detectOpenShiftClusterInfo())
			Eventually(trustedCAHasInjectionLabel).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stamping the platform checksum before the name is reused")
			setConfigMapKey(common.TrustedCAConfigMapName, common.TrustedCADefaultFileVolumePath, platformPEM)
			platformHash := contenthash.String(platformPEM)
			expectTrustedCAMount(common.TrustedCAConfigMapName, common.TrustedCADefaultFileVolumePath, true, &platformHash)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: common.TrustedCAConfigMapName,
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			By("waiting for the leftover operator-owned ConfigMap to be removed so the name can be reused")
			Eventually(trustedCAIsGone).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			expectManagerConfigExists()

			By("keeping the platform checksum while the user key is still missing")
			expectTrustedCAMount(common.TrustedCAConfigMapName, "tls.pem", false, &platformHash)

			userCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: common.TrustedCAConfigMapName, Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": userPEM},
			}
			Expect(k8sClient.Create(ctx, userCM)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, userCM)

			By("stamping the user checksum once the reused ConfigMap provides tls.pem")
			userHash := contenthash.String(userPEM)
			expectTrustedCAMount(common.TrustedCAConfigMapName, "tls.pem", false, &userHash)
			Eventually(func(g Gomega) {
				got := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      common.TrustedCAConfigMapName,
					Namespace: integrationServiceNamespace,
				}, got)).To(Succeed())
				g.Expect(got.Data).To(HaveKeyWithValue("tls.pem", userPEM))
				g.Expect(got.Labels).NotTo(HaveKey(common.OpenShiftInjectTrustedCABundleLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should create trusted-ca ConfigMap after spec.trustedCA is cleared", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			startManagerWithClusterInfo(detectOpenShiftClusterInfo())

			expectVolumeConfigMapName("custom-ca-bundle")
			Expect(trustedCAExists()).To(BeFalse())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = nil
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			By("verifying the platform-injected trusted-ca ConfigMap is created and mounted after trustedCA is cleared")
			Eventually(trustedCAHasInjectionLabel).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			expectVolumeConfigMapName(common.TrustedCAConfigMapName)
		})

		It("Should stamp the pod-template hash when the platform trusted-ca bundle is populated", func() {
			const originalPEM = "-----BEGIN CERTIFICATE-----\nplatform\n-----END CERTIFICATE-----\n"
			const rotatedPEM = "-----BEGIN CERTIFICATE-----\nplatform-rotated\n-----END CERTIFICATE-----\n"

			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: integrationServiceNamespace}}
			err := k8sClient.Create(ctx, ns)
			Expect(err == nil || errors.IsAlreadyExists(err)).To(BeTrue(), "unexpected error: %v", err)

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\nuser\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			startManagerWithClusterInfo(detectOpenShiftClusterInfo())
			expectVolumeConfigMapName("custom-ca-bundle")

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = nil
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			By("verifying the restored mount has no hash until ca-bundle.crt is written")
			Eventually(func(g Gomega) {
				trustedCAHasInjectionLabel(g)
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      controllerManagerDeploymentName,
					Namespace: integrationServiceNamespace,
				}, dep)).To(Succeed())
				g.Expect(dep.Spec.Template.Annotations).NotTo(HaveKey(common.TrustedCAHashAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			setPlatformBundle := func(pem string) {
				Eventually(func(g Gomega) {
					got := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name:      common.TrustedCAConfigMapName,
						Namespace: integrationServiceNamespace,
					}, got)).To(Succeed())
					if got.Data == nil {
						got.Data = map[string]string{}
					}
					got.Data[common.TrustedCADefaultFileVolumePath] = pem
					g.Expect(k8sClient.Update(ctx, got)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			}

			expectPlatformBundleHash := func(pem string) {
				Eventually(func(g Gomega) {
					dep := &appsv1.Deployment{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name:      controllerManagerDeploymentName,
						Namespace: integrationServiceNamespace,
					}, dep)).To(Succeed())
					vol := kubernetes.FindVolume(dep.Spec.Template.Spec.Volumes, common.TrustedCAVolumeName)
					g.Expect(vol).NotTo(BeNil())
					g.Expect(vol.ConfigMap).NotTo(BeNil())
					g.Expect(vol.ConfigMap.Name).To(Equal(common.TrustedCAConfigMapName))
					g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
					g.Expect(*vol.ConfigMap.Optional).To(BeTrue())
					g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(
						common.TrustedCAHashAnnotation, contenthash.String(pem)))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			}

			By("writing ca-bundle.crt the way the cluster network operator would")
			setPlatformBundle(originalPEM)
			expectPlatformBundleHash(originalPEM)

			By("updating the bundle again")
			setPlatformBundle(rotatedPEM)
			expectPlatformBundleHash(rotatedPEM)
		})
	})

	Context("trustedCA volume mount", func() {
		var integrationService *konfluxv1alpha1.KonfluxIntegrationService

		getDeployment := func(g Gomega) *appsv1.Deployment {
			dep := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      controllerManagerDeploymentName,
				Namespace: integrationServiceNamespace,
			}, dep)).To(Succeed())
			return dep
		}

		findTrustedCAMount := func(dep *appsv1.Deployment) *corev1.VolumeMount {
			manager := kubernetes.FindContainer(dep.Spec.Template.Spec.Containers, managerContainerName)
			if manager == nil {
				return nil
			}
			return kubernetes.FindVolumeMount(manager.VolumeMounts, common.TrustedCAVolumeName)
		}

		findTrustedCAVolume := func(dep *appsv1.Deployment) *corev1.Volume {
			return kubernetes.FindVolume(dep.Spec.Template.Spec.Volumes, common.TrustedCAVolumeName)
		}

		BeforeEach(func() {
			startManagerWithClusterInfo(nil)
			integrationService = &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, integrationService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, integrationService,
				&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleName}},
				&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleBindingName}},
			)
			// envtest does not GC the leaked controller-manager Deployment.
			// Wait until this CR's reconcile has restored the default extra-file
			// mount so a previous spec's hash is not copied onto a never-stamped apply.
			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal(common.TrustedCAConfigMapName))
				g.Expect(dep.Spec.Template.Annotations).NotTo(HaveKey(common.TrustedCAHashAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should keep the extra-file mount when trustedCA is omitted", func() {
			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				mount := findTrustedCAMount(dep)
				g.Expect(mount).NotTo(BeNil())
				g.Expect(mount.MountPath).To(Equal(common.TrustedCADefaultFileMountPath))
				g.Expect(mount.SubPath).To(Equal(common.TrustedCADefaultFileVolumePath))

				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal(common.TrustedCAConfigMapName))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeTrue())
				g.Expect(vol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
					Key:  common.TrustedCADefaultFileVolumePath,
					Path: common.TrustedCADefaultFileVolumePath,
				}))
				g.Expect(dep.Spec.Template.Annotations).NotTo(HaveKey(common.TrustedCAHashAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should retarget the trusted-ca volume to the specified ConfigMap when trustedCA is set", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				mount := findTrustedCAMount(dep)
				g.Expect(mount).NotTo(BeNil())
				g.Expect(mount.MountPath).To(Equal(common.TrustedCADefaultFileMountPath))
				g.Expect(mount.SubPath).To(Equal(common.TrustedCADefaultFileVolumePath))

				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal("custom-ca-bundle"))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeFalse())
				g.Expect(vol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
					Key:  "tls.pem",
					Path: common.TrustedCADefaultFileVolumePath,
				}))
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(
					common.TrustedCAHashAnnotation,
					contenthash.String("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"),
				))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should restore the default ConfigMap reference when trustedCA is cleared", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal("custom-ca-bundle"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = nil
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				mount := findTrustedCAMount(dep)
				g.Expect(mount).NotTo(BeNil())
				g.Expect(mount.MountPath).To(Equal(common.TrustedCADefaultFileMountPath))
				g.Expect(mount.SubPath).To(Equal(common.TrustedCADefaultFileVolumePath))

				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal(common.TrustedCAConfigMapName))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeTrue())
				g.Expect(vol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
					Key:  common.TrustedCADefaultFileVolumePath,
					Path: common.TrustedCADefaultFileVolumePath,
				}))
				g.Expect(dep.Spec.Template.Annotations).NotTo(HaveKey(common.TrustedCAHashAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should update the pod-template hash when the trustedCA ConfigMap data changes", func() {
			const originalPEM = "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"
			const rotatedPEM = "-----BEGIN CERTIFICATE-----\nrotated\n-----END CERTIFICATE-----\n"

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": originalPEM},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(
					common.TrustedCAHashAnnotation, contenthash.String(originalPEM)))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, cm)).To(Succeed())
			cm.Data["tls.pem"] = rotatedPEM
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(
					common.TrustedCAHashAnnotation, contenthash.String(rotatedPEM)))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should still reconcile when the trustedCA ConfigMap is missing", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal("custom-ca-bundle"))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeFalse())
				g.Expect(dep.Spec.Template.Annotations).NotTo(HaveKey(common.TrustedCAHashAnnotation))

				current := &konfluxv1alpha1.KonfluxIntegrationService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, current)).To(Succeed())
				g.Expect(current.Status.Conditions).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should keep the pod-template hash when the trustedCA ConfigMap is deleted after a successful mount", func() {
			const pem = "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": pem},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			wantHash := contenthash.String(pem)
			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(common.TrustedCAHashAnnotation, wantHash))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, &corev1.ConfigMap{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Consistently(func(g Gomega) {
				dep := getDeployment(g)
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(common.TrustedCAHashAnnotation, wantHash))
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal("custom-ca-bundle"))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeFalse())
			}, 5*time.Second, time.Second).Should(Succeed())
		})

		It("should retarget from one ConfigMap to another when trustedCA name changes", func() {
			cmA := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle-a", Namespace: integrationServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\na\n-----END CERTIFICATE-----\n"},
			}
			cmB := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle-b", Namespace: integrationServiceNamespace},
				Data:       map[string]string{"ca.pem": "-----BEGIN CERTIFICATE-----\nb\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cmA)).To(Succeed())
			Expect(k8sClient.Create(ctx, cmB)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cmA)
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cmB)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: cmA.Name,
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal(cmA.Name))
				g.Expect(vol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
					Key:  "tls.pem",
					Path: common.TrustedCADefaultFileVolumePath,
				}))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, integrationService)).To(Succeed())
			integrationService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: cmB.Name,
				Key:  "ca.pem",
			}
			Expect(k8sClient.Update(ctx, integrationService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal(cmB.Name))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeFalse())
				g.Expect(vol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
					Key:  "ca.pem",
					Path: common.TrustedCADefaultFileVolumePath,
				}))
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(
					common.TrustedCAHashAnnotation,
					contenthash.String(cmB.Data["ca.pem"]),
				))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("trustedCA CRD validation", func() {
		newCR := func(name, key string) *konfluxv1alpha1.KonfluxIntegrationService {
			return &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxIntegrationServiceSpec(
					konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
						TrustedCA: &konfluxv1alpha1.TrustedCAConfigMap{
							Name: name,
							Key:  key,
						},
					},
					testutil.DefaultComponentMetricsConfig(),
				),
			}
		}

		expectInvalidField := func(err error, field string) {
			GinkgoHelper()
			Expect(errors.IsInvalid(err)).To(BeTrue(), "unexpected error: %v", err)
			statusErr, ok := err.(*errors.StatusError)
			Expect(ok).To(BeTrue())
			Expect(statusErr.Status().Details).NotTo(BeNil())
			Expect(statusErr.Status().Details.Causes).NotTo(BeEmpty())
			Expect(statusErr.Status().Details.Causes[0].Field).To(Equal(field))
		}

		// The TrustedCAConfigMap type is shared between build-service and
		// integration-service. Full CRD boundary-value tests (key/name length
		// limits, pattern rejections, empty values) are in the build-service
		// test suite. Here we validate the happy path and the CEL rule
		// that rejects "." / ".." keys.

		It("should accept a valid trustedCA", func() {
			cr := newCR("custom-ca-bundle", "tls.pem")
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)
		})

		It("should reject trustedCA keys that are '.' or '..'", func() {
			for _, key := range []string{".", ".."} {
				err := k8sClient.Create(ctx, newCR("custom-ca-bundle", key))
				expectInvalidField(err, "spec.trustedCA.key")
			}
		})
	})
})

// integrationServiceMockDiscoveryClient implements clusterinfo.DiscoveryClient for testing.
type integrationServiceMockDiscoveryClient struct {
	resources     map[string]*metav1.APIResourceList
	serverVersion *version.Info
}

func (m *integrationServiceMockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if r, ok := m.resources[groupVersion]; ok {
		return r, nil
	}
	return nil, errors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *integrationServiceMockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return m.serverVersion, nil
}
