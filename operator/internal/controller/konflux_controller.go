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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
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

	// Apply the KonfluxApplicationAPI CR
	if err := r.applyKonfluxApplicationAPI(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxApplicationAPI")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxBuildService CR
	if err := r.applyKonfluxBuildService(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxBuildService")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxIntegrationService CR
	if err := r.applyKonfluxIntegrationService(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxIntegrationService")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxReleaseService CR
	if err := r.applyKonfluxReleaseService(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxReleaseService")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxUI CR
	if err := r.applyKonfluxUI(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxUI")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxRBAC CR
	if err := r.applyKonfluxRBAC(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxRBAC")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxInfo CR
	if err := r.applyKonfluxInfo(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxInfo")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxNamespaceLister CR
	if err := r.applyKonfluxNamespaceLister(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxNamespaceLister")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxEnterpriseContract CR
	if err := r.applyKonfluxEnterpriseContract(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxEnterpriseContract")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply the KonfluxImageController CR (if enabled)
	if err := r.applyKonfluxImageController(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply KonfluxImageController")
		SetFailedCondition(konflux, ConditionTypeReady, "ApplyFailed", err)
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
func (r *KonfluxReconciler) applyKonfluxBuildService(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
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
			Labels: map[string]string{
				KonfluxOwnerLabel:     owner.Name,
				KonfluxComponentLabel: string(manifests.BuildService),
			},
		},
		Spec: spec,
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, buildService, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxBuildService: %w", err)
	}

	log.Info("Applying KonfluxBuildService CR", "name", buildService.Name)
	return r.Patch(ctx, buildService, client.Apply, client.FieldOwner(FieldManagerKonflux), client.ForceOwnership)
}

// applyKonfluxIntegrationService creates or updates the KonfluxIntegrationService CR.
func (r *KonfluxReconciler) applyKonfluxIntegrationService(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
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
			Labels: map[string]string{
				KonfluxOwnerLabel:     owner.Name,
				KonfluxComponentLabel: string(manifests.Integration),
			},
		},
		Spec: spec,
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, integrationService, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxIntegrationService: %w", err)
	}

	log.Info("Applying KonfluxIntegrationService CR", "name", integrationService.Name)
	return r.Patch(ctx, integrationService, client.Apply, client.FieldOwner(FieldManagerKonflux), client.ForceOwnership)
}

// applyKonfluxReleaseService creates or updates the KonfluxReleaseService CR.
func (r *KonfluxReconciler) applyKonfluxReleaseService(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
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
			Labels: map[string]string{
				KonfluxOwnerLabel:     owner.Name,
				KonfluxComponentLabel: string(manifests.Release),
			},
		},
		Spec: spec,
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, releaseService, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxReleaseService: %w", err)
	}

	log.Info("Applying KonfluxReleaseService CR", "name", releaseService.Name)
	return r.Patch(ctx, releaseService, client.Apply, client.FieldOwner(FieldManagerKonflux), client.ForceOwnership)
}

// applyKonfluxUI creates or updates the KonfluxUI CR.
func (r *KonfluxReconciler) applyKonfluxUI(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
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
			Labels: map[string]string{
				KonfluxOwnerLabel:     owner.Name,
				KonfluxComponentLabel: string(manifests.UI),
			},
		},
		Spec: spec,
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, ui, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxUI: %w", err)
	}

	log.Info("Applying KonfluxUI CR", "name", ui.Name)
	return r.Patch(ctx, ui, client.Apply, client.FieldOwner(FieldManagerKonflux), client.ForceOwnership)
}

// applyKonfluxRBAC creates or updates the KonfluxRBAC CR.
func (r *KonfluxReconciler) applyKonfluxRBAC(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	konfluxRBAC := &konfluxv1alpha1.KonfluxRBAC{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxRBAC",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: KonfluxRBACCRName,
			Labels: map[string]string{
				KonfluxOwnerLabel:     owner.Name,
				KonfluxComponentLabel: string(manifests.RBAC),
			},
		},
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, konfluxRBAC, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxRBAC: %w", err)
	}

	log.Info("Applying KonfluxRBAC CR", "name", konfluxRBAC.Name)
	return r.Patch(ctx, konfluxRBAC, client.Apply, client.FieldOwner(FieldManagerKonflux), client.ForceOwnership)
}

// applyKonfluxInfo creates or updates the KonfluxInfo CR.
func (r *KonfluxReconciler) applyKonfluxInfo(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
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
			Labels: map[string]string{
				KonfluxOwnerLabel:     owner.Name,
				KonfluxComponentLabel: string(manifests.Info),
			},
		},
		Spec: spec,
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, konfluxInfo, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxInfo: %w", err)
	}

	log.Info("Applying KonfluxInfo CR", "name", konfluxInfo.Name)
	return r.Patch(ctx, konfluxInfo, client.Apply, client.FieldOwner(FieldManagerKonflux), client.ForceOwnership)
}

// applyKonfluxNamespaceLister creates or updates the KonfluxNamespaceLister CR.
func (r *KonfluxReconciler) applyKonfluxNamespaceLister(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
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
			Labels: map[string]string{
				KonfluxOwnerLabel:     owner.Name,
				KonfluxComponentLabel: string(manifests.NamespaceLister),
			},
		},
		Spec: spec,
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, konfluxNamespaceLister, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxNamespaceLister: %w", err)
	}

	log.Info("Applying KonfluxNamespaceLister CR", "name", konfluxNamespaceLister.Name)
	return r.Patch(ctx, konfluxNamespaceLister, client.Apply, client.FieldOwner(FieldManagerKonflux), client.ForceOwnership)
}

// applyKonfluxEnterpriseContract creates or updates the KonfluxEnterpriseContract CR.
func (r *KonfluxReconciler) applyKonfluxEnterpriseContract(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	konfluxEnterpriseContract := &konfluxv1alpha1.KonfluxEnterpriseContract{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxEnterpriseContract",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: KonfluxEnterpriseContractCRName,
			Labels: map[string]string{
				KonfluxOwnerLabel:     owner.Name,
				KonfluxComponentLabel: string(manifests.EnterpriseContract),
			},
		},
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, konfluxEnterpriseContract, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxEnterpriseContract: %w", err)
	}

	log.Info("Applying KonfluxEnterpriseContract CR", "name", konfluxEnterpriseContract.Name)
	return r.Patch(ctx, konfluxEnterpriseContract, client.Apply, client.FieldOwner(FieldManagerKonflux), client.ForceOwnership)
}

// applyKonfluxApplicationAPI creates or updates the KonfluxApplicationAPI CR.
func (r *KonfluxReconciler) applyKonfluxApplicationAPI(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	applicationAPI := &konfluxv1alpha1.KonfluxApplicationAPI{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxApplicationAPI",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: KonfluxApplicationAPICRName,
			Labels: map[string]string{
				KonfluxOwnerLabel:     owner.Name,
				KonfluxComponentLabel: string(manifests.ApplicationAPI),
			},
		},
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, applicationAPI, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxApplicationAPI: %w", err)
	}

	log.Info("Applying KonfluxApplicationAPI CR", "name", applicationAPI.Name)
	return r.Patch(ctx, applicationAPI, client.Apply, client.FieldOwner(FieldManagerKonflux), client.ForceOwnership)
}

// applyKonfluxImageController creates or updates the KonfluxImageController CR if enabled,
// or deletes it if disabled.
func (r *KonfluxReconciler) applyKonfluxImageController(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	// Check if image-controller is enabled
	if !owner.Spec.IsImageControllerEnabled() {
		// Delete the CR if it exists (idempotent - NotFound is ignored)
		imageController := &konfluxv1alpha1.KonfluxImageController{
			ObjectMeta: metav1.ObjectMeta{
				Name: KonfluxImageControllerCRName,
			},
		}
		if err := r.Delete(ctx, imageController); err != nil {
			return client.IgnoreNotFound(err)
		}
		log.Info("Image-controller disabled, deleted KonfluxImageController CR", "name", KonfluxImageControllerCRName)
		return nil
	}

	// Create or update the CR
	imageController := &konfluxv1alpha1.KonfluxImageController{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konfluxv1alpha1.GroupVersion.String(),
			Kind:       "KonfluxImageController",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: KonfluxImageControllerCRName,
			Labels: map[string]string{
				KonfluxOwnerLabel:     owner.Name,
				KonfluxComponentLabel: string(manifests.ImageController),
			},
		},
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, imageController, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxImageController: %w", err)
	}

	log.Info("Applying KonfluxImageController CR", "name", imageController.Name)
	return r.Patch(ctx, imageController, client.Apply, client.FieldOwner(FieldManagerKonflux), client.ForceOwnership)
}

// getKind returns the Kind of a client.Object.
// For unstructured objects, it uses the GVK directly.
// For typed objects, it uses the GVK from the object's metadata.
func getKind(obj client.Object) string {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		return u.GetKind()
	}
	return obj.GetObjectKind().GroupVersionKind().Kind
}

// setOwnership sets owner reference and labels on the object to establish ownership.
func setOwnership(obj client.Object, owner client.Object, component string, scheme *runtime.Scheme) error {
	// Set ownership labels
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[KonfluxOwnerLabel] = owner.GetName()
	labels[KonfluxComponentLabel] = component
	obj.SetLabels(labels)

	// Set owner reference for garbage collection and watch triggers
	// Since Konflux CR is cluster-scoped, it can own both cluster-scoped and namespaced resources
	if err := controllerutil.SetControllerReference(owner, obj, scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	return nil
}

// applyObject applies a single object to the cluster using server-side apply.
// Server-side apply is idempotent and only triggers updates when there are actual changes,
// preventing reconcile loops when watching owned resources.
// The fieldManager parameter identifies which controller manages the fields being applied,
// making it clear when different reconcilers try to manage the same resource.
func applyObject(ctx context.Context, k8sClient client.Client, obj client.Object, fieldManager string) error {
	return k8sClient.Patch(ctx, obj, client.Apply, client.FieldOwner(fieldManager), client.ForceOwnership)
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
		Complete(r)
}
