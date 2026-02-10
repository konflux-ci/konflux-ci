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

package imagecontroller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	crdhandler "github.com/konflux-ci/konflux-ci/operator/internal/controller/handler"
	"github.com/konflux-ci/konflux-ci/operator/internal/predicate"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxImageController CR.
	CRName = "konflux-image-controller"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-imagecontroller-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxImageController"
)

// ImageControllerCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. All resources managed by this controller are always
// applied, so no cleanup GVKs are needed (they're always tracked and never become orphans).
var ImageControllerCleanupGVKs = []schema.GroupVersionKind{}

// ImageControllerClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. All cluster-scoped resources managed by this controller are always
// applied, so no allow list is needed (they're always tracked and never become orphans).
var ImageControllerClusterScopedAllowList tracking.ClusterScopedAllowList = nil

// KonfluxImageControllerReconciler reconciles a KonfluxImageController object
type KonfluxImageControllerReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluximagecontrollers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluximagecontrollers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluximagecontrollers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=services;secrets;serviceaccounts,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,resourceNames=image-controller-leader-election-role,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,resourceNames=image-controller-leader-election-rolebinding,verbs=bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=image-controller-imagerepository-editor-role;image-controller-imagerepository-viewer-role;image-controller-manager-role;image-controller-metrics-auth-role,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=image-controller-manager-rolebinding;image-controller-metrics-auth-rolebinding,verbs=bind
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KonfluxImageControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxImageController instance
	imageController := &konfluxv1alpha1.KonfluxImageController{}
	if err := r.Get(ctx, req.NamespacedName, imageController); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxImageController", "name", imageController.Name)

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), imageController, crKind)

	// Create a tracking client with ownership config for this reconcile.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             imageController,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.ImageController),
		FieldManager:      FieldManager,
	})

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Cleanup orphaned resources
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, imageController.Name, ImageControllerCleanupGVKs,
		tracking.WithClusterScopedAllowList(ImageControllerClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Check the status of owned deployments and update KonfluxImageController status
	if err := condition.UpdateComponentStatuses(ctx, r.Client, imageController); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update status
	if err := r.Status().Update(ctx, imageController); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxImageController")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxImageControllerReconciler) applyManifests(ctx context.Context, tc *tracking.Client) error {
	objects, err := r.ObjectStore.GetForComponent(manifests.ImageController)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for ImageController: %w", err)
	}

	for _, obj := range objects {
		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.ImageController, err)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxImageControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	crdMapFunc, err := crdhandler.MapCRDToRequest(r.ObjectStore, manifests.ImageController, CRName)
	if err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxImageController{}).
		Named("konfluximagecontroller").
		// Use predicates to filter out unnecessary updates and prevent reconcile loops
		// Deployments: watch spec changes AND readiness status changes
		Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.DeploymentReadinessPredicate)).
		Owns(&corev1.Service{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(predicate.LabelsOrAnnotationsChangedPredicate)).
		Owns(&corev1.Secret{}, builder.WithPredicates(predicate.LabelsOrAnnotationsChangedPredicate)).
		Owns(&corev1.Namespace{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.Role{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.RoleBinding{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.ClusterRole{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.ClusterRoleBinding{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		// Watch CRDs so that out-of-band deletion triggers reconcile and re-apply.
		Watches(&apiextensionsv1.CustomResourceDefinition{},
			handler.EnqueueRequestsFromMapFunc(crdMapFunc)).
		Complete(r)
}
