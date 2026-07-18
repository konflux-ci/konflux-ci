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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
			// We also assert that every always-on sub-CR was created. This catches the case where both
			// an applyKonflux* call and its corresponding Get are removed together — the Ready condition
			// would still be set, but the sub-CR existence check would fail.
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())

				// Verify all always-on sub-CRs were created.
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

		It("should create DefaultTenant CR when defaultTenant is omitted (enabled by default)", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR without defaultTenant config")
			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: resourceName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying DefaultTenant CR was created (enabled by default)")
			tenant := &konfluxv1alpha1.KonfluxDefaultTenant{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName}, tenant)).To(Succeed())
				g.Expect(tenant.Name).To(Equal(defaulttenant.CRName))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should create DefaultTenant CR when enabled is explicitly true", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with defaultTenant.enabled=true")
			enabled := true
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					DefaultTenant: &konfluxv1alpha1.DefaultTenantConfig{Enabled: &enabled},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("verifying DefaultTenant CR was created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName},
					&konfluxv1alpha1.KonfluxDefaultTenant{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should not create DefaultTenant CR when enabled is false", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with defaultTenant.enabled=false")
			disabled := false
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					DefaultTenant: &konfluxv1alpha1.DefaultTenantConfig{Enabled: &disabled},
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
			Expect(errors.IsNotFound(err)).To(BeTrue(), "DefaultTenant CR should not exist when enabled=false")
		})

		It("should delete DefaultTenant CR when enabled changes from true to false", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR with defaultTenant.enabled=true")
			enabled := true
			cr := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName},
				Spec: konfluxv1alpha1.KonfluxSpec{
					DefaultTenant: &konfluxv1alpha1.DefaultTenantConfig{Enabled: &enabled},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, allSubCRs()...)

			By("waiting for DefaultTenant CR to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName},
					&konfluxv1alpha1.KonfluxDefaultTenant{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating Konflux CR to set enabled=false")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updatedKonflux)).To(Succeed())
			disabled := false
			updatedKonflux.Spec.DefaultTenant = &konfluxv1alpha1.DefaultTenantConfig{Enabled: &disabled}
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("waiting for DefaultTenant CR to be deleted")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName},
					&konfluxv1alpha1.KonfluxDefaultTenant{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "DefaultTenant CR should be deleted when enabled changes to false")
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
