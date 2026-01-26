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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
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
	uictrl "github.com/konflux-ci/konflux-ci/operator/internal/controller/ui"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the Konflux CR.
	CRName = "konflux"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "Konflux"
)

// konfluxCleanupGVKs defines which sub-CR types should be cleaned up when they are
// no longer part of the desired state. Only optional/conditional sub-CRs are listed here.
// Always-applied sub-CRs don't need cleanup (they're always tracked and never become orphans).
var konfluxCleanupGVKs = []schema.GroupVersionKind{
	// KonfluxImageController is optional - only created when spec.imageController.enabled is true
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxImageController"},
	// KonfluxInternalRegistry is optional - only created when spec.internalRegistry.enabled is true
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxInternalRegistry"},
	// KonfluxDefaultTenant is optional - only created when spec.defaultTenant.enabled is true (default)
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxDefaultTenant"},
}

// konfluxClusterScopedAllowList restricts which cluster-scoped sub-CRs can be deleted
// during orphan cleanup. Only conditionally-created sub-CRs need to be listed here.
var konfluxClusterScopedAllowList = tracking.ClusterScopedAllowList{
	// KonfluxImageController is optional - only created when spec.imageController.enabled is true
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxImageController"}: sets.New(
		"konflux-image-controller",
	),
	// KonfluxInternalRegistry is optional - only created when spec.internalRegistry.enabled is true
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxInternalRegistry"}: sets.New(
		"konflux-internal-registry",
	),
	// KonfluxDefaultTenant is optional - only created when spec.defaultTenant.enabled is true (default)
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxDefaultTenant"}: sets.New(
		"konflux-default-tenant",
	),
}

// KonfluxReconciler reconciles a Konflux object
type KonfluxReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ClusterInfo *clusterinfo.Info
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxbuildservices,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxbuildservices/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxbuildservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxintegrationservices,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxintegrationservices/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxintegrationservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxreleaseservices,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxreleaseservices/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxreleaseservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxuis,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxuis/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxuis/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxrbacs,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxrbacs/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxrbacs/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxnamespacelisters,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxnamespacelisters/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxnamespacelisters/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxenterprisecontracts,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxenterprisecontracts/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxenterprisecontracts/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxapplicationapis,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxapplicationapis/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxapplicationapis/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluximagecontrollers,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluximagecontrollers/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluximagecontrollers/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxcertmanagers,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxcertmanagers/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxcertmanagers/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxdefaulttenants,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxdefaulttenants/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxdefaulttenants/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
//
//nolint:gocyclo // High complexity is acceptable here due to multiple sub-CR reconciliations
func (r *KonfluxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Konflux instance
	konflux := &konfluxv1alpha1.Konflux{}
	if err := r.Get(ctx, req.NamespacedName, konflux); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling Konflux", "name", konflux.Name)

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), konflux, crKind)

	// Initialize tracking client for declarative resource management
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             konflux,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         "konflux",
		FieldManager:      FieldManager,
	})

	// Apply the KonfluxApplicationAPI CR
	if err := r.applyKonfluxApplicationAPI(ctx, tc); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxApplicationAPI")
	}

	// Apply the KonfluxBuildService CR
	if err := r.applyKonfluxBuildService(ctx, tc, konflux); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxBuildService")
	}

	// Apply the KonfluxIntegrationService CR
	if err := r.applyKonfluxIntegrationService(ctx, tc, konflux); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxIntegrationService")
	}

	// Apply the KonfluxReleaseService CR
	if err := r.applyKonfluxReleaseService(ctx, tc, konflux); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxReleaseService")
	}

	// Apply the KonfluxUI CR
	if err := r.applyKonfluxUI(ctx, tc, konflux); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxUI")
	}

	// Apply the KonfluxRBAC CR
	if err := r.applyKonfluxRBAC(ctx, tc); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxRBAC")
	}

	// Apply the KonfluxInfo CR
	if err := r.applyKonfluxInfo(ctx, tc, konflux); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxInfo")
	}

	// Apply the KonfluxNamespaceLister CR
	if err := r.applyKonfluxNamespaceLister(ctx, tc, konflux); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxNamespaceLister")
	}

	// Apply the KonfluxEnterpriseContract CR
	if err := r.applyKonfluxEnterpriseContract(ctx, tc); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxEnterpriseContract")
	}

	// Apply the KonfluxImageController CR (only if enabled)
	if konflux.Spec.IsImageControllerEnabled() {
		if err := r.applyKonfluxImageController(ctx, tc); err != nil {
			return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxImageController")
		}
	}

	// Apply the KonfluxCertManager CR
	if err := r.applyKonfluxCertManager(ctx, tc, konflux); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxCertManager")
	}

	// Apply the KonfluxInternalRegistry CR (only if enabled)
	if konflux.Spec.IsInternalRegistryEnabled() {
		if err := r.applyKonfluxInternalRegistry(ctx, tc); err != nil {
			return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxInternalRegistry")
		}
	}

	// Apply the KonfluxDefaultTenant CR (enabled by default)
	if konflux.Spec.IsDefaultTenantEnabled() {
		if err := r.applyKonfluxDefaultTenant(ctx, tc); err != nil {
			return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "apply KonfluxDefaultTenant")
		}
	}

	// Cleanup orphaned sub-CRs - delete any sub-CRs with our owner label
	// that weren't applied during this reconcile (e.g., disabled optional components)
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, konflux.Name, konfluxCleanupGVKs,
		tracking.WithClusterScopedAllowList(konfluxClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Collect status from all sub-CRs.
	// All component deployments are managed by their respective reconcilers,
	// so we aggregate readiness by checking each sub-CR's Ready condition.
	var subCRStatuses []condition.SubCRStatus

	// Get and copy status from the KonfluxBuildService CR
	buildService := &konfluxv1alpha1.KonfluxBuildService{}
	if err := r.Get(ctx, client.ObjectKey{Name: buildservice.CRName}, buildService); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxBuildService status")
	}
	subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, buildService, "build-service"))

	// Get and copy status from the KonfluxIntegrationService CR
	integrationService := &konfluxv1alpha1.KonfluxIntegrationService{}
	if err := r.Get(ctx, client.ObjectKey{Name: integrationservice.CRName}, integrationService); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxIntegrationService status")
	}
	subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, integrationService, "integration-service"))

	// Get and copy status from the KonfluxReleaseService CR
	releaseService := &konfluxv1alpha1.KonfluxReleaseService{}
	if err := r.Get(ctx, client.ObjectKey{Name: releaseservice.CRName}, releaseService); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxReleaseService status")
	}
	subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, releaseService, "release-service"))

	// Get and copy status from the KonfluxUI CR
	ui := &konfluxv1alpha1.KonfluxUI{}
	if err := r.Get(ctx, client.ObjectKey{Name: uictrl.CRName}, ui); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxUI status")
	}
	subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, ui, "ui"))

	// Propagate UI URL from KonfluxUI status to Konflux status
	if ui.Status.Ingress != nil {
		konflux.Status.UIURL = ui.Status.Ingress.URL
	}

	// Get and copy status from the KonfluxRBAC CR
	konfluxRBAC := &konfluxv1alpha1.KonfluxRBAC{}
	if err := r.Get(ctx, client.ObjectKey{Name: rbac.CRName}, konfluxRBAC); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxRBAC status")
	}
	subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, konfluxRBAC, "rbac"))

	// Get and copy status from the KonfluxInfo CR
	konfluxInfo := &konfluxv1alpha1.KonfluxInfo{}
	if err := r.Get(ctx, client.ObjectKey{Name: info.CRName}, konfluxInfo); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxInfo status")
	}
	subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, konfluxInfo, "info"))

	// Get and copy status from the KonfluxNamespaceLister CR
	konfluxNamespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{}
	if err := r.Get(ctx, client.ObjectKey{Name: namespacelister.CRName}, konfluxNamespaceLister); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxNamespaceLister status")
	}
	subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, konfluxNamespaceLister, "namespace-lister"))

	// Get and copy status from the KonfluxEnterpriseContract CR
	konfluxEnterpriseContract := &konfluxv1alpha1.KonfluxEnterpriseContract{}
	if err := r.Get(ctx, client.ObjectKey{Name: enterprisecontract.CRName}, konfluxEnterpriseContract); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxEnterpriseContract status")
	}
	subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, konfluxEnterpriseContract, "enterprise-contract"))

	// Get and copy status from the KonfluxApplicationAPI CR
	applicationAPI := &konfluxv1alpha1.KonfluxApplicationAPI{}
	if err := r.Get(ctx, client.ObjectKey{Name: applicationapi.CRName}, applicationAPI); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxApplicationAPI status")
	}
	subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, applicationAPI, "application-api"))

	// Get and copy status from the KonfluxImageController CR (if enabled)
	if konflux.Spec.IsImageControllerEnabled() {
		imageController := &konfluxv1alpha1.KonfluxImageController{}
		if err := r.Get(ctx, client.ObjectKey{Name: imagecontroller.CRName}, imageController); err != nil {
			return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxImageController status")
		}
		subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, imageController, "image-controller"))
	}

	// Get and copy status from the KonfluxCertManager CR
	certManager := &konfluxv1alpha1.KonfluxCertManager{}
	if err := r.Get(ctx, client.ObjectKey{Name: certmanager.CRName}, certManager); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxCertManager status")
	}
	subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, certManager, "cert-manager"))

	// Get and copy status from the KonfluxInternalRegistry CR (if enabled)
	if konflux.Spec.IsInternalRegistryEnabled() {
		registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
		if err := r.Get(ctx, client.ObjectKey{Name: internalregistry.CRName}, registry); err != nil {
			return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxInternalRegistry status")
		}
		subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, registry, "internal-registry"))
	}

	// Get and copy status from the KonfluxDefaultTenant CR (if enabled)
	if konflux.Spec.IsDefaultTenantEnabled() {
		defaultTenantCR := &konfluxv1alpha1.KonfluxDefaultTenant{}
		if err := r.Get(ctx, client.ObjectKey{Name: defaulttenant.CRName}, defaultTenantCR); err != nil {
			return errHandler.HandleWithReason(ctx, err, condition.ReasonSubCRStatusFailed, "get KonfluxDefaultTenant status")
		}
		subCRStatuses = append(subCRStatuses, condition.CopySubCRStatus(konflux, defaultTenantCR, "default-tenant"))
	}

	// Set overall Ready condition based on all sub-CRs.
	// All deployments are managed by component-specific reconcilers, so we only aggregate sub-CR statuses.
	condition.SetAggregatedReadyCondition(konflux, subCRStatuses)

	// Check cert-manager availability, set CertManagerAvailable condition, and override Ready if missing.
	certManagerResult := r.checkCertManagerAvailability(ctx, konflux)

	// Update the status subresource with all collected conditions
	if err := r.Status().Update(ctx, konflux); err != nil {
		log.Error(err, "Failed to update Konflux status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled Konflux")

	// Requeue when cert-manager check failed (transient error) or cert-manager is missing,
	// so we periodically re-run the check and status self-heals when cert-manager is installed.
	return certManagerResult, nil
}

// checkCertManagerAvailability checks if cert-manager CRDs are installed, sets the
// CertManagerAvailable condition on the Konflux CR, and overrides Ready to False
// when cert-manager is explicitly missing. Returns a ctrl.Result to requeue when
// the check should be retried (transient error) or when cert-manager is missing.
// Call this after SetAggregatedReadyCondition so the override applies to the aggregated Ready.
func (r *KonfluxReconciler) checkCertManagerAvailability(
	ctx context.Context, konflux *konfluxv1alpha1.Konflux,
) ctrl.Result {
	log := logf.FromContext(ctx)
	var result ctrl.Result
	certManagerInstalled, err := r.ClusterInfo.HasCertManager()
	if err != nil {
		log.Error(err, "Failed to check if cert-manager is installed")
		// Set condition to Unknown since we couldn't determine availability
		// This allows Ready to remain True if sub-CRs are ready, since we don't
		// know for certain that cert-manager is missing.
		condition.SetCondition(konflux, metav1.Condition{
			Type:    constant.ConditionTypeCertManagerAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  condition.ReasonCertManagerInstallationCheckFailed,
			Message: "Failed to check cert-manager availability: " + err.Error(),
		})
		result = ctrl.Result{RequeueAfter: 30 * time.Second}
	} else if !certManagerInstalled {
		log.Info("cert-manager CRDs not found - some components may fail to create Certificate resources")
		condition.SetCondition(konflux, metav1.Condition{
			Type:    constant.ConditionTypeCertManagerAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  condition.ReasonCertManagerNotInstalled,
			Message: "cert-manager CRDs are not installed. Several Konflux components require cert-manager to create Certificate resources for TLS. Please install cert-manager before proceeding.",
		})
		result = ctrl.Result{RequeueAfter: 1 * time.Minute}
	} else {
		condition.SetCondition(konflux, metav1.Condition{
			Type:    constant.ConditionTypeCertManagerAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  condition.ReasonCertManagerInstalled,
			Message: "cert-manager CRDs are installed",
		})
		result = ctrl.Result{}
	}
	// Override Ready to False when cert-manager is explicitly missing (Unknown is allowed).
	condition.OverrideReadyIfDependencyFalse(konflux, []condition.DependencyOverride{
		{
			ConditionType: constant.ConditionTypeCertManagerAvailable,
			Reason:        condition.ReasonCertManagerNotInstalled,
			Message:       "cert-manager CRDs are not installed. Some components require cert-manager to function properly.",
		},
	})
	return result
}

// applyKonfluxBuildService creates or updates the KonfluxBuildService CR.
func (r *KonfluxReconciler) applyKonfluxBuildService(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	var spec konfluxv1alpha1.KonfluxBuildServiceSpec
	if owner.Spec.KonfluxBuildService != nil && owner.Spec.KonfluxBuildService.Spec != nil {
		spec = *owner.Spec.KonfluxBuildService.Spec
	}

	buildService := &konfluxv1alpha1.KonfluxBuildService{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxBuildService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: buildservice.CRName,
		},
		Spec: spec,
	}

	log.Info("Applying KonfluxBuildService CR", "name", buildService.Name)
	return tc.ApplyOwned(ctx, buildService)
}

// applyKonfluxIntegrationService creates or updates the KonfluxIntegrationService CR.
func (r *KonfluxReconciler) applyKonfluxIntegrationService(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	var spec konfluxv1alpha1.KonfluxIntegrationServiceSpec
	if owner.Spec.KonfluxIntegrationService != nil && owner.Spec.KonfluxIntegrationService.Spec != nil {
		spec = *owner.Spec.KonfluxIntegrationService.Spec
	}

	integrationService := &konfluxv1alpha1.KonfluxIntegrationService{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxIntegrationService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: integrationservice.CRName,
		},
		Spec: spec,
	}

	log.Info("Applying KonfluxIntegrationService CR", "name", integrationService.Name)
	return tc.ApplyOwned(ctx, integrationService)
}

// applyKonfluxReleaseService creates or updates the KonfluxReleaseService CR.
func (r *KonfluxReconciler) applyKonfluxReleaseService(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	var spec konfluxv1alpha1.KonfluxReleaseServiceSpec
	if owner.Spec.KonfluxReleaseService != nil && owner.Spec.KonfluxReleaseService.Spec != nil {
		spec = *owner.Spec.KonfluxReleaseService.Spec
	}

	releaseService := &konfluxv1alpha1.KonfluxReleaseService{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxReleaseService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: releaseservice.CRName,
		},
		Spec: spec,
	}

	log.Info("Applying KonfluxReleaseService CR", "name", releaseService.Name)
	return tc.ApplyOwned(ctx, releaseService)
}

// applyKonfluxUI creates or updates the KonfluxUI CR.
func (r *KonfluxReconciler) applyKonfluxUI(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)
	var spec konfluxv1alpha1.KonfluxUISpec
	if owner.Spec.KonfluxUI != nil && owner.Spec.KonfluxUI.Spec != nil {
		spec = *owner.Spec.KonfluxUI.Spec
	}

	ui := &konfluxv1alpha1.KonfluxUI{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxUI",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: uictrl.CRName,
		},
		Spec: spec,
	}

	log.Info("Applying KonfluxUI CR", "name", ui.Name)
	return tc.ApplyOwned(ctx, ui)
}

// applyKonfluxRBAC creates or updates the KonfluxRBAC CR.
func (r *KonfluxReconciler) applyKonfluxRBAC(ctx context.Context, tc *tracking.Client) error {
	log := logf.FromContext(ctx)

	konfluxRBAC := &konfluxv1alpha1.KonfluxRBAC{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxRBAC",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: rbac.CRName,
		},
	}

	log.Info("Applying KonfluxRBAC CR", "name", konfluxRBAC.Name)
	return tc.ApplyOwned(ctx, konfluxRBAC)
}

// applyKonfluxInfo creates or updates the KonfluxInfo CR.
func (r *KonfluxReconciler) applyKonfluxInfo(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	var spec konfluxv1alpha1.KonfluxInfoSpec
	if owner.Spec.KonfluxInfo != nil && owner.Spec.KonfluxInfo.Spec != nil {
		spec = *owner.Spec.KonfluxInfo.Spec
	}

	// Normalize Banner field to prevent empty banner array from being serialized
	if spec.Banner != nil && (spec.Banner.Items == nil || len(*spec.Banner.Items) == 0) {
		spec.Banner = nil
	}

	konfluxInfo := &konfluxv1alpha1.KonfluxInfo{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxInfo",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: info.CRName,
		},
		Spec: spec,
	}

	log.Info("Applying KonfluxInfo CR", "name", konfluxInfo.Name)
	return tc.ApplyOwned(ctx, konfluxInfo)
}

// applyKonfluxNamespaceLister creates or updates the KonfluxNamespaceLister CR.
func (r *KonfluxReconciler) applyKonfluxNamespaceLister(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	var spec konfluxv1alpha1.KonfluxNamespaceListerSpec
	if owner.Spec.NamespaceLister != nil && owner.Spec.NamespaceLister.Spec != nil {
		spec = *owner.Spec.NamespaceLister.Spec
	}

	konfluxNamespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxNamespaceLister",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namespacelister.CRName,
		},
		Spec: spec,
	}

	log.Info("Applying KonfluxNamespaceLister CR", "name", konfluxNamespaceLister.Name)
	return tc.ApplyOwned(ctx, konfluxNamespaceLister)
}

// applyKonfluxEnterpriseContract creates or updates the KonfluxEnterpriseContract CR.
func (r *KonfluxReconciler) applyKonfluxEnterpriseContract(ctx context.Context, tc *tracking.Client) error {
	log := logf.FromContext(ctx)

	konfluxEnterpriseContract := &konfluxv1alpha1.KonfluxEnterpriseContract{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxEnterpriseContract",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: enterprisecontract.CRName,
		},
	}

	log.Info("Applying KonfluxEnterpriseContract CR", "name", konfluxEnterpriseContract.Name)
	return tc.ApplyOwned(ctx, konfluxEnterpriseContract)
}

// applyKonfluxApplicationAPI creates or updates the KonfluxApplicationAPI CR.
func (r *KonfluxReconciler) applyKonfluxApplicationAPI(ctx context.Context, tc *tracking.Client) error {
	log := logf.FromContext(ctx)

	applicationAPI := &konfluxv1alpha1.KonfluxApplicationAPI{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxApplicationAPI",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: applicationapi.CRName,
		},
	}

	log.Info("Applying KonfluxApplicationAPI CR", "name", applicationAPI.Name)
	return tc.ApplyOwned(ctx, applicationAPI)
}

// applyKonfluxImageController creates or updates the KonfluxImageController CR.
// The caller is responsible for checking if image-controller is enabled.
func (r *KonfluxReconciler) applyKonfluxImageController(ctx context.Context, tc *tracking.Client) error {
	log := logf.FromContext(ctx)

	imageController := &konfluxv1alpha1.KonfluxImageController{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxImageController",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imagecontroller.CRName,
		},
	}

	log.Info("Applying KonfluxImageController CR", "name", imageController.Name)
	return tc.ApplyOwned(ctx, imageController)
}

// applyKonfluxCertManager creates or updates the KonfluxCertManager CR.
func (r *KonfluxReconciler) applyKonfluxCertManager(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	var spec konfluxv1alpha1.KonfluxCertManagerSpec
	if owner.Spec.CertManager != nil {
		spec.CreateClusterIssuer = owner.Spec.CertManager.CreateClusterIssuer
	}

	certManager := &konfluxv1alpha1.KonfluxCertManager{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxCertManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: certmanager.CRName,
		},
		Spec: spec,
	}

	log.Info("Applying KonfluxCertManager CR", "name", certManager.Name)
	return tc.ApplyOwned(ctx, certManager)
}

// applyKonfluxInternalRegistry creates or updates the KonfluxInternalRegistry CR.
// The caller is responsible for checking if internal registry is enabled.
func (r *KonfluxReconciler) applyKonfluxInternalRegistry(ctx context.Context, tc *tracking.Client) error {
	log := logf.FromContext(ctx)

	registry := &konfluxv1alpha1.KonfluxInternalRegistry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxInternalRegistry",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: internalregistry.CRName,
		},
	}

	log.Info("Applying KonfluxInternalRegistry CR", "name", registry.Name)
	return tc.ApplyOwned(ctx, registry)
}

// applyKonfluxDefaultTenant creates or updates the KonfluxDefaultTenant CR.
// The caller is responsible for checking if default tenant is enabled.
func (r *KonfluxReconciler) applyKonfluxDefaultTenant(ctx context.Context, tc *tracking.Client) error {
	log := logf.FromContext(ctx)

	defaultTenantCR := &konfluxv1alpha1.KonfluxDefaultTenant{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxDefaultTenant",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: defaulttenant.CRName,
		},
	}

	log.Info("Applying KonfluxDefaultTenant CR", "name", defaultTenantCR.Name)
	return tc.ApplyOwned(ctx, defaultTenantCR)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.Konflux{}).
		Named("konflux").
		// Watch sub-CRs for status changes to aggregate conditions on the parent Konflux CR
		// All resource management (Deployments, Services, etc.) is handled by component-specific reconcilers
		// Watch KonfluxBuildService for any changes to copy conditions to Konflux CR
		// No predicate needed - the For() GenerationChangedPredicate prevents self-triggering loops
		Owns(&konfluxv1alpha1.KonfluxBuildService{}).
		// Watch KonfluxIntegrationService for any changes to copy conditions to Konflux CR
		Owns(&konfluxv1alpha1.KonfluxIntegrationService{}).
		// Watch KonfluxReleaseService for any changes to copy conditions to Konflux CR
		Owns(&konfluxv1alpha1.KonfluxReleaseService{}).
		// Watch KonfluxUI for any changes to copy conditions to Konflux CR
		Owns(&konfluxv1alpha1.KonfluxUI{}).
		// Watch KonfluxRBAC for any changes to copy conditions to Konflux CR
		// No predicate needed - the For() GenerationChangedPredicate prevents self-triggering loops
		Owns(&konfluxv1alpha1.KonfluxRBAC{}).
		// Watch KonfluxInfo for any changes to copy conditions to Konflux CR
		// No predicate needed - the For() GenerationChangedPredicate prevents self-triggering loops
		Owns(&konfluxv1alpha1.KonfluxInfo{}).
		// Watch KonfluxNamespaceLister for any changes to copy conditions to Konflux CR
		// No predicate needed - the For() GenerationChangedPredicate prevents self-triggering loops
		Owns(&konfluxv1alpha1.KonfluxNamespaceLister{}).
		// Watch KonfluxEnterpriseContract for any changes to copy conditions to Konflux CR
		// No predicate needed - the For() GenerationChangedPredicate prevents self-triggering loops
		Owns(&konfluxv1alpha1.KonfluxEnterpriseContract{}).
		// Watch KonfluxApplicationAPI for any changes to copy conditions to Konflux CR
		// No predicate needed - the For() GenerationChangedPredicate prevents self-triggering loops
		Owns(&konfluxv1alpha1.KonfluxApplicationAPI{}).
		// Watch KonfluxImageController for any changes to copy conditions to Konflux CR
		// No predicate needed - the For() GenerationChangedPredicate prevents self-triggering loops
		Owns(&konfluxv1alpha1.KonfluxImageController{}).
		// Watch KonfluxCertManager for any changes to copy conditions to Konflux CR
		// No predicate needed - the For() GenerationChangedPredicate prevents self-triggering loops
		Owns(&konfluxv1alpha1.KonfluxCertManager{}).
		// Watch KonfluxInternalRegistry for any changes to copy conditions to Konflux CR
		// No predicate needed - the For() GenerationChangedPredicate prevents self-triggering loops
		Owns(&konfluxv1alpha1.KonfluxInternalRegistry{}).
		// Watch KonfluxDefaultTenant for any changes to copy conditions to Konflux CR
		// No predicate needed - the For() GenerationChangedPredicate prevents self-triggering loops
		Owns(&konfluxv1alpha1.KonfluxDefaultTenant{}).
		Complete(r)
}
