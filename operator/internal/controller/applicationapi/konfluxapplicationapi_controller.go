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

package applicationapi

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxApplicationAPI CR.
	CRName = "konflux-application-api"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-applicationapi-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxApplicationAPI"
)

// ApplicationAPICleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state for the ApplicationAPI component.
// Note: This component only contains CRDs, which are excluded from cleanup.
var ApplicationAPICleanupGVKs = []schema.GroupVersionKind{}

// KonfluxApplicationAPIReconciler reconciles a KonfluxApplicationAPI object
type KonfluxApplicationAPIReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxapplicationapis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxapplicationapis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxapplicationapis/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=list
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KonfluxApplicationAPIReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxApplicationAPI instance
	applicationAPI := &konfluxv1alpha1.KonfluxApplicationAPI{}
	if err := r.Get(ctx, req.NamespacedName, applicationAPI); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxApplicationAPI", "name", applicationAPI.Name)

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), applicationAPI, crKind)

	// Create a tracking client with ownership config for this reconcile.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             applicationAPI,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.ApplicationAPI),
		FieldManager:      FieldManager,
	})

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Cleanup orphaned resources
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, applicationAPI.Name, ApplicationAPICleanupGVKs); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Check the status of owned deployments and update KonfluxApplicationAPI status
	if err := condition.UpdateComponentStatuses(ctx, r.Client, applicationAPI); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update status
	if err := r.Status().Update(ctx, applicationAPI); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxApplicationAPI")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxApplicationAPIReconciler) applyManifests(ctx context.Context, tc *tracking.Client) error {
	objects, err := r.ObjectStore.GetForComponent(manifests.ApplicationAPI)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for ApplicationAPI: %w", err)
	}

	for _, obj := range objects {
		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.ApplicationAPI, err)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxApplicationAPIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxApplicationAPI{}).
		Named("konfluxapplicationapi").
		// ApplicationAPI only installs CRDs, so no need to watch Deployments, Services, etc.
		// The reconciler will reapply CRDs if they are deleted.
		Complete(r)
}
