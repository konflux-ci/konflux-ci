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

package controller

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// KonfluxOwnerLabel is the label used to identify resources owned by the Konflux operator.
	// Resources are owned either directly by the Konflux CR (e.g., ApplicationAPI CRDs) or
	// indirectly via component-specific CRs (e.g., deployments owned by KonfluxImageController CR).
	KonfluxOwnerLabel = "konflux.konflux-ci.dev/owner"
	// KonfluxComponentLabel is the label used to identify which component a resource belongs to.
	KonfluxComponentLabel = "konflux.konflux-ci.dev/component"
	// KonfluxCRName is the singleton name for the Konflux CR.
	KonfluxCRName = "konflux"
	// ConditionTypeReady is the condition type for overall readiness
	ConditionTypeReady = "Ready"
	// KonfluxBuildServiceCRName is the name for the KonfluxBuildService CR.
	KonfluxBuildServiceCRName = "konflux-build-service"
	// KonfluxIntegrationServiceCRName is the name for the KonfluxIntegrationService CR.
	KonfluxIntegrationServiceCRName = "konflux-integration-service"
	// KonfluxReleaseServiceCRName is the name for the KonfluxReleaseService CR.
	KonfluxReleaseServiceCRName = "konflux-release-service"
	// KonfluxUICRName is the namespace for UI resources
	KonfluxUICRName = "konflux-ui"
	// KonfluxRBACCRName is the name for the KonfluxRBAC CR.
	KonfluxRBACCRName = "konflux-rbac"
	// KonfluxNamespaceListerCRName is the name for the KonfluxNamespaceLister CR.
	KonfluxNamespaceListerCRName = "konflux-namespace-lister"
	// KonfluxEnterpriseContractCRName is the name for the KonfluxEnterpriseContract CR.
	KonfluxEnterpriseContractCRName = "konflux-enterprise-contract"
	// KonfluxImageControllerCRName is the name for the KonfluxImageController CR.
	KonfluxImageControllerCRName = "konflux-image-controller"
	// KonfluxApplicationAPICRName is the name for the KonfluxApplicationAPI CR.
	KonfluxApplicationAPICRName = "konflux-application-api"
	// KonfluxInfoCRName is the name for the KonfluxInfo CR.
	KonfluxInfoCRName = "konflux-info"
	// KonfluxCertManagerCRName is the name for the KonfluxCertManager CR.
	KonfluxCertManagerCRName = "konflux-cert-manager"
	// KonfluxInternalRegistryCRName is the name for the KonfluxInternalRegistry CR.
	KonfluxInternalRegistryCRName = "konflux-internal-registry"
	// CertManagerGroup is the API group for cert-manager resources
	CertManagerGroup = "cert-manager.io"
	// KyvernoGroup is the API group for Kyverno resources
	KyvernoGroup = "kyverno.io"

	// Field manager identifiers for server-side apply.
	// Each controller uses a unique field manager to make it clear which controller
	// manages which fields when inspecting managedFields on resources.
	FieldManagerKonflux            = "konflux-controller"
	FieldManagerBuildService       = "konflux-buildservice-controller"
	FieldManagerIntegrationService = "konflux-integrationservice-controller"
	FieldManagerReleaseService     = "konflux-releaseservice-controller"
	FieldManagerUI                 = "konflux-ui-controller"
	FieldManagerRBAC               = "konflux-rbac-controller"
	FieldManagerNamespaceLister    = "konflux-namespacelister-controller"
	FieldManagerImageController    = "konflux-imagecontroller-controller"
	FieldManagerEnterpriseContract = "konflux-enterprisecontract-controller"
	FieldManagerApplicationAPI     = "konflux-applicationapi-controller"
	FieldManagerInfo               = "konflux-info-controller"
)

// konfluxCleanupGVKs defines which sub-CR types should be cleaned up when they are
// no longer part of the desired state. This handles optional components like
// ImageController and InternalRegistry that can be enabled/disabled.
var konfluxCleanupGVKs = []schema.GroupVersionKind{
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxBuildService"},
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxIntegrationService"},
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxReleaseService"},
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxUI"},
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxRBAC"},
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxInfo"},
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxNamespaceLister"},
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxEnterpriseContract"},
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxApplicationAPI"},
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxImageController"},
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxCertManager"},
	{Group: konfluxv1alpha1.GroupVersion.Group, Version: konfluxv1alpha1.GroupVersion.Version, Kind: "KonfluxInternalRegistry"},
}

// KonfluxReconciler reconciles a Konflux object
type KonfluxReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

	// Initialize tracking client for declarative resource management
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             konflux,
		OwnerLabelKey:     KonfluxOwnerLabel,
		ComponentLabelKey: KonfluxComponentLabel,
		Component:         "konflux",
		FieldManager:      FieldManagerKonflux,
	})

	// Apply the KonfluxApplicationAPI CR
	if err := r.applyKonfluxApplicationAPI(ctx, tc); err != nil {
		log.Error(err, "Failed to apply KonfluxApplicationAPI")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxBuildService CR
	if err := r.applyKonfluxBuildService(ctx, tc, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxBuildService")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxIntegrationService CR
	if err := r.applyKonfluxIntegrationService(ctx, tc, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxIntegrationService")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxReleaseService CR
	if err := r.applyKonfluxReleaseService(ctx, tc, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxReleaseService")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxUI CR
	if err := r.applyKonfluxUI(ctx, tc, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxUI")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxRBAC CR
	if err := r.applyKonfluxRBAC(ctx, tc); err != nil {
		log.Error(err, "Failed to apply KonfluxRBAC")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxInfo CR
	if err := r.applyKonfluxInfo(ctx, tc, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxInfo")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxNamespaceLister CR
	if err := r.applyKonfluxNamespaceLister(ctx, tc, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxNamespaceLister")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxEnterpriseContract CR
	if err := r.applyKonfluxEnterpriseContract(ctx, tc); err != nil {
		log.Error(err, "Failed to apply KonfluxEnterpriseContract")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxImageController CR (only if enabled)
	if konflux.Spec.IsImageControllerEnabled() {
		if err := r.applyKonfluxImageController(ctx, tc); err != nil {
			log.Error(err, "Failed to apply KonfluxImageController")
			SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
			if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
				log.Error(updateErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
	}

	// Apply the KonfluxCertManager CR
	if err := r.applyKonfluxCertManager(ctx, tc, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxCertManager")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxInternalRegistry CR (only if enabled)
	if konflux.Spec.IsInternalRegistryEnabled() {
		if err := r.applyKonfluxInternalRegistry(ctx, tc); err != nil {
			log.Error(err, "Failed to apply KonfluxInternalRegistry")
			SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
			if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
				log.Error(updateErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
	}

	// Cleanup orphaned sub-CRs - delete any sub-CRs with our owner label
	// that weren't applied during this reconcile (e.g., disabled optional components)
	if err := tc.CleanupOrphans(ctx, KonfluxOwnerLabel, konflux.Name, konfluxCleanupGVKs); err != nil {
		log.Error(err, "Failed to cleanup orphaned sub-CRs")
		SetFailedCondition(konflux, ConditionTypeReady, "CleanupFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Collect status from all sub-CRs.
	// All component deployments are managed by their respective reconcilers,
	// so we aggregate readiness by checking each sub-CR's Ready condition.
	var subCRStatuses []SubCRStatus

	// Get and copy status from the KonfluxBuildService CR
	buildService := &konfluxv1alpha1.KonfluxBuildService{}
	if err := r.Get(ctx, client.ObjectKey{Name: KonfluxBuildServiceCRName}, buildService); err != nil {
		log.Error(err, "Failed to get KonfluxBuildService")
		SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetBuildServiceStatus", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, buildService, "build-service"))

	// Get and copy status from the KonfluxIntegrationService CR
	integrationService := &konfluxv1alpha1.KonfluxIntegrationService{}
	if err := r.Get(ctx, client.ObjectKey{Name: KonfluxIntegrationServiceCRName}, integrationService); err != nil {
		log.Error(err, "Failed to get KonfluxIntegrationService")
		SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetIntegrationServiceStatus", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, integrationService, "integration-service"))

	// Get and copy status from the KonfluxReleaseService CR
	releaseService := &konfluxv1alpha1.KonfluxReleaseService{}
	if err := r.Get(ctx, client.ObjectKey{Name: KonfluxReleaseServiceCRName}, releaseService); err != nil {
		log.Error(err, "Failed to get KonfluxReleaseService")
		SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetReleaseServiceStatus", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, releaseService, "release-service"))

	// Get and copy status from the KonfluxUI CR
	ui := &konfluxv1alpha1.KonfluxUI{}
	if err := r.Get(ctx, client.ObjectKey{Name: KonfluxUICRName}, ui); err != nil {
		log.Error(err, "Failed to get KonfluxUI")
		SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetUIStatus", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, ui, "ui"))

	// Propagate UI URL from KonfluxUI status to Konflux status
	if ui.Status.Ingress != nil {
		konflux.Status.UIURL = ui.Status.Ingress.URL
	}

	// Get and copy status from the KonfluxRBAC CR
	konfluxRBAC := &konfluxv1alpha1.KonfluxRBAC{}
	if err := r.Get(ctx, client.ObjectKey{Name: KonfluxRBACCRName}, konfluxRBAC); err != nil {
		log.Error(err, "Failed to get KonfluxRBAC")
		SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetRBACStatus", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, konfluxRBAC, "rbac"))

	// Get and copy status from the KonfluxInfo CR
	konfluxInfo := &konfluxv1alpha1.KonfluxInfo{}
	if err := r.Get(ctx, client.ObjectKey{Name: KonfluxInfoCRName}, konfluxInfo); err != nil {
		log.Error(err, "Failed to get KonfluxInfo")
		SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetInfoStatus", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, konfluxInfo, "info"))

	// Get and copy status from the KonfluxNamespaceLister CR
	konfluxNamespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{}
	if err := r.Get(ctx, client.ObjectKey{Name: KonfluxNamespaceListerCRName}, konfluxNamespaceLister); err != nil {
		log.Error(err, "Failed to get KonfluxNamespaceLister")
		SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetNamespaceListerStatus", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, konfluxNamespaceLister, "namespace-lister"))

	// Get and copy status from the KonfluxEnterpriseContract CR
	konfluxEnterpriseContract := &konfluxv1alpha1.KonfluxEnterpriseContract{}
	if err := r.Get(ctx, client.ObjectKey{Name: KonfluxEnterpriseContractCRName}, konfluxEnterpriseContract); err != nil {
		log.Error(err, "Failed to get KonfluxEnterpriseContract")
		SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetEnterpriseContractStatus", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, konfluxEnterpriseContract, "enterprise-contract"))

	// Get and copy status from the KonfluxApplicationAPI CR
	applicationAPI := &konfluxv1alpha1.KonfluxApplicationAPI{}
	if err := r.Get(ctx, client.ObjectKey{Name: KonfluxApplicationAPICRName}, applicationAPI); err != nil {
		log.Error(err, "Failed to get KonfluxApplicationAPI")
		SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetApplicationAPIStatus", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, applicationAPI, "application-api"))

	// Get and copy status from the KonfluxImageController CR (if enabled)
	if konflux.Spec.IsImageControllerEnabled() {
		imageController := &konfluxv1alpha1.KonfluxImageController{}
		if err := r.Get(ctx, client.ObjectKey{Name: KonfluxImageControllerCRName}, imageController); err != nil {
			log.Error(err, "Failed to get KonfluxImageController")
			SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetImageControllerStatus", err)
			if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
				log.Error(updateErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, imageController, "image-controller"))
	}

	// Get and copy status from the KonfluxCertManager CR
	certManager := &konfluxv1alpha1.KonfluxCertManager{}
	if err := r.Get(ctx, client.ObjectKey{Name: KonfluxCertManagerCRName}, certManager); err != nil {
		log.Error(err, "Failed to get KonfluxCertManager")
		SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetCertManagerStatus", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, certManager, "cert-manager"))

	// Get and copy status from the KonfluxInternalRegistry CR (if enabled)
	if konflux.Spec.IsInternalRegistryEnabled() {
		registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
		if err := r.Get(ctx, client.ObjectKey{Name: KonfluxInternalRegistryCRName}, registry); err != nil {
			log.Error(err, "Failed to get KonfluxInternalRegistry")
			SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetInternalRegistryStatus", err)
			if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
				log.Error(updateErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		subCRStatuses = append(subCRStatuses, CopySubCRStatus(konflux, registry, "internal-registry"))
	}

	// Set overall Ready condition based on all sub-CRs.
	// All deployments are managed by component-specific reconcilers, so we only aggregate sub-CR statuses.
	SetAggregatedReadyCondition(konflux, ConditionTypeReady, subCRStatuses)

	// Update the status subresource with all collected conditions
	if err := r.Status().Update(ctx, konflux); err != nil {
		log.Error(err, "Failed to update Konflux status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled Konflux")
	return ctrl.Result{}, nil
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
			Name: KonfluxBuildServiceCRName,
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
			Name: KonfluxIntegrationServiceCRName,
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
			Name: KonfluxReleaseServiceCRName,
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
			Name: KonfluxUICRName,
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
			Name: KonfluxRBACCRName,
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
			Name: KonfluxInfoCRName,
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
			Name: KonfluxNamespaceListerCRName,
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
			Name: KonfluxEnterpriseContractCRName,
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
			Name: KonfluxApplicationAPICRName,
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
			Name: KonfluxImageControllerCRName,
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
			Name: KonfluxCertManagerCRName,
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
			Name: KonfluxInternalRegistryCRName,
		},
	}

	log.Info("Applying KonfluxInternalRegistry CR", "name", registry.Name)
	return tc.ApplyOwned(ctx, registry)
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
		Complete(r)
}
