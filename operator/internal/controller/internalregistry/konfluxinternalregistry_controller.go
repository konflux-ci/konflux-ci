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

package internalregistry

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/predicate"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxInternalRegistry CR.
	CRName = "konflux-internal-registry"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-internal-registry-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxInternalRegistry"
)

// InternalRegistryCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. All resources managed by this controller are always
// applied, so no cleanup GVKs are needed (they're always tracked and never become orphans).
var InternalRegistryCleanupGVKs = []schema.GroupVersionKind{}

// InternalRegistryClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. This is a security measure to prevent attackers from
// triggering deletion of arbitrary cluster resources by adding the owner label.
// InternalRegistryClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. All cluster-scoped resources managed by this controller are always
// applied, so no allow list is needed (they're always tracked and never become orphans).
var InternalRegistryClusterScopedAllowList tracking.ClusterScopedAllowList = nil

// KonfluxInternalRegistryReconciler reconciles a KonfluxInternalRegistry object
type KonfluxInternalRegistryReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxinternalregistries,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxinternalregistries/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxinternalregistries/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=trust.cert-manager.io,resources=bundles,verbs=get;list;watch;create;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *KonfluxInternalRegistryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxInternalRegistry instance
	registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
	if err := r.Get(ctx, req.NamespacedName, registry); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxInternalRegistry", "name", registry.Name)

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), registry, crKind)

	// Create a tracking client with ownership config for this reconcile.
	// Resources applied through this client are automatically tracked and owned.
	// At the end of a successful reconcile, orphaned resources are cleaned up.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             registry,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.Registry),
		FieldManager:      FieldManager,
	})

	// Apply manifests (if CR exists, it's enabled)
	if err := r.applyManifests(ctx, tc); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Cleanup orphaned resources - delete any resources with our owner label
	// that weren't applied during this reconcile. This handles the case where
	// enabled changes from true to false (resources are automatically deleted).
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, registry.Name, InternalRegistryCleanupGVKs,
		tracking.WithClusterScopedAllowList(InternalRegistryClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Check the status of owned deployments and update KonfluxInternalRegistry status
	if err := condition.UpdateComponentStatuses(ctx, r.Client, registry); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update status
	if err := r.Status().Update(ctx, registry); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxInternalRegistry")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxInternalRegistryReconciler) applyManifests(ctx context.Context, tc *tracking.Client) error {
	log := logf.FromContext(ctx)

	objects, err := r.ObjectStore.GetForComponent(manifests.Registry)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for Registry: %w", err)
	}

	for _, obj := range objects {
		// Apply with ownership - automatically sets labels, owner reference, and tracks
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			// Only skip if it's specifically a "CRD not installed" error for trust-manager (Bundle).
			// This prevents masking real reconciliation failures like RBAC denials,
			// validation errors, or resource conflicts.
			// Note: cert-manager errors should propagate (CRDs are installed in test environment).
			if tracking.IsNoKindMatchError(err) {
				gvk := obj.GetObjectKind().GroupVersionKind()
				// Only ignore trust-manager (Bundle) errors, not cert-manager errors
				if gvk.Group == "trust.cert-manager.io" {
					log.Info("Skipping resource: CRD not installed (test environment)",
						"kind", gvk.Kind,
						"apiVersion", gvk.GroupVersion().String(),
						"namespace", obj.GetNamespace(),
						"name", obj.GetName(),
					)
					continue
				}
			}
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.Registry, err)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxInternalRegistryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxInternalRegistry{}).
		Named("konfluxinternalregistry").
		// Watch for changes to registry resources
		Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.DeploymentReadinessPredicate)).
		Owns(&corev1.Service{}, builder.WithPredicates(predicate.LabelsOrAnnotationsChangedPredicate)).
		Owns(&corev1.Namespace{}, builder.WithPredicates(predicate.LabelsOrAnnotationsChangedPredicate)).
		Complete(r)
}
