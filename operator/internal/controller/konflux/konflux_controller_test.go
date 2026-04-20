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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/applicationapi"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/buildservice"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/certmanager"
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

	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

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
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, &konfluxv1alpha1.KonfluxImageController{})).To(MatchError(ContainSubstring("not found")))
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName}, &konfluxv1alpha1.KonfluxInternalRegistry{})).To(MatchError(ContainSubstring("not found")))
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, &konfluxv1alpha1.KonfluxSegmentBridge{})).To(MatchError(ContainSubstring("not found")))
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, konflux)

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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, konflux)

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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			By("waiting for reconcile to complete")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			By("waiting for reconcile to complete")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)
			DeferCleanup(testutil.DeleteAndWait, k8sClient,
				&konfluxv1alpha1.KonfluxInternalRegistry{ObjectMeta: metav1.ObjectMeta{Name: internalregistry.CRName}})

			By("verifying InternalRegistry CR was created")
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName}, registry)).To(Succeed())
				g.Expect(registry.Name).To(Equal(internalregistry.CRName))
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			By("waiting for InternalRegistry CR to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: internalregistry.CRName},
					&konfluxv1alpha1.KonfluxInternalRegistry{})).To(Succeed())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

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
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})
	})

	Context("DefaultTenant conditional enablement", func() {
		const resourceName = "konflux"

		It("should create DefaultTenant CR when defaultTenant is omitted (enabled by default)", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR without defaultTenant config")
			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: resourceName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)
			DeferCleanup(testutil.DeleteAndWait, k8sClient,
				&konfluxv1alpha1.KonfluxDefaultTenant{ObjectMeta: metav1.ObjectMeta{Name: defaulttenant.CRName}})

			By("verifying DefaultTenant CR was created (enabled by default)")
			tenant := &konfluxv1alpha1.KonfluxDefaultTenant{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName}, tenant)).To(Succeed())
				g.Expect(tenant.Name).To(Equal(defaulttenant.CRName))
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)
			DeferCleanup(testutil.DeleteAndWait, k8sClient,
				&konfluxv1alpha1.KonfluxDefaultTenant{ObjectMeta: metav1.ObjectMeta{Name: defaulttenant.CRName}})

			By("verifying DefaultTenant CR was created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName},
					&konfluxv1alpha1.KonfluxDefaultTenant{})).To(Succeed())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			By("waiting for reconcile to complete")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			By("waiting for DefaultTenant CR to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: defaulttenant.CRName},
					&konfluxv1alpha1.KonfluxDefaultTenant{})).To(Succeed())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

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
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})
	})

	Context("SegmentBridge conditional enablement", func() {
		const resourceName = "konflux"

		It("should not create SegmentBridge CR when telemetry is omitted", func(ctx context.Context) {
			startManager(createTestClusterInfo())

			By("creating Konflux CR without telemetry config")
			cr := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: resourceName}}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			By("waiting for reconcile to complete")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			By("waiting for reconcile to complete")
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.Konflux{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, updated)).To(Succeed())
				g.Expect(apimeta.FindStatusCondition(updated.GetConditions(), constant.ConditionTypeReady)).NotTo(BeNil())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)
			DeferCleanup(testutil.DeleteAndWait, k8sClient,
				&konfluxv1alpha1.KonfluxSegmentBridge{ObjectMeta: metav1.ObjectMeta{Name: segmentbridge.CRName}})

			By("verifying SegmentBridge CR was created")
			sb := &konfluxv1alpha1.KonfluxSegmentBridge{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, sb)).To(Succeed())
				g.Expect(sb.Name).To(Equal(segmentbridge.CRName))
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			By("waiting for SegmentBridge CR to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName},
					&konfluxv1alpha1.KonfluxSegmentBridge{})).To(Succeed())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

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
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
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
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)
			DeferCleanup(testutil.DeleteAndWait, k8sClient,
				&konfluxv1alpha1.KonfluxImageController{ObjectMeta: metav1.ObjectMeta{Name: imagecontroller.CRName}})

			By("verifying ImageController CR was created with empty spec")
			ic := &konfluxv1alpha1.KonfluxImageController{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, ic)).To(Succeed())
				g.Expect(ic.Spec.QuayCABundle).To(BeNil())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
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
						Spec: &konfluxv1alpha1.KonfluxImageControllerSpec{
							QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
								ConfigMapName: "my-ca-bundle",
								Key:           "ca.crt",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)
			DeferCleanup(testutil.DeleteAndWait, k8sClient,
				&konfluxv1alpha1.KonfluxImageController{ObjectMeta: metav1.ObjectMeta{Name: imagecontroller.CRName}})

			By("verifying ImageController CR has the quayCABundle spec")
			ic := &konfluxv1alpha1.KonfluxImageController{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, ic)).To(Succeed())
				g.Expect(ic.Spec.QuayCABundle).NotTo(BeNil())
				g.Expect(ic.Spec.QuayCABundle.ConfigMapName).To(Equal("my-ca-bundle"))
				g.Expect(ic.Spec.QuayCABundle.Key).To(Equal("ca.crt"))
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
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
						Spec: &konfluxv1alpha1.KonfluxImageControllerSpec{
							QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
								ConfigMapName: "my-ca-bundle",
								Key:           "ca.crt",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)
			DeferCleanup(testutil.DeleteAndWait, k8sClient,
				&konfluxv1alpha1.KonfluxImageController{ObjectMeta: metav1.ObjectMeta{Name: imagecontroller.CRName}})

			By("waiting for ImageController CR to be created with quayCABundle")
			Eventually(func(g Gomega) {
				ic := &konfluxv1alpha1.KonfluxImageController{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: imagecontroller.CRName}, ic)).To(Succeed())
				g.Expect(ic.Spec.QuayCABundle).NotTo(BeNil())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

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
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
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
