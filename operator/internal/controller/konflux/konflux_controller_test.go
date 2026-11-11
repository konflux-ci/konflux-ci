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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	prometheustestutil "github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/applicationapi"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/buildservice"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/certmanager"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/cli"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/defaulttenant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/enterprisecontract"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/imagecontroller"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/info"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/integrationservice"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/internalregistry"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/namespacelister"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/rbac"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/releaseservice"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/segmentbridge"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	uictrl "github.com/konflux-ci/konflux-ci/operator/internal/controller/ui"
	"github.com/konflux-ci/konflux-ci/operator/internal/operatormetrics"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

var _ = Describe("Konflux Controller", func() {
	// allSubCRs returns every sub-CR the Konflux reconciler can create (unconditional + conditional).
	// envtest has no GC, so these must be cleaned explicitly after each test.
	// Conditional sub-CRs that were not created are safe to list: DeleteAndWait is a no-op when absent.
	allSubCRs := func() []client.Object {
		return []client.Object{
			&konfluxv1alpha1.KonfluxApplicationAPI{ObjectMeta: metav1.ObjectMeta{Name: applicationapi.CRName}},
			&konfluxv1alpha1.KonfluxBuildService{ObjectMeta: metav1.ObjectMeta{Name: buildservice.CRName}},
			&konfluxv1alpha1.KonfluxIntegrationService{ObjectMeta: metav1.ObjectMeta{Name: integrationservice.CRName}},
			&konfluxv1alpha1.KonfluxReleaseService{ObjectMeta: metav1.ObjectMeta{Name: releaseservice.CRName}},
			&konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: uictrl.CRName}},
			&konfluxv1alpha1.KonfluxRBAC{ObjectMeta: metav1.ObjectMeta{Name: rbac.CRName}},
			&konfluxv1alpha1.KonfluxInfo{ObjectMeta: metav1.ObjectMeta{Name: info.CRName}},
			&konfluxv1alpha1.KonfluxNamespaceLister{ObjectMeta: metav1.ObjectMeta{Name: namespacelister.CRName}},
			&konfluxv1alpha1.KonfluxEnterpriseContract{ObjectMeta: metav1.ObjectMeta{Name: enterprisecontract.CRName}},
			&konfluxv1alpha1.KonfluxCertManager{ObjectMeta: metav1.ObjectMeta{Name: certmanager.CRName}},
			&konfluxv1alpha1.KonfluxDefaultTenant{ObjectMeta: metav1.ObjectMeta{Name: defaulttenant.CRName}},
			&konfluxv1alpha1.KonfluxCLI{ObjectMeta: metav1.ObjectMeta{Name: cli.CRName}},
			&konfluxv1alpha1.KonfluxInternalRegistry{ObjectMeta: metav1.ObjectMeta{Name: internalregistry.CRName}},
			&konfluxv1alpha1.KonfluxSegmentBridge{ObjectMeta: metav1.ObjectMeta{Name: segmentbridge.CRName}},
			&konfluxv1alpha1.KonfluxImageController{ObjectMeta: metav1.ObjectMeta{Name: imagecontroller.CRName}},
		}
	}

	// startManager creates a per-test manager with the given ClusterInfo
	// and registers a DeferCleanup to cancel it after the test.
	// A per-test manager is required because each test wires the reconciler with a different
	// ClusterInfo (e.g. OpenShift vs vanilla Kubernetes), and a shared suite-level manager
	// cannot be re-configured between tests.
	startManager := func(clusterInfo *clusterinfo.Info) {
		mgr := testutil.NewTestManager(testEnv)
		Expect((&KonfluxReconciler{
			Client:      mgr.GetClient(),
			Scheme:      mgr.GetScheme(),
			ClusterInfo: clusterInfo,
		}).SetupWithManager(mgr)).To(Succeed())
		mgrCtx, cancel := context.WithCancel(testEnv.Ctx)
		waitForStop := testutil.StartManagerWithContext(mgrCtx, mgr)
		DeferCleanup(func() {
			cancel()
			waitForStop()
		})
	}

	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			// The Ready condition is written only after the reconciler completes all apply/get/status steps
			// without an early error return. Its presence (regardless of True/False) is therefore a reliable
			// sentinel that the entire reconcile loop ran to completion. Ready=True is not expected here
			// because the sub-controllers are not running in this test, so sub-CRs have no conditions yet.
			//
			// We also assert that every enabled-by-default sub-CR was created. This catches the case where
			// both an applyKonflux* call and its corresponding Get are removed together — the Ready condition
			// would still be set, but the sub-CR existence check would fail.
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())

				// Verify all enabled-by-default sub-CRs were created.
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: applicationapi.CRName}, &konfluxv1alpha1.KonfluxApplicationAPI{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildservice.CRName}, &konfluxv1alpha1.KonfluxBuildService{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: integrationservice.CRName}, &konfluxv1alpha1.KonfluxIntegrationService{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: releaseservice.CRName}, &konfluxv1alpha1.KonfluxReleaseService{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: uictrl.CRName}, &konfluxv1alpha1.KonfluxUI{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespacelister.CRName}, &konfluxv1alpha1.KonfluxNamespaceLister{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: enterprisecontract.CRName}, &konfluxv1alpha1.KonfluxEnterpriseContract{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: certmanager.CRName}, &konfluxv1alpha1.KonfluxCertManager{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName}, &konfluxv1alpha1.KonfluxDefaultTenant{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cli.CRName}, &konfluxv1alpha1.KonfluxCLI{})).To(Succeed())

				// Verify optional sub-CRs are NOT created (disabled by default in empty spec).
				err := k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, &konfluxv1alpha1.KonfluxImageController{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
				err = k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName}, &konfluxv1alpha1.KonfluxInternalRegistry{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
				err = k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, &konfluxv1alpha1.KonfluxSegmentBridge{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Konflux Name Validation (CEL)", func() {
		const requiredKonfluxName = "konflux"

		It("Should allow creation with the required name 'konflux'", func(ctx context.Context) {
			By("creating a Konflux instance with the required name")
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: requiredKonfluxName},
			}
			Expect(k8sClient.Create(ctx, konflux)).To(Succeed(), "Creation with required name should be allowed")
			testutil.DeferCleanupParentAndChildren(k8sClient, konflux)

			By("verifying the instance was created")
			created := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: requiredKonfluxName}, created)).To(Succeed())
			Expect(created.GetName()).To(Equal(requiredKonfluxName))
		})

		It("Should deny creation with a different name", func(ctx context.Context) {
			By("attempting to create a Konflux instance with a different name")
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: "my-konflux"},
			}
			err := k8sClient.Create(ctx, konflux)
			Expect(err).To(HaveOccurred(), "Creation with different name should be rejected")
			Expect(err.Error()).To(ContainSubstring("konflux"), "Error message should mention 'konflux'")
		})

		It("Should allow updates to the instance with the required name", func(ctx context.Context) {
			By("creating a Konflux instance with the required name")
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: requiredKonfluxName},
			}
			Expect(k8sClient.Create(ctx, konflux)).To(Succeed(), "Failed to create Konflux instance")
			testutil.DeferCleanupParentAndChildren(k8sClient, konflux)

			By("updating the instance")
			updated := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: requiredKonfluxName}, updated)).To(Succeed())
			if updated.Labels == nil {
				updated.Labels = make(map[string]string)
			}
			updated.Labels["test"] = "value"
			Expect(k8sClient.Update(ctx, updated)).To(Succeed(), "Updates should be allowed")
		})
	})

	Context("InternalRegistry conditional enablement", func() {
		const resourceName = "konflux"

		It("should not create InternalRegistry CR when internalRegistry is omitted", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR without internalRegistry config")
			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: resourceName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for reconcile to complete")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying InternalRegistry CR was not created")
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName}, registry)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "InternalRegistry CR should not exist when omitted")
		})

		It("should not create InternalRegistry CR when enabled is false", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with internalRegistry.enabled=false")
			disabled := false
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InternalRegistry: &konfluxv1alpha1.InternalRegistryConfig{Enabled: &disabled},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for reconcile to complete")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying InternalRegistry CR was not created")
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName}, registry)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "InternalRegistry CR should not exist when enabled=false")
		})

		It("should create InternalRegistry CR when enabled is true", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with internalRegistry.enabled=true")
			enabled := true
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InternalRegistry: &konfluxv1alpha1.InternalRegistryConfig{Enabled: &enabled},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying InternalRegistry CR was created")
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName}, registry)).To(Succeed())
				g.Expect(registry.Name).To(Equal(internalregistry.CRName))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should delete InternalRegistry CR when enabled changes from true to false", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with internalRegistry.enabled=true")
			enabled := true
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InternalRegistry: &konfluxv1alpha1.InternalRegistryConfig{Enabled: &enabled},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for InternalRegistry CR to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName},
					&konfluxv1alpha1.KonfluxInternalRegistry{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating Konflux CR to set enabled=false")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updatedKonflux)).To(Succeed())
			disabled := false
			updatedKonflux.Spec.InternalRegistry = &konfluxv1alpha1.InternalRegistryConfig{Enabled: &disabled}
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("waiting for InternalRegistry CR to be deleted")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName},
					&konfluxv1alpha1.KonfluxInternalRegistry{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "InternalRegistry CR should be deleted when enabled changes to false")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("DefaultTenant conditional enablement", func() {
		const resourceName = "konflux"

		It("should create DefaultTenant CR in standard installation type (enabled by default)", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR without defaultTenant config (standard type)")
			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: resourceName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying DefaultTenant CR was created (enabled in standard type)")
			tenant := &konfluxv1alpha1.KonfluxDefaultTenant{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName}, tenant)).To(Succeed())
				g.Expect(tenant.Name).To(Equal(defaulttenant.CRName))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should not create DefaultTenant CR in spoke installation type", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with spoke installation type")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InstallationType: konfluxv1alpha1.InstallationTypeSpoke,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for reconcile to complete")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying DefaultTenant CR was not created")
			tenant := &konfluxv1alpha1.KonfluxDefaultTenant{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName}, tenant)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "DefaultTenant CR should not exist in spoke type")
		})

		It("should delete DefaultTenant CR when switching from standard to spoke", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with standard installation type")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for DefaultTenant CR to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName},
					&konfluxv1alpha1.KonfluxDefaultTenant{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating Konflux CR to spoke installation type")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updatedKonflux)).To(Succeed())
			updatedKonflux.Spec.InstallationType = konfluxv1alpha1.InstallationTypeSpoke
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("waiting for DefaultTenant CR to be deleted")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName},
					&konfluxv1alpha1.KonfluxDefaultTenant{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "DefaultTenant CR should be deleted when switching to spoke")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("SegmentBridge conditional enablement", func() {
		const resourceName = "konflux"

		It("should not create SegmentBridge CR when telemetry is omitted", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR without telemetry config")
			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: resourceName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for reconcile to complete")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying SegmentBridge CR was not created")
			sb := &konfluxv1alpha1.KonfluxSegmentBridge{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, sb)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "SegmentBridge CR should not exist when telemetry is omitted")
		})

		It("should not create SegmentBridge CR when telemetry.enabled is false", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with telemetry.enabled=false")
			disabled := false
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					Telemetry: &konfluxv1alpha1.TelemetryConfig{Enabled: &disabled},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for reconcile to complete")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying SegmentBridge CR was not created")
			sb := &konfluxv1alpha1.KonfluxSegmentBridge{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, sb)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "SegmentBridge CR should not exist when telemetry.enabled=false")
		})

		It("should create SegmentBridge CR when telemetry.enabled is true", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with telemetry.enabled=true")
			enabled := true
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					Telemetry: &konfluxv1alpha1.TelemetryConfig{Enabled: &enabled},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying SegmentBridge CR was created")
			sb := &konfluxv1alpha1.KonfluxSegmentBridge{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, sb)).To(Succeed())
				g.Expect(sb.Name).To(Equal(segmentbridge.CRName))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should pass through telemetry.spec.cronJob to the child SegmentBridge CR", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with telemetry.spec.cronJob overrides")
			enabled := true
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					Telemetry: &konfluxv1alpha1.TelemetryConfig{
						Enabled: &enabled,
						Spec: &konfluxv1alpha1.KonfluxSegmentBridgeSpec{
							CronJob: &konfluxv1alpha1.ContainerSpec{
								Env: []corev1.EnvVar{
									{Name: "HTTP_PROXY", Value: "http://proxy.example.com:3128"},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying the child SegmentBridge CR carries the cronJob override")
			sb := &konfluxv1alpha1.KonfluxSegmentBridge{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, sb)).To(Succeed())
				g.Expect(sb.Spec.CronJob).NotTo(BeNil())
				g.Expect(sb.Spec.CronJob.Env).To(ContainElement(
					corev1.EnvVar{Name: "HTTP_PROXY", Value: "http://proxy.example.com:3128"}))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should delete SegmentBridge CR when enabled changes from true to false", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with telemetry.enabled=true")
			enabled := true
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					Telemetry: &konfluxv1alpha1.TelemetryConfig{Enabled: &enabled},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for SegmentBridge CR to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName},
					&konfluxv1alpha1.KonfluxSegmentBridge{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating Konflux CR to set telemetry.enabled=false")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updatedKonflux)).To(Succeed())
			disabled := false
			updatedKonflux.Spec.Telemetry = &konfluxv1alpha1.TelemetryConfig{Enabled: &disabled}
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("waiting for SegmentBridge CR to be deleted")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName},
					&konfluxv1alpha1.KonfluxSegmentBridge{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "SegmentBridge CR should be deleted when enabled changes to false")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("ImageController spec propagation", func() {
		const resourceName = "konflux"

		It("should create ImageController CR with empty spec when no spec is provided", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with imageController.enabled=true but no spec")
			enabled := true
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					ImageController: &konfluxv1alpha1.ImageControllerConfig{Enabled: &enabled},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying ImageController CR was created with empty spec")
			ic := &konfluxv1alpha1.KonfluxImageController{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, ic)).To(Succeed())
				g.Expect(ic.Spec.QuayCABundle).To(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should propagate quayCABundle spec to ImageController CR", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with imageController.spec.quayCABundle")
			enabled := true
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					ImageController: &konfluxv1alpha1.ImageControllerConfig{
						Enabled: &enabled,
						Spec: &konfluxv1alpha1.KonfluxImageControllerConfigSpec{
							QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
								ConfigMapName: "my-ca-bundle",
								Key:           "ca.crt",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying ImageController CR has the quayCABundle spec")
			ic := &konfluxv1alpha1.KonfluxImageController{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, ic)).To(Succeed())
				g.Expect(ic.Spec.QuayCABundle).NotTo(BeNil())
				g.Expect(ic.Spec.QuayCABundle.ConfigMapName).To(Equal("my-ca-bundle"))
				g.Expect(ic.Spec.QuayCABundle.Key).To(Equal("ca.crt"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should update ImageController CR spec when Konflux CR spec changes", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with imageController.spec.quayCABundle")
			enabled := true
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					ImageController: &konfluxv1alpha1.ImageControllerConfig{
						Enabled: &enabled,
						Spec: &konfluxv1alpha1.KonfluxImageControllerConfigSpec{
							QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
								ConfigMapName: "my-ca-bundle",
								Key:           "ca.crt",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for ImageController CR to be created with quayCABundle")
			Eventually(func(g Gomega) {
				ic := &konfluxv1alpha1.KonfluxImageController{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, ic)).To(Succeed())
				g.Expect(ic.Spec.QuayCABundle).NotTo(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating the Konflux CR to remove quayCABundle")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updatedKonflux)).To(Succeed())
			updatedKonflux.Spec.ImageController.Spec = nil
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("verifying ImageController CR no longer has quayCABundle")
			Eventually(func(g Gomega) {
				ic := &konfluxv1alpha1.KonfluxImageController{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, ic)).To(Succeed())
				g.Expect(ic.Spec.QuayCABundle).To(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("NamespaceLister spec propagation", func() {
		const resourceName = "konflux"

		It("should propagate namespace-lister config spec to KonfluxNamespaceLister CR", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					NamespaceLister: &konfluxv1alpha1.NamespaceListerConfig{
						Spec: &konfluxv1alpha1.KonfluxNamespaceListerConfigSpec{
							LogLevel: konfluxv1alpha1.LogLevelInfo,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			Eventually(func(g Gomega) {
				nl := &konfluxv1alpha1.KonfluxNamespaceLister{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespacelister.CRName}, nl)).To(Succeed())
				g.Expect(nl.Spec.LogLevel).To(Equal(konfluxv1alpha1.LogLevelInfo))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("CertManager spec forwarding", func() {
		const resourceName = "konflux"

		It("should forward spec.certManager.distributeClusterCABundle to KonfluxCertManager", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			enabled := true
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					CertManager: &konfluxv1alpha1.CertManagerConfig{
						DistributeClusterCABundle: &enabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			Eventually(func(g Gomega) {
				cm := &konfluxv1alpha1.KonfluxCertManager{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: certmanager.CRName}, cm)).To(Succeed())
				g.Expect(cm.Spec.DistributeClusterCABundle).NotTo(BeNil())
				g.Expect(*cm.Spec.DistributeClusterCABundle).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should forward spec.certManager.distributeClusterCABundle=false to KonfluxCertManager", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			disabled := false
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					CertManager: &konfluxv1alpha1.CertManagerConfig{
						DistributeClusterCABundle: &disabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			Eventually(func(g Gomega) {
				cm := &konfluxv1alpha1.KonfluxCertManager{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: certmanager.CRName}, cm)).To(Succeed())
				g.Expect(cm.Spec.DistributeClusterCABundle).NotTo(BeNil())
				g.Expect(*cm.Spec.DistributeClusterCABundle).To(BeFalse())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should forward nil distributeClusterCABundle when certManager config is omitted", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			Eventually(func(g Gomega) {
				cm := &konfluxv1alpha1.KonfluxCertManager{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: certmanager.CRName}, cm)).To(Succeed())
				g.Expect(cm.Spec.DistributeClusterCABundle).To(BeNil(), "distributeClusterCABundle should be nil when certManager config is omitted")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should forward nil distributeClusterCABundle when field is omitted within certManager config", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					CertManager: &konfluxv1alpha1.CertManagerConfig{},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			Eventually(func(g Gomega) {
				cm := &konfluxv1alpha1.KonfluxCertManager{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: certmanager.CRName}, cm)).To(Succeed())
				g.Expect(cm.Spec.DistributeClusterCABundle).To(BeNil(), "distributeClusterCABundle should be nil when field is omitted")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("ComponentMetrics propagation", func() {
		const resourceName = "konflux"

		It("should forward spec.componentMetrics to metrics-enabled operand CRs", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			disabled := false
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					ComponentMetrics: &konfluxv1alpha1.ComponentMetricsConfig{
						Enabled: &disabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			Eventually(func(g Gomega) {
				bs := &konfluxv1alpha1.KonfluxBuildService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildservice.CRName}, bs)).To(Succeed())
				g.Expect(bs.Spec.ComponentMetrics).NotTo(BeNil())
				g.Expect(bs.Spec.ComponentMetrics.IsEnabled()).To(BeFalse())

				is := &konfluxv1alpha1.KonfluxIntegrationService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: integrationservice.CRName}, is)).To(Succeed())
				g.Expect(is.Spec.ComponentMetrics).NotTo(BeNil())
				g.Expect(is.Spec.ComponentMetrics.IsEnabled()).To(BeFalse())

				rs := &konfluxv1alpha1.KonfluxReleaseService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: releaseservice.CRName}, rs)).To(Succeed())
				g.Expect(rs.Spec.ComponentMetrics).NotTo(BeNil())
				g.Expect(rs.Spec.ComponentMetrics.IsEnabled()).To(BeFalse())

				nl := &konfluxv1alpha1.KonfluxNamespaceLister{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespacelister.CRName}, nl)).To(Succeed())
				g.Expect(nl.Spec.ComponentMetrics).NotTo(BeNil())
				g.Expect(nl.Spec.ComponentMetrics.IsEnabled()).To(BeFalse())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should copy parent release and ui config specs onto operand CRs", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					KonfluxReleaseService: &konfluxv1alpha1.ReleaseServiceConfig{
						Spec: &konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
							Debug: true,
						},
					},
					KonfluxUI: &konfluxv1alpha1.KonfluxUIConfig{
						Spec: &konfluxv1alpha1.KonfluxUIConfigSpec{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			Eventually(func(g Gomega) {
				rs := &konfluxv1alpha1.KonfluxReleaseService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: releaseservice.CRName}, rs)).To(Succeed())
				g.Expect(rs.Spec.Debug).To(BeTrue())

				ui := &konfluxv1alpha1.KonfluxUI{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: uictrl.CRName}, ui)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should update operand CR componentMetrics when Konflux spec changes", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					ComponentMetrics: &konfluxv1alpha1.ComponentMetricsConfig{},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			Eventually(func(g Gomega) {
				bs := &konfluxv1alpha1.KonfluxBuildService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildservice.CRName}, bs)).To(Succeed())
				g.Expect(bs.Spec.ComponentMetrics.IsEnabled()).To(BeTrue())

				nl := &konfluxv1alpha1.KonfluxNamespaceLister{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespacelister.CRName}, nl)).To(Succeed())
				g.Expect(nl.Spec.ComponentMetrics.IsEnabled()).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			updated := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
			disabled := false
			updated.Spec.ComponentMetrics.Enabled = &disabled
			Expect(k8sClient.Update(ctx, updated)).To(Succeed())

			Eventually(func(g Gomega) {
				bs := &konfluxv1alpha1.KonfluxBuildService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildservice.CRName}, bs)).To(Succeed())
				g.Expect(bs.Spec.ComponentMetrics.IsEnabled()).To(BeFalse())

				nl := &konfluxv1alpha1.KonfluxNamespaceLister{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespacelister.CRName}, nl)).To(Succeed())
				g.Expect(nl.Spec.ComponentMetrics.IsEnabled()).To(BeFalse())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("EnterpriseContract spec propagation", func() {
		const resourceName = "konflux"

		It("should create EnterpriseContract CR with skipPolicies=false by default", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR without enterpriseContract config")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying EnterpriseContract CR was created with skipPolicies=false")
			ec := &konfluxv1alpha1.KonfluxEnterpriseContract{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: enterprisecontract.CRName}, ec)).To(Succeed())
				g.Expect(ec.Spec.SkipPolicies).To(BeFalse())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should propagate skipPolicies=true from Konflux CR to EnterpriseContract CR", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with enterpriseContract.skipPolicies=true")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					EnterpriseContract: &konfluxv1alpha1.EnterpriseContractConfig{
						SkipPolicies: true,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying EnterpriseContract CR has skipPolicies=true")
			ec := &konfluxv1alpha1.KonfluxEnterpriseContract{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: enterprisecontract.CRName}, ec)).To(Succeed())
				g.Expect(ec.Spec.SkipPolicies).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should update EnterpriseContract CR when Konflux CR spec changes", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with enterpriseContract.skipPolicies=true")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					EnterpriseContract: &konfluxv1alpha1.EnterpriseContractConfig{
						SkipPolicies: true,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for EnterpriseContract CR to be created with skipPolicies=true")
			Eventually(func(g Gomega) {
				ec := &konfluxv1alpha1.KonfluxEnterpriseContract{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: enterprisecontract.CRName}, ec)).To(Succeed())
				g.Expect(ec.Spec.SkipPolicies).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating the Konflux CR to remove enterpriseContract config")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updatedKonflux)).To(Succeed())
			updatedKonflux.Spec.EnterpriseContract = nil
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("verifying EnterpriseContract CR now has skipPolicies=false")
			Eventually(func(g Gomega) {
				ec := &konfluxv1alpha1.KonfluxEnterpriseContract{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: enterprisecontract.CRName}, ec)).To(Succeed())
				g.Expect(ec.Spec.SkipPolicies).To(BeFalse())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should reset skipPolicies to false on a pre-existing EC CR created by another manager", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("pre-creating the KonfluxEnterpriseContract CR with skipPolicies=true (simulating user-created CR)")
			preExisting := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: enterprisecontract.CRName},
				Spec:       konfluxv1alpha1.KonfluxEnterpriseContractSpec{SkipPolicies: true},
			}
			Expect(k8sClient.Create(ctx, preExisting)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, preExisting)

			By("creating Konflux CR without enterpriseContract config (defaults to skipPolicies=false)")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying the Konflux controller resets skipPolicies to false via SSA")
			Eventually(func(g Gomega) {
				ec := &konfluxv1alpha1.KonfluxEnterpriseContract{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: enterprisecontract.CRName}, ec)).To(Succeed())
				g.Expect(ec.Spec.SkipPolicies).To(BeFalse())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Spoke configuration (Info/RBAC only)", func() {
		const resourceName = "konflux"

		// spokeSpec returns a KonfluxSpec with spoke installation type.
		spokeSpec := func() konfluxv1alpha1.KonfluxSpec {
			return konfluxv1alpha1.KonfluxSpec{
				InstallationType: konfluxv1alpha1.InstallationTypeSpoke,
			}
		}

		It("should create only Info and RBAC child CRs in spoke configuration", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with spoke configuration")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec:       spokeSpec(),
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for reconcile to complete and verifying only Info+RBAC CRs exist")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())

				// Info and RBAC should be created
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())

				// All other components should NOT be created
				for _, tc := range []struct {
					name string
					obj  client.Object
				}{
					{"application-api", &konfluxv1alpha1.KonfluxApplicationAPI{ObjectMeta: metav1.ObjectMeta{Name: applicationapi.CRName}}},
					{"build-service", &konfluxv1alpha1.KonfluxBuildService{ObjectMeta: metav1.ObjectMeta{Name: buildservice.CRName}}},
					{"integration-service", &konfluxv1alpha1.KonfluxIntegrationService{ObjectMeta: metav1.ObjectMeta{Name: integrationservice.CRName}}},
					{"release-service", &konfluxv1alpha1.KonfluxReleaseService{ObjectMeta: metav1.ObjectMeta{Name: releaseservice.CRName}}},
					{"ui", &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: uictrl.CRName}}},
					{"namespace-lister", &konfluxv1alpha1.KonfluxNamespaceLister{ObjectMeta: metav1.ObjectMeta{Name: namespacelister.CRName}}},
					{"enterprise-contract", &konfluxv1alpha1.KonfluxEnterpriseContract{ObjectMeta: metav1.ObjectMeta{Name: enterprisecontract.CRName}}},
					{"cert-manager", &konfluxv1alpha1.KonfluxCertManager{ObjectMeta: metav1.ObjectMeta{Name: certmanager.CRName}}},
					{"default-tenant", &konfluxv1alpha1.KonfluxDefaultTenant{ObjectMeta: metav1.ObjectMeta{Name: defaulttenant.CRName}}},
					{"cli", &konfluxv1alpha1.KonfluxCLI{ObjectMeta: metav1.ObjectMeta{Name: cli.CRName}}},
				} {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tc.obj), tc.obj)
					g.Expect(errors.IsNotFound(err)).To(BeTrue(), "%s CR should not exist in spoke config, got: %v", tc.name, err)
				}

				// status.uiURL should be empty when UI is disabled
				g.Expect(updated.Status.UIURL).To(BeEmpty(), "uiURL should be empty when UI is disabled")

				// CertManagerAvailable condition should not be present when certManager is disabled
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeCertManagerAvailable)).To(BeNil(),
					"CertManagerAvailable condition should not be present when certManager is disabled")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should transition from full to spoke by removing disabled child CRs", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with full (default) configuration")
			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: resourceName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for all enabled-by-default sub-CRs to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildservice.CRName}, &konfluxv1alpha1.KonfluxBuildService{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating Konflux CR to spoke configuration")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updatedKonflux)).To(Succeed())
			updatedKonflux.Spec = spokeSpec()
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("waiting for disabled child CRs to be cleaned up")
			Eventually(func(g Gomega) {
				// Info and RBAC should still exist
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())

				// BuildService should be cleaned up
				err := k8sClient.Get(ctx, types.NamespacedName{Name: buildservice.CRName}, &konfluxv1alpha1.KonfluxBuildService{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "BuildService should be deleted in spoke config")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should transition from spoke to full by restoring components", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with spoke configuration")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec:       spokeSpec(),
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for spoke reconcile to complete")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating Konflux CR to full (default) configuration")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updatedKonflux)).To(Succeed())
			updatedKonflux.Spec = konfluxv1alpha1.KonfluxSpec{}
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("waiting for all enabled-by-default components to be restored")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildservice.CRName}, &konfluxv1alpha1.KonfluxBuildService{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: applicationapi.CRName}, &konfluxv1alpha1.KonfluxApplicationAPI{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: certmanager.CRName}, &konfluxv1alpha1.KonfluxCertManager{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Installation type deployment", func() {
		const resourceName = "konflux"

		boolPtr := func(v bool) *bool { return &v }

		// allStandardCRs lists all CRs created under the standard installation type.
		type crCheck struct {
			name   string
			crName string
			obj    func() client.Object
		}

		allStandardCRs := func() []crCheck {
			return []crCheck{
				{"application-api", applicationapi.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxApplicationAPI{} }},
				{"build-service", buildservice.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxBuildService{} }},
				{"integration-service", integrationservice.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxIntegrationService{} }},
				{"release-service", releaseservice.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxReleaseService{} }},
				{"ui", uictrl.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxUI{} }},
				{"rbac", rbac.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxRBAC{} }},
				{"info", info.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxInfo{} }},
				{"namespace-lister", namespacelister.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxNamespaceLister{} }},
				{"enterprise-contract", enterprisecontract.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxEnterpriseContract{} }},
				{"cert-manager", certmanager.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxCertManager{} }},
				{"default-tenant", defaulttenant.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxDefaultTenant{} }},
				{"cli", cli.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxCLI{} }},
			}
		}

		allOptInCRs := func() []crCheck {
			return []crCheck{
				{"image-controller", imagecontroller.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxImageController{} }},
				{"internal-registry", internalregistry.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxInternalRegistry{} }},
				{"segment-bridge", segmentbridge.CRName, func() client.Object { return &konfluxv1alpha1.KonfluxSegmentBridge{} }},
			}
		}

		It("should reconcile with all opt-in components enabled alongside standard defaults", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with all opt-in components enabled (standard type)")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					ImageController:  &konfluxv1alpha1.ImageControllerConfig{Enabled: boolPtr(true)},
					InternalRegistry: &konfluxv1alpha1.InternalRegistryConfig{Enabled: boolPtr(true)},
					Telemetry:        &konfluxv1alpha1.TelemetryConfig{Enabled: boolPtr(true)},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying all standard AND opt-in CRs are created")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())

				// All standard components
				for _, tc := range allStandardCRs() {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tc.crName}, tc.obj())).To(Succeed(),
						"%s CR should exist when all components enabled", tc.name)
				}

				// All opt-in components
				for _, tc := range allOptInCRs() {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tc.crName}, tc.obj())).To(Succeed(),
						"%s CR should exist when explicitly enabled", tc.name)
				}
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should reconcile spoke type with opt-in components enabled", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with spoke type and opt-in components")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InstallationType: konfluxv1alpha1.InstallationTypeSpoke,
					ImageController:  &konfluxv1alpha1.ImageControllerConfig{Enabled: boolPtr(true)},
					InternalRegistry: &konfluxv1alpha1.InternalRegistryConfig{Enabled: boolPtr(true)},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying only spoke + opt-in CRs are created")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())

				// Info and RBAC (spoke components) should exist
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())

				// Opt-in components should exist
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, &konfluxv1alpha1.KonfluxImageController{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName}, &konfluxv1alpha1.KonfluxInternalRegistry{})).To(Succeed())

				// Standard-only components should NOT exist
				for _, tc := range []struct {
					name string
					obj  client.Object
				}{
					{"application-api", &konfluxv1alpha1.KonfluxApplicationAPI{ObjectMeta: metav1.ObjectMeta{Name: applicationapi.CRName}}},
					{"build-service", &konfluxv1alpha1.KonfluxBuildService{ObjectMeta: metav1.ObjectMeta{Name: buildservice.CRName}}},
					{"integration-service", &konfluxv1alpha1.KonfluxIntegrationService{ObjectMeta: metav1.ObjectMeta{Name: integrationservice.CRName}}},
					{"release-service", &konfluxv1alpha1.KonfluxReleaseService{ObjectMeta: metav1.ObjectMeta{Name: releaseservice.CRName}}},
					{"ui", &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: uictrl.CRName}}},
					{"namespace-lister", &konfluxv1alpha1.KonfluxNamespaceLister{ObjectMeta: metav1.ObjectMeta{Name: namespacelister.CRName}}},
					{"enterprise-contract", &konfluxv1alpha1.KonfluxEnterpriseContract{ObjectMeta: metav1.ObjectMeta{Name: enterprisecontract.CRName}}},
					{"cert-manager", &konfluxv1alpha1.KonfluxCertManager{ObjectMeta: metav1.ObjectMeta{Name: certmanager.CRName}}},
					{"default-tenant", &konfluxv1alpha1.KonfluxDefaultTenant{ObjectMeta: metav1.ObjectMeta{Name: defaulttenant.CRName}}},
					{"cli", &konfluxv1alpha1.KonfluxCLI{ObjectMeta: metav1.ObjectMeta{Name: cli.CRName}}},
				} {
					err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tc.obj), tc.obj)
					g.Expect(errors.IsNotFound(err)).To(BeTrue(), "%s CR should not exist in spoke config, got: %v", tc.name, err)
				}

				// status.uiURL should be empty when UI is disabled
				g.Expect(updated.Status.UIURL).To(BeEmpty(), "uiURL should be empty in spoke config")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should transition from spoke to standard by restoring all components", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with spoke type")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InstallationType: konfluxv1alpha1.InstallationTypeSpoke,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for spoke CRs to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())
				// Build service should not exist yet
				err := k8sClient.Get(ctx, types.NamespacedName{Name: buildservice.CRName}, &konfluxv1alpha1.KonfluxBuildService{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating to standard installation type")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updatedKonflux)).To(Succeed())
			updatedKonflux.Spec.InstallationType = konfluxv1alpha1.InstallationTypeStandard
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("verifying all standard components are restored")
			Eventually(func(g Gomega) {
				for _, tc := range allStandardCRs() {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tc.crName}, tc.obj())).To(Succeed(),
						"%s CR should exist after switching to standard", tc.name)
				}
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Stale condition cleanup on component disable", func() {
		const resourceName = "konflux"

		It("should remove prefixed conditions when a component is disabled", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with all defaults enabled")
			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: resourceName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for sub-CRs to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildservice.CRName}, &konfluxv1alpha1.KonfluxBuildService{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: uictrl.CRName}, &konfluxv1alpha1.KonfluxUI{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("setting Ready condition on the BuildService sub-CR to simulate sub-controller")
			bs := &konfluxv1alpha1.KonfluxBuildService{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildservice.CRName}, bs)).To(Succeed())
			apimeta.SetStatusCondition(&bs.Status.Conditions, metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "DeploymentReady",
				Message:            "test condition",
				ObservedGeneration: bs.Generation,
			})
			Expect(k8sClient.Status().Update(ctx, bs)).To(Succeed())

			By("waiting for the reconciler to copy the prefixed condition to the Konflux CR")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				// CopySubCRStatus should have created a "build-service.Ready" condition
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), "build-service.Ready")).NotTo(BeNil(),
					"build-service.Ready condition should be copied to parent")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("switching to spoke installation type to disable BuildService")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updatedKonflux)).To(Succeed())
			updatedKonflux.Spec.InstallationType = konfluxv1alpha1.InstallationTypeSpoke
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("waiting for the stale build-service.Ready condition to be cleaned up")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())

				// The prefixed condition should be removed by CleanupStaleConditions
				for _, cond := range updated.GetConditions() {
					g.Expect(strings.HasPrefix(cond.Type, "build-service.")).To(BeFalse(),
						"stale condition %q should have been cleaned up", cond.Type)
				}

				// Non-prefixed conditions should still exist
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil(),
					"Ready condition should still exist")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("UI URL propagation", func() {
		const resourceName = "konflux"

		It("should propagate UI URL from KonfluxUI status and clear it when UI is disabled", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with defaults (UI enabled)")
			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: resourceName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for KonfluxUI sub-CR to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: uictrl.CRName}, &konfluxv1alpha1.KonfluxUI{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("setting Ingress status on the KonfluxUI sub-CR to simulate UI controller")
			ui := &konfluxv1alpha1.KonfluxUI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: uictrl.CRName}, ui)).To(Succeed())
			ui.Status.Ingress = &konfluxv1alpha1.IngressStatus{
				Enabled:  true,
				Hostname: "konflux.example.com",
				URL:      "https://konflux.example.com",
			}
			Expect(k8sClient.Status().Update(ctx, ui)).To(Succeed())

			By("waiting for the reconciler to propagate the URL to the Konflux CR")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(updated.Status.UIURL).To(Equal("https://konflux.example.com"),
					"uiURL should be propagated from KonfluxUI status")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("switching to spoke installation type (UI is disabled in spoke)")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updatedKonflux)).To(Succeed())
			updatedKonflux.Spec.InstallationType = konfluxv1alpha1.InstallationTypeSpoke
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("waiting for the UI URL to be cleared")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(updated.Status.UIURL).To(BeEmpty(),
					"uiURL should be cleared when switching to spoke")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Sub-CR status aggregation to parent", func() {
		const resourceName = "konflux"

		boolPtr := func(v bool) *bool { return &v }

		// setSubCRReady sets the Ready condition on a sub-CR to simulate a sub-controller.
		setSubCRReady := func(ctx context.Context, obj konfluxv1alpha1.ConditionAccessor, ready bool) {
			status := metav1.ConditionTrue
			reason := "DeploymentReady"
			message := "All deployments are ready"
			if !ready {
				status = metav1.ConditionFalse
				reason = "DeploymentNotReady"
				message = "Deployments not ready"
			}
			conditions := obj.GetConditions()
			apimeta.SetStatusCondition(&conditions, metav1.Condition{
				Type:               constant.ConditionTypeReady,
				Status:             status,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: obj.GetGeneration(),
			})
			obj.SetConditions(conditions)
			ExpectWithOffset(1, k8sClient.Status().Update(ctx, obj)).To(Succeed())
		}

		It("should report Ready=True when all enabled sub-CRs are ready (spoke config)", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with spoke configuration (Info+RBAC only)")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InstallationType: konfluxv1alpha1.InstallationTypeSpoke,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for Info and RBAC sub-CRs to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("setting Ready=True on both sub-CRs to simulate sub-controllers")
			infoObj := &konfluxv1alpha1.KonfluxInfo{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, infoObj)).To(Succeed())
			setSubCRReady(ctx, infoObj, true)

			rbacObj := &konfluxv1alpha1.KonfluxRBAC{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, rbacObj)).To(Succeed())
			setSubCRReady(ctx, rbacObj, true)

			By("verifying the parent Konflux CR becomes Ready=True with correct component count")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())

				readyCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(readyCond.Reason).To(Equal("AllComponentsReady"))
				g.Expect(readyCond.Message).To(ContainSubstring("2 components are ready"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should report Ready=False when some enabled sub-CRs are not ready", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with spoke configuration (Info+RBAC only)")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InstallationType: konfluxv1alpha1.InstallationTypeSpoke,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for Info and RBAC sub-CRs to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("setting Ready=True on Info but leaving RBAC not-ready")
			infoObj := &konfluxv1alpha1.KonfluxInfo{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, infoObj)).To(Succeed())
			setSubCRReady(ctx, infoObj, true)

			By("verifying the parent shows Ready=False with reason identifying the not-ready component")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())

				readyCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(readyCond.Reason).To(Equal("ComponentsNotReady"))
				g.Expect(readyCond.Message).To(ContainSubstring("rbac is not ready"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should transition to Ready=True when all spoke sub-CRs become ready", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with spoke configuration (Info+RBAC)")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InstallationType: konfluxv1alpha1.InstallationTypeSpoke,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for Info and RBAC sub-CRs to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("setting Info as ready, RBAC as not ready")
			infoObj := &konfluxv1alpha1.KonfluxInfo{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, infoObj)).To(Succeed())
			setSubCRReady(ctx, infoObj, true)

			By("confirming parent is not ready because RBAC is not ready")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())

				readyCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(readyCond.Message).To(ContainSubstring("rbac is not ready"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("transitioning RBAC to ready")
			rbacObj := &konfluxv1alpha1.KonfluxRBAC{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, rbacObj)).To(Succeed())
			setSubCRReady(ctx, rbacObj, true)

			By("verifying parent becomes Ready=True with both spoke components ready")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())

				readyCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(readyCond.Reason).To(Equal("AllComponentsReady"))
				g.Expect(readyCond.Message).To(ContainSubstring("2 components are ready"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should copy sub-CR conditions to parent with correct prefixes", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with spoke configuration (Info+RBAC only)")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InstallationType: konfluxv1alpha1.InstallationTypeSpoke,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for Info and RBAC sub-CRs to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("setting Ready conditions on both sub-CRs")
			infoObj := &konfluxv1alpha1.KonfluxInfo{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, infoObj)).To(Succeed())
			setSubCRReady(ctx, infoObj, true)

			rbacObj := &konfluxv1alpha1.KonfluxRBAC{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, rbacObj)).To(Succeed())
			setSubCRReady(ctx, rbacObj, false)

			By("verifying prefixed conditions appear on the parent CR")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())

				// info.Ready should be True (copied from KonfluxInfo sub-CR)
				infoCond := apimeta.FindStatusCondition(updated.GetConditions(), "info.Ready")
				g.Expect(infoCond).NotTo(BeNil(), "info.Ready condition should be present on parent")
				g.Expect(infoCond.Status).To(Equal(metav1.ConditionTrue))

				// rbac.Ready should be False (copied from KonfluxRBAC sub-CR)
				rbacCond := apimeta.FindStatusCondition(updated.GetConditions(), "rbac.Ready")
				g.Expect(rbacCond).NotTo(BeNil(), "rbac.Ready condition should be present on parent")
				g.Expect(rbacCond.Status).To(Equal(metav1.ConditionFalse))

				// Disabled components should NOT have prefixed conditions
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), "build-service.Ready")).To(BeNil(),
					"disabled component should not have prefixed conditions on parent")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should update aggregated readiness when a sub-CR transitions from not-ready to ready", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with spoke configuration (Info+RBAC only)")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InstallationType: konfluxv1alpha1.InstallationTypeSpoke,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for sub-CRs to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("setting both sub-CRs to not-ready initially")
			infoObj := &konfluxv1alpha1.KonfluxInfo{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, infoObj)).To(Succeed())
			setSubCRReady(ctx, infoObj, false)

			rbacObj := &konfluxv1alpha1.KonfluxRBAC{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, rbacObj)).To(Succeed())
			setSubCRReady(ctx, rbacObj, false)

			By("confirming parent is not ready")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())

				readyCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(readyCond.Message).To(ContainSubstring("info is not ready"))
				g.Expect(readyCond.Message).To(ContainSubstring("rbac is not ready"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("transitioning both sub-CRs to ready")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, infoObj)).To(Succeed())
			setSubCRReady(ctx, infoObj, true)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, rbacObj)).To(Succeed())
			setSubCRReady(ctx, rbacObj, true)

			By("verifying parent transitions to Ready=True")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())

				readyCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(readyCond.Reason).To(Equal("AllComponentsReady"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should correctly aggregate status with spoke plus optional component", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with spoke + imageController enabled")
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InstallationType: konfluxv1alpha1.InstallationTypeSpoke,
					ImageController:  &konfluxv1alpha1.ImageControllerConfig{Enabled: boolPtr(true)},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for Info, RBAC, and ImageController sub-CRs to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, &konfluxv1alpha1.KonfluxInfo{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, &konfluxv1alpha1.KonfluxRBAC{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, &konfluxv1alpha1.KonfluxImageController{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("setting all three sub-CRs to ready")
			infoObj := &konfluxv1alpha1.KonfluxInfo{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: info.CRName}, infoObj)).To(Succeed())
			setSubCRReady(ctx, infoObj, true)

			rbacObj := &konfluxv1alpha1.KonfluxRBAC{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: rbac.CRName}, rbacObj)).To(Succeed())
			setSubCRReady(ctx, rbacObj, true)

			icObj := &konfluxv1alpha1.KonfluxImageController{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, icObj)).To(Succeed())
			setSubCRReady(ctx, icObj, true)

			By("verifying parent reports Ready=True with 3 components")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())

				readyCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(readyCond.Message).To(ContainSubstring("3 components are ready"))

				// All three prefixed conditions should be present
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), "info.Ready")).NotTo(BeNil())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), "rbac.Ready")).NotTo(BeNil())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), "image-controller.Ready")).NotTo(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("making ImageController not-ready")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, icObj)).To(Succeed())
			setSubCRReady(ctx, icObj, false)

			By("verifying parent transitions to Ready=False mentioning the not-ready component")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())

				readyCond := apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(readyCond.Message).To(ContainSubstring("image-controller is not ready"))
				// Info and RBAC should not be mentioned as not-ready
				g.Expect(readyCond.Message).NotTo(ContainSubstring("info is not ready"))
				g.Expect(readyCond.Message).NotTo(ContainSubstring("rbac is not ready"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("konflux_up metric", func() {
		const resourceName = "konflux"

		It("should set konflux_up to 0 when the Konflux CR is deleted", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			// Create the CR and wait for reconcile to settle.
			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: resourceName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			// Re-seed gauge to 1 AFTER reconcile settled (defer already set it to 0
			// because Ready=False without sub-controllers). This ensures that only the
			// NotFound path can flip the gauge back to 0.
			operatormetrics.SetKonfluxUp(true)
			Expect(prometheustestutil.ToFloat64(operatormetrics.KonfluxUpGauge())).To(Equal(float64(1)))

			Expect(k8sClient.Delete(ctx, cr)).To(Succeed())

			Eventually(func() float64 {
				return prometheustestutil.ToFloat64(operatormetrics.KonfluxUpGauge())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Equal(float64(0)))
		})

		It("should update konflux_up after a successful reconcile", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: resourceName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			// After reconcile completes, the gauge should be set (0 because sub-controllers
			// are not running so Ready will not be True, but the metric is actively updated).
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
				// Gauge must reflect the current Ready state.
				val := prometheustestutil.ToFloat64(operatormetrics.KonfluxUpGauge())
				if updated.IsReady() {
					g.Expect(val).To(Equal(float64(1)))
				} else {
					g.Expect(val).To(Equal(float64(0)))
				}
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})
})

// createTestClusterInfo creates a minimal ClusterInfo for testing
func createTestClusterInfo() *clusterinfo.Info {
	mockClient := &testMockDiscoveryClient{
		resources:     map[string]*metav1.APIResourceList{},
		serverVersion: &version.Info{GitVersion: "v1.30.0"},
	}
	clusterInfo, _ := clusterinfo.DetectWithClient(mockClient)
	return clusterInfo
}

// testMockDiscoveryClient implements clusterinfo.DiscoveryClient for general testing
type testMockDiscoveryClient struct {
	resources     map[string]*metav1.APIResourceList
	serverVersion *version.Info
}

func (m *testMockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if r, ok := m.resources[groupVersion]; ok {
		return r, nil
	}
	return nil, errors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *testMockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return m.serverVersion, nil
}
