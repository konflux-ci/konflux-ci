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

package releaseservice

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
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxReleaseService CR.
	CRName = "konflux-release-service"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-releaseservice-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxReleaseService"

	// Deployment names
	releaseControllerManagerDeploymentName = "release-service-controller-manager"

	// Container names
	releaseManagerContainerName = "manager"
)

// ReleaseServiceCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. All resources managed by this controller are always
// applied, so no cleanup GVKs are needed (they're always tracked and never become orphans).
var ReleaseServiceCleanupGVKs = []schema.GroupVersionKind{}

// ReleaseServiceClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. All cluster-scoped resources managed by this controller are always
// applied, so no allow list is needed (they're always tracked and never become orphans).
var ReleaseServiceClusterScopedAllowList tracking.ClusterScopedAllowList = nil

// KonfluxReleaseServiceReconciler reconciles a KonfluxReleaseService object
type KonfluxReleaseServiceReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxreleaseservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxreleaseservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxreleaseservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=services;secrets;serviceaccounts,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,resourceNames=release-service-leader-election-role,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,resourceNames=release-service-leader-election-rolebinding;releaseserviceconfigs-rolebinding,verbs=bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=release-pipeline-resource-role;release-service-application-role;release-service-component-role;release-service-environment-viewer-role;release-service-manager-role;release-service-metrics-auth-role;release-service-release-editor-role;release-service-release-viewer-role;release-service-releaseplan-editor-role;release-service-releaseplan-viewer-role;release-service-releaseplanadmission-editor-role;release-service-releaseplanadmission-viewer-role;release-service-snapshot-editor-role;release-service-snapshot-viewer-role;release-service-snapshotenvironmentbinding-editor-role;release-service-tekton-role;releaseserviceconfig-role,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=release-service-application-role-binding;release-service-component-role-binding;release-service-environment-role-binding;release-service-manager-rolebinding;release-service-metrics-auth-rolebinding;release-service-release-role-binding;release-service-releaseplan-role-binding;release-service-releaseplanadmission-role-binding;release-service-snapshot-role-binding;release-service-snapshotenvironmentbinding-role-binding;release-service-tekton-role-binding,verbs=bind
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates;issuers,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=appstudio.redhat.com,resources=releaseserviceconfigs,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;watch;create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *KonfluxReleaseServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxReleaseService instance
	releaseService := &konfluxv1alpha1.KonfluxReleaseService{}
	if err := r.Get(ctx, req.NamespacedName, releaseService); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxReleaseService", "name", releaseService.Name)

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), releaseService, crKind)

	// Create a tracking client with ownership config for this reconcile.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             releaseService,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.Release),
		FieldManager:      FieldManager,
	})

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc, releaseService); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Cleanup orphaned resources
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, releaseService.Name, ReleaseServiceCleanupGVKs,
		tracking.WithClusterScopedAllowList(ReleaseServiceClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Check the status of owned deployments and update KonfluxReleaseService status
	if err := condition.UpdateComponentStatuses(ctx, r.Client, releaseService); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update status
	if err := r.Status().Update(ctx, releaseService); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxReleaseService")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxReleaseServiceReconciler) applyManifests(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.KonfluxReleaseService) error {
	objects, err := r.ObjectStore.GetForComponent(manifests.Release)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for Release: %w", err)
	}

	for _, obj := range objects {
		// Apply customizations for deployments
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if err := applyReleaseServiceDeploymentCustomizations(deployment, owner.Spec); err != nil {
				return fmt.Errorf("failed to apply customizations to deployment %s: %w", deployment.Name, err)
			}
		}

		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.Release, err)
		}
	}
	return nil
}

// applyReleaseServiceDeploymentCustomizations applies user-defined customizations to ReleaseService deployments.
func applyReleaseServiceDeploymentCustomizations(deployment *appsv1.Deployment, spec konfluxv1alpha1.KonfluxReleaseServiceSpec) error {
	switch deployment.Name {
	case releaseControllerManagerDeploymentName:
		if spec.ReleaseControllerManager != nil {
			deployment.Spec.Replicas = &spec.ReleaseControllerManager.Replicas
		}
		if err := buildReleaseControllerManagerOverlay(spec.ReleaseControllerManager).ApplyToDeployment(deployment); err != nil {
			return err
		}
	}
	return nil
}

// buildReleaseControllerManagerOverlay builds the pod overlay for the controller-manager deployment.
func buildReleaseControllerManagerOverlay(spec *konfluxv1alpha1.ControllerManagerDeploymentSpec) *customization.PodOverlay {
	if spec == nil {
		return customization.NewPodOverlay()
	}

	return customization.BuildPodOverlay(
		customization.DeploymentContext{Replicas: spec.Replicas},
		customization.WithContainerBuilder(
			releaseManagerContainerName,
			customization.FromContainerSpec(spec.Manager),
			customization.WithLeaderElection(),
		),
	)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxReleaseServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	crdMapFunc, err := crdhandler.MapCRDToRequest(r.ObjectStore, manifests.Release, CRName)
	if err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxReleaseService{}).
		Named("konfluxreleaseservice").
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
