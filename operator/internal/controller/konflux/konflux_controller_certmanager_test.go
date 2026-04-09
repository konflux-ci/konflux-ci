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

package konflux

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

var _ = Describe("Konflux Controller - Cert-Manager Dependency", func() {
	// startManager creates a per-test manager with the given ClusterInfo
	// and registers a DeferCleanup to cancel it after the test.
	startManager := func(clusterInfo *clusterinfo.Info) {
		mgr := testutil.NewTestManager(testEnv)
		Expect((&KonfluxReconciler{
			Client:      mgr.GetClient(),
			Scheme:      mgr.GetScheme(),
			ClusterInfo: clusterInfo,
		}).SetupWithManager(mgr)).To(Succeed())
		mgrCtx, cancel := context.WithCancel(testEnv.Ctx)
		DeferCleanup(cancel)
		testutil.StartManagerWithContext(mgrCtx, mgr)
	}

	Context("When cert-manager CRDs are not installed", func() {
		var clusterInfo *clusterinfo.Info

		BeforeEach(func() {
			var err error
			clusterInfo, err = clusterinfo.DetectWithClient(&certManagerMockDiscoveryClient{
				hasCertManager: false,
			})
			Expect(err).NotTo(HaveOccurred())

			By("pre-cleaning any existing Konflux CR")
			_ = k8sClient.Delete(ctx, &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})
		})

		It("should set CertManagerAvailable and Ready conditions to False when cert-manager is missing", func(ctx context.Context) {
			startManager(clusterInfo)

			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())

				cond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeCertManagerAvailable)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(condition.ReasonCertManagerNotInstalled))
				g.Expect(cond.Message).To(ContainSubstring("cert-manager CRDs are not installed"))

				readyCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(readyCond.Reason).To(Equal(condition.ReasonCertManagerNotInstalled))
				g.Expect(readyCond.Message).To(ContainSubstring("cert-manager CRDs are not installed"))
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})
	})

	Context("When cert-manager CRDs are installed", func() {
		var clusterInfo *clusterinfo.Info

		BeforeEach(func() {
			var err error
			clusterInfo, err = clusterinfo.DetectWithClient(&certManagerMockDiscoveryClient{
				hasCertManager: true,
			})
			Expect(err).NotTo(HaveOccurred())

			By("pre-cleaning any existing Konflux CR")
			_ = k8sClient.Delete(ctx, &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})
		})

		It("should set CertManagerAvailable condition to True", func(ctx context.Context) {
			startManager(clusterInfo)

			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())

				cond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeCertManagerAvailable)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal("CertManagerInstalled"))
				g.Expect(cond.Message).To(ContainSubstring("cert-manager CRDs are installed"))
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})

		It("should not override Ready condition when cert-manager is available", func(ctx context.Context) {
			startManager(clusterInfo)

			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			// Wait for CertManagerAvailable=True to confirm reconcile ran.
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())

				cond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeCertManagerAvailable)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))

				readyCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)
				if readyCond != nil {
					g.Expect(readyCond.Reason).NotTo(Equal(condition.ReasonCertManagerNotInstalled))
				}
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})
	})

	Context("When cert-manager check fails with an error", func() {
		var clusterInfo *clusterinfo.Info

		BeforeEach(func() {
			var err error
			clusterInfo, err = clusterinfo.DetectWithClient(&certManagerMockDiscoveryClient{
				hasCertManager: false,
				returnError:    true,
			})
			Expect(err).NotTo(HaveOccurred())

			By("pre-cleaning any existing Konflux CR")
			_ = k8sClient.Delete(ctx, &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})
		})

		It("should continue reconciliation and set CertManagerAvailable to Unknown", func(ctx context.Context) {
			startManager(clusterInfo)

			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())

				cond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeCertManagerAvailable)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
				g.Expect(cond.Reason).To(Equal(condition.ReasonCertManagerInstallationCheckFailed))
				g.Expect(cond.Message).To(ContainSubstring("simulated RBAC or network error"))
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})

		It("should allow Ready to be True when CertManagerAvailable is Unknown", func(ctx context.Context) {
			startManager(clusterInfo)

			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			// Wait for CertManagerAvailable=Unknown to confirm reconcile ran.
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())

				certManagerCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeCertManagerAvailable)
				g.Expect(certManagerCond).NotTo(BeNil())
				g.Expect(certManagerCond.Status).To(Equal(metav1.ConditionUnknown))

				readyCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)
				if readyCond != nil {
					g.Expect(readyCond.Reason).NotTo(Equal(condition.ReasonCertManagerNotInstalled))
				}
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})
	})
})

// certManagerMockDiscoveryClient implements clusterinfo.DiscoveryClient for testing cert-manager scenarios
type certManagerMockDiscoveryClient struct {
	hasCertManager bool
	returnError    bool
}

func (m *certManagerMockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if m.returnError && groupVersion == "cert-manager.io/v1" {
		return nil, apierrors.NewInternalError(errors.New("simulated RBAC or network error"))
	}

	if groupVersion == "cert-manager.io/v1" && m.hasCertManager {
		return &metav1.APIResourceList{
			GroupVersion: "cert-manager.io/v1",
			APIResources: []metav1.APIResource{
				{Kind: "Certificate"},
				{Kind: "Issuer"},
				{Kind: "ClusterIssuer"},
			},
		}, nil
	}

	if groupVersion == "config.openshift.io/v1" {
		// Return empty to indicate not OpenShift
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
	}

	return nil, apierrors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *certManagerMockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return &version.Info{GitVersion: "v1.30.0"}, nil
}
