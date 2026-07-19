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

package releaseservice

import (
	"context"

	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	releaseServiceSAName          = "release-service-controller-manager"
	metricsServiceName            = "release-service-controller-manager-metrics-service"
	managerConfigMapName          = "release-service-manager-properties"
	leaderElectionRoleName        = "release-service-leader-election-role"
	leaderElectionRoleBindingName = "release-service-leader-election-rolebinding"
	managerClusterRoleName        = "release-service-manager-role"
	managerClusterRoleBindingName = "release-service-manager-rolebinding"
	servingCertificateName        = "serving-cert"
	selfsignedIssuerName          = "selfsigned-issuer"
	validatingWebhookName         = "release-service-validating-webhook-configuration"
	mutatingWebhookName           = "release-service-mutating-webhook-configuration"
	releaseServiceConfigName      = "release-service-config"
)

func newReleaseServiceConfig() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(releaseServiceConfigGVK)
	obj.SetName(releaseServiceConfigName)
	obj.SetNamespace(releaseServiceNamespace)
	return obj
}

// rsClusterScopedChildren returns all cluster-scoped resources that the reconciler creates.
// envtest has no garbage collector, so these must be explicitly cleaned up after each test.
func rsClusterScopedChildren() []client.Object {
	return []client.Object{
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleName}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleBindingName}},
		&admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: validatingWebhookName}},
		&admissionregistrationv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: mutatingWebhookName}},
	}
}

type rsCRDInfo struct {
	name string
	kind string
}

var rsManagedCRDs = []rsCRDInfo{
	{"internalrequests.appstudio.redhat.com", "InternalRequest"},
	{"internalservicesconfigs.appstudio.redhat.com", "InternalServicesConfig"},
	{"releaseplanadmissions.appstudio.redhat.com", "ReleasePlanAdmission"},
	{"releaseplans.appstudio.redhat.com", "ReleasePlan"},
	{"releases.appstudio.redhat.com", "Release"},
	{"releaseserviceconfigs.appstudio.redhat.com", "ReleaseServiceConfig"},
}

func rsCRDEntries() []TableEntry {
	entries := make([]TableEntry, len(rsManagedCRDs))
	for i, c := range rsManagedCRDs {
		entries[i] = Entry(c.kind, c.name, c.kind)
	}
	return entries
}

func rsCRDEntriesNameOnly() []TableEntry {
	entries := make([]TableEntry, len(rsManagedCRDs))
	for i, c := range rsManagedCRDs {
		entries[i] = Entry(c.kind, c.name)
	}
	return entries
}

var _ = Describe("KonfluxReleaseService Controller", func() {
	// startManager starts a per-test manager and registers a DeferCleanup to stop it.
	// Per-test managers avoid races with scrape-token tests that wire TokenCreator.
	startManager := func() {
		mgrCtx, mgrCancel := context.WithCancel(testEnv.Ctx)
		mgr := testutil.NewTestManager(testEnv)
		Expect((&KonfluxReleaseServiceReconciler{
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
			releaseRes := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, releaseRes)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, releaseRes)

			// Wait for the Deployment rather than Ready=True: UpdateComponentStatuses
			// gates Ready=True on ReadyReplicas == Replicas, which never happens in
			// envtest (no kubelet → pods never start).
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      releaseControllerManagerDeploymentName,
					Namespace: releaseServiceNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Self-healing", func() {
		It("recreates Deployment when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			deploymentNN := types.NamespacedName{
				Name:      releaseControllerManagerDeploymentName,
				Namespace: releaseServiceNamespace,
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
				manager := testutil.FindContainer(dep.Spec.Template.Spec.Containers, releaseManagerContainerName)
				g.Expect(manager).NotTo(BeNil(), "manager container should exist")
				g.Expect(manager.Image).NotTo(BeEmpty(), "manager container image should be set")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ServiceAccount when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			saNN := types.NamespacedName{
				Name:      releaseServiceSAName,
				Namespace: releaseServiceNamespace,
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

		It("recreates Service when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			cmNN := types.NamespacedName{
				Name:      managerConfigMapName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			roleNN := types.NamespacedName{
				Name:      leaderElectionRoleName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			rbNN := types.NamespacedName{
				Name:      leaderElectionRoleBindingName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, &rbacv1.ClusterRole{
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
				clusterRole := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, clusterRole)).To(Succeed())
				g.Expect(clusterRole.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ClusterRoleBinding when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, &rbacv1.ClusterRoleBinding{
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

		It("recreates Certificate when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			certNN := types.NamespacedName{
				Name:      servingCertificateName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			issuerNN := types.NamespacedName{
				Name:      selfsignedIssuerName,
				Namespace: releaseServiceNamespace,
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

		It("recreates ValidatingWebhookConfiguration when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr,
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

		It("recreates ReleaseServiceConfig when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, rsClusterScopedChildren()...)

			rscNN := types.NamespacedName{
				Name:      releaseServiceConfigName,
				Namespace: releaseServiceNamespace,
			}

			By("waiting for initial ReleaseServiceConfig creation")
			Eventually(func(g Gomega) {
				rsc := newReleaseServiceConfig()
				g.Expect(k8sClient.Get(ctx, rscNN, rsc)).To(Succeed())
				g.Expect(rsc.GetLabels()).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ReleaseServiceConfig")
			Expect(k8sClient.Delete(ctx, newReleaseServiceConfig())).To(Succeed())

			By("verifying the ReleaseServiceConfig is recreated with ownership labels")
			Eventually(func(g Gomega) {
				rsc := newReleaseServiceConfig()
				g.Expect(k8sClient.Get(ctx, rscNN, rsc)).To(Succeed())
				g.Expect(rsc.GetLabels()).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		DescribeTable("recreates CRD when deleted",
			func(ctx context.Context, crdName string, expectedKind string) {
				cr := &konfluxv1alpha1.KonfluxReleaseService{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

				crdNN := types.NamespacedName{Name: crdName}

				By("waiting for CRD with owner labels")
				var originalUID types.UID
				Eventually(func(g Gomega) {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
					g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
					g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.Release)))
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
					g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.Release)))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			rsCRDEntries(),
		)
	})

	Context("Drift correction", func() {
		It("restores Deployment image when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			deploymentNN := types.NamespacedName{
				Name:      releaseControllerManagerDeploymentName,
				Namespace: releaseServiceNamespace,
			}

			By("waiting for initial Deployment creation")
			var originalImage string
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				manager := testutil.FindContainer(dep.Spec.Template.Spec.Containers, releaseManagerContainerName)
				g.Expect(manager).NotTo(BeNil())
				originalImage = manager.Image
				g.Expect(originalImage).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Deployment image")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				manager := testutil.FindContainer(dep.Spec.Template.Spec.Containers, releaseManagerContainerName)
				g.Expect(manager).NotTo(BeNil())
				manager.Image = "tampered-image:latest"
				g.Expect(k8sClient.Update(ctx, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Deployment image is restored")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				m := testutil.FindContainer(dep.Spec.Template.Spec.Containers, releaseManagerContainerName)
				g.Expect(m).NotTo(BeNil())
				g.Expect(m.Image).To(Equal(originalImage))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ServiceAccount labels when stripped", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			saNN := types.NamespacedName{
				Name:      releaseServiceSAName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			cmNN := types.NamespacedName{
				Name:      managerConfigMapName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			nsNN := types.NamespacedName{Name: releaseServiceNamespace}

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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			roleNN := types.NamespacedName{
				Name:      leaderElectionRoleName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			rbNN := types.NamespacedName{
				Name:      leaderElectionRoleBindingName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: managerClusterRoleName},
			})

			crNN := types.NamespacedName{Name: managerClusterRoleName}

			By("waiting for initial ClusterRole creation")
			var originalRules []rbacv1.PolicyRule
			Eventually(func(g Gomega) {
				clusterRole := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, clusterRole)).To(Succeed())
				g.Expect(clusterRole.Rules).NotTo(BeEmpty())
				originalRules = clusterRole.Rules
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ClusterRole rules")
			Eventually(func(g Gomega) {
				clusterRole := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, clusterRole)).To(Succeed())
				clusterRole.Rules = []rbacv1.PolicyRule{{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"delete"},
				}}
				g.Expect(k8sClient.Update(ctx, clusterRole)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ClusterRole rules are restored")
			Eventually(func(g Gomega) {
				clusterRole := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, clusterRole)).To(Succeed())
				g.Expect(clusterRole.Rules).To(Equal(originalRules))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ClusterRoleBinding subjects when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, &rbacv1.ClusterRoleBinding{
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			certNN := types.NamespacedName{
				Name:      servingCertificateName,
				Namespace: releaseServiceNamespace,
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
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			issuerNN := types.NamespacedName{
				Name:      selfsignedIssuerName,
				Namespace: releaseServiceNamespace,
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

		It("restores Issuer spec when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			issuerNN := types.NamespacedName{
				Name:      selfsignedIssuerName,
				Namespace: releaseServiceNamespace,
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

		It("restores ValidatingWebhookConfiguration labels when stripped", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr,
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

		It("restores ValidatingWebhookConfiguration spec when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr,
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

		It("restores MutatingWebhookConfiguration labels when stripped", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr,
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

		It("restores MutatingWebhookConfiguration spec when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr,
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

		It("restores ReleaseServiceConfig spec when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, rsClusterScopedChildren()...)

			rscNN := types.NamespacedName{
				Name:      releaseServiceConfigName,
				Namespace: releaseServiceNamespace,
			}

			By("waiting for initial ReleaseServiceConfig creation")
			var originalDebug interface{}
			Eventually(func(g Gomega) {
				rsc := newReleaseServiceConfig()
				g.Expect(k8sClient.Get(ctx, rscNN, rsc)).To(Succeed())
				spec, ok := rsc.Object["spec"].(map[string]interface{})
				g.Expect(ok).To(BeTrue())
				originalDebug = spec["debug"]
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ReleaseServiceConfig spec")
			Eventually(func(g Gomega) {
				rsc := newReleaseServiceConfig()
				g.Expect(k8sClient.Get(ctx, rscNN, rsc)).To(Succeed())
				spec := rsc.Object["spec"].(map[string]interface{})
				spec["debug"] = true
				spec["tampered"] = "injected-field"
				g.Expect(k8sClient.Update(ctx, rsc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ReleaseServiceConfig spec is restored")
			Eventually(func(g Gomega) {
				rsc := newReleaseServiceConfig()
				g.Expect(k8sClient.Get(ctx, rscNN, rsc)).To(Succeed())
				spec := rsc.Object["spec"].(map[string]interface{})
				g.Expect(spec["debug"]).To(Equal(originalDebug))
				g.Expect(spec).NotTo(HaveKey("tampered"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		DescribeTable("restores CRD spec when version is disabled",
			func(ctx context.Context, crdName string) {
				cr := &konfluxv1alpha1.KonfluxReleaseService{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

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
			rsCRDEntriesNameOnly(),
		)
	})

	Context("Component metrics gating via Reconcile", Serial, func() {
		serviceMonitorGVK := schema.GroupVersionKind{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "ServiceMonitor",
		}
		serviceMonitorNN := types.NamespacedName{
			Name:      "release-service",
			Namespace: releaseServiceNamespace,
		}
		metricsReaderCRName := "release-service-metrics-reader"
		metricsReaderCRBName := "prometheus-release-service-metrics-reader"
		metricsScraperSAName := "metrics-scraper"

		metricsScrapeChildren := func() []client.Object {
			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(serviceMonitorGVK)
			sm.SetName(serviceMonitorNN.Name)
			sm.SetNamespace(serviceMonitorNN.Namespace)
			return []client.Object{
				sm,
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: metricsScraperSAName, Namespace: releaseServiceNamespace}},
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: kubernetes.ScrapeTokenSecretName, Namespace: releaseServiceNamespace}},
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
			testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})
			for _, child := range metricsScrapeChildren() {
				testutil.DeleteAndWait(ctx, k8sClient, child)
			}
		})

		waitForReconcile := func(ctx context.Context) {
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      releaseControllerManagerDeploymentName,
					Namespace: releaseServiceNamespace,
				}, &appsv1.Deployment{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		}

		It("applies ServiceMonitor and metrics-reader ClusterRole when ComponentMetrics is nil (default enabled)", func(ctx context.Context) {
			releaseService := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, releaseService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, releaseService, metricsScrapeChildren()...)

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
					Name:      releaseControllerManagerDeploymentName,
					Namespace: releaseServiceNamespace,
				}, dep)).To(Succeed())
				g.Expect(dep.Spec.Template.Spec.Containers).NotTo(BeEmpty())
				g.Expect(dep.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--metrics-bind-address=:8443"))
				g.Expect(dep.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--metrics-secure=true"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      metricsServiceName,
					Namespace: releaseServiceNamespace,
				}, svc)).To(Succeed())
				g.Expect(svc.Spec.Ports).NotTo(BeEmpty())
				g.Expect(svc.Spec.Ports[0].Name).To(Equal("https"))
				g.Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8443)))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("skips ServiceMonitor and metrics-reader ClusterRole when ComponentMetrics.Enabled is false", func(ctx context.Context) {
			disabled := false
			releaseService := &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxReleaseServiceSpec{
					ComponentMetrics: &konfluxv1alpha1.ComponentMetricsConfig{
						Enabled: &disabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, releaseService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, releaseService)

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
