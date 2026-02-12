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

package namespacelister

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxNamespaceLister CR.
	CRName = "konflux-namespace-lister"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-namespacelister-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxNamespaceLister"

	// namespaceListerContainerName is the name of the namespace-lister container
	namespaceListerContainerName = "namespace-lister"
)

// NamespaceListerCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. All resources managed by this controller are always
// applied, so no cleanup GVKs are needed (they're always tracked and never become orphans).
var NamespaceListerCleanupGVKs = []schema.GroupVersionKind{}

// NamespaceListerClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. This is a security measure to prevent attackers from
// triggering deletion of arbitrary cluster resources by adding the owner label.
// NamespaceListerClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. All cluster-scoped resources managed by this controller are always
// applied, so no allow list is needed (they're always tracked and never become orphans).
var NamespaceListerClusterScopedAllowList tracking.ClusterScopedAllowList = nil

// KonfluxNamespaceListerReconciler reconciles a KonfluxNamespaceLister object
type KonfluxNamespaceListerReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxnamespacelisters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxnamespacelisters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxnamespacelisters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=services;serviceaccounts,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=namespace-lister-authorizer,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=namespace-lister-authorizer,verbs=bind
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=kyverno.io,resources=clusterpolicies,verbs=get;list;watch;create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the KonfluxNamespaceLister object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *KonfluxNamespaceListerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxNamespaceLister instance
	konfluxNamespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{}
	if err := r.Get(ctx, req.NamespacedName, konfluxNamespaceLister); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxNamespaceLister", "name", konfluxNamespaceLister.Name)

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), konfluxNamespaceLister, crKind)

	// Create a tracking client with ownership config for this reconcile.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             konfluxNamespaceLister,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.NamespaceLister),
		FieldManager:      FieldManager,
	})

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc, konfluxNamespaceLister); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Cleanup orphaned resources
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, konfluxNamespaceLister.Name, NamespaceListerCleanupGVKs,
		tracking.WithClusterScopedAllowList(NamespaceListerClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Check the status of owned deployments and update KonfluxNamespaceLister status
	if err := condition.UpdateComponentStatuses(ctx, r.Client, konfluxNamespaceLister); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update status
	if err := r.Status().Update(ctx, konfluxNamespaceLister); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxNamespaceLister")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxNamespaceListerReconciler) applyManifests(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.KonfluxNamespaceLister) error {
	log := logf.FromContext(ctx)

	objects, err := r.ObjectStore.GetForComponent(manifests.NamespaceLister)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for NamespaceLister: %w", err)
	}

	for _, obj := range objects {
		// Apply customizations for deployments
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if err := applyNamespaceListerCustomizations(deployment, owner.Spec); err != nil {
				return fmt.Errorf("failed to apply customizations to deployment %s: %w", deployment.Name, err)
			}
		}

		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			gvk := obj.GetObjectKind().GroupVersionKind()
			// TODO: Remove this once we decide if we want to have a dependency on Kyverno
			if gvk.Group == constant.KyvernoGroup {
				log.Info("Skipping resource: CRD not installed",
					"kind", gvk.Kind,
					"apiVersion", gvk.GroupVersion().String(),
					"namespace", obj.GetNamespace(),
					"name", obj.GetName(),
				)
				continue
			}
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.NamespaceLister, err)
		}
	}
	return nil
}

// applyNamespaceListerCustomizations applies user-defined customizations to the namespace-lister deployment.
func applyNamespaceListerCustomizations(deployment *appsv1.Deployment, spec konfluxv1alpha1.KonfluxNamespaceListerSpec) error {
	if spec.NamespaceLister == nil {
		return nil
	}

	deploymentSpec := spec.NamespaceLister

	// Apply replicas if set (non-zero value)
	if deploymentSpec.Replicas > 0 {
		deployment.Spec.Replicas = &deploymentSpec.Replicas
	}

	// Build and apply container customizations using pkg/customization
	overlay := customization.BuildPodOverlay(
		customization.DeploymentContext{},
		customization.WithContainerBuilder(
			namespaceListerContainerName,
			customization.FromContainerSpec(deploymentSpec.NamespaceLister),
		),
	)
	return overlay.ApplyToDeployment(deployment)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxNamespaceListerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxNamespaceLister{}).
		Named("konfluxnamespacelister").
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
		Complete(r)
}
