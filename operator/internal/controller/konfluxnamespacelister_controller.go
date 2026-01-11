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
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// NamespaceListerConditionTypeReady is the condition type for overall readiness
	NamespaceListerConditionTypeReady = "Ready"

	// namespaceListerContainerName is the name of the namespace-lister container
	namespaceListerContainerName = "namespace-lister"
)

// namespaceListerCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state for the NamespaceLister component.
var namespaceListerCleanupGVKs = []schema.GroupVersionKind{
	{Group: "apps", Version: "v1", Kind: "Deployment"},
	{Group: "", Version: "v1", Kind: "Service"},
	{Group: "", Version: "v1", Kind: "ServiceAccount"},
	{Group: "", Version: "v1", Kind: "Namespace"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
	{Group: "networking.k8s.io", Version: "v1", Kind: "NetworkPolicy"},
	{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"},
	{Group: "kyverno.io", Version: "v1", Kind: "ClusterPolicy"},
}

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

	// Create a tracking client with ownership config for this reconcile.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             konfluxNamespaceLister,
		OwnerLabelKey:     KonfluxOwnerLabel,
		ComponentLabelKey: KonfluxComponentLabel,
		Component:         string(manifests.NamespaceLister),
		FieldManager:      FieldManagerNamespaceLister,
	})

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc, konfluxNamespaceLister); err != nil {
		log.Error(err, "Failed to apply manifests")
		SetFailedCondition(konfluxNamespaceLister, NamespaceListerConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konfluxNamespaceLister); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Cleanup orphaned resources
	if err := tc.CleanupOrphans(ctx, KonfluxOwnerLabel, konfluxNamespaceLister.Name, namespaceListerCleanupGVKs); err != nil {
		log.Error(err, "Failed to cleanup orphaned resources")
		SetFailedCondition(konfluxNamespaceLister, NamespaceListerConditionTypeReady, "CleanupFailed", err)
		if updateErr := r.Status().Update(ctx, konfluxNamespaceLister); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Check the status of owned deployments and update KonfluxNamespaceLister status
	if err := UpdateComponentStatuses(ctx, r.Client, konfluxNamespaceLister, NamespaceListerConditionTypeReady); err != nil {
		log.Error(err, "Failed to update component statuses")
		SetFailedCondition(konfluxNamespaceLister, NamespaceListerConditionTypeReady, "FailedToGetDeploymentStatus", err)
		if updateErr := r.Status().Update(ctx, konfluxNamespaceLister); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
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
			if gvk.Group == CertManagerGroup || gvk.Group == KyvernoGroup {
				// TODO: Remove this once we decide how to install cert-manager crds in envtest
				// TODO: Remove this once we decide if we want to have a dependency on Kyverno
				log.Info("Skipping resource: CRD not installed",
					"kind", gvk.Kind,
					"apiVersion", gvk.GroupVersion().String(),
					"namespace", obj.GetNamespace(),
					"name", obj.GetName(),
				)
				continue
			}
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), getKind(obj), manifests.NamespaceLister, err)
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
		Owns(&appsv1.Deployment{}, builder.WithPredicates(deploymentReadinessPredicate)).
		Owns(&corev1.Service{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(labelsOrAnnotationsChangedPredicate)).
		Owns(&corev1.Secret{}, builder.WithPredicates(labelsOrAnnotationsChangedPredicate)).
		Owns(&corev1.Namespace{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&rbacv1.Role{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&rbacv1.RoleBinding{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&rbacv1.ClusterRole{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&rbacv1.ClusterRoleBinding{}, builder.WithPredicates(generationChangedPredicate)).
		Complete(r)
}
