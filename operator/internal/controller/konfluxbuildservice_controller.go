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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const (
	// BuildServiceConditionTypeReady is the condition type for overall readiness
	BuildServiceConditionTypeReady = "Ready"
)

// KonfluxBuildServiceReconciler reconciles a KonfluxBuildService object
type KonfluxBuildServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxbuildservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxbuildservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxbuildservices/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the KonfluxBuildService object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *KonfluxBuildServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxBuildService instance
	buildService := &konfluxv1alpha1.KonfluxBuildService{}
	if err := r.Get(ctx, req.NamespacedName, buildService); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxBuildService", "name", buildService.Name)

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, buildService); err != nil {
		log.Error(err, "Failed to apply manifests")
		SetFailedCondition(buildService, BuildServiceConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, buildService); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Check the status of owned deployments and update KonfluxBuildService status
	if err := r.updateComponentStatuses(ctx, buildService); err != nil {
		log.Error(err, "Failed to update component statuses")
		SetFailedCondition(buildService, BuildServiceConditionTypeReady, "FailedToGetDeploymentStatus", err)
		if updateErr := r.Status().Update(ctx, buildService); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxBuildService")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster.
func (r *KonfluxBuildServiceReconciler) applyManifests(ctx context.Context, owner *konfluxv1alpha1.KonfluxBuildService) error {
	log := logf.FromContext(ctx)

	buildServiceManifest, err := manifests.GetManifest(manifests.BuildService)
	if err != nil {
		return fmt.Errorf("failed to get manifests for BuildService: %w", err)
	}

	objects, err := parseManifests(buildServiceManifest)
	if err != nil {
		return fmt.Errorf("failed to parse manifests for BuildService: %w", err)
	}

	for _, obj := range objects {
		// Set ownership labels and owner reference
		if err := setOwnership(obj, owner, string(manifests.BuildService), r.Scheme); err != nil {
			return fmt.Errorf("failed to set ownership for %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), obj.GetKind(), manifests.BuildService, err)
		}

		if err := applyObject(ctx, r.Client, obj); err != nil {
			if obj.GroupVersionKind().Group == "cert-manager.io" || obj.GroupVersionKind().Group == "kyverno.io" {
				// TODO: Remove this once we decide how to install cert-manager crds in envtest
				// TODO: Remove this once we decide if we want to have a dependency on Kyverno
				log.Info("Skipping resource: CRD not installed",
					"kind", obj.GetKind(),
					"apiVersion", obj.GetAPIVersion(),
					"namespace", obj.GetNamespace(),
					"name", obj.GetName(),
				)
				continue
			}
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), obj.GetKind(), manifests.BuildService, err)
		}
	}
	return nil
}

// updateComponentStatuses checks the status of all owned Deployments and updates the KonfluxBuildService status.
func (r *KonfluxBuildServiceReconciler) updateComponentStatuses(ctx context.Context, buildService *konfluxv1alpha1.KonfluxBuildService) error {
	log := logf.FromContext(ctx)

	// List all deployments owned by this KonfluxBuildService instance
	deploymentList := &appsv1.DeploymentList{}
	if err := r.List(ctx, deploymentList, client.MatchingLabels{
		KonfluxOwnerLabel: buildService.Name,
	}); err != nil {
		return fmt.Errorf("failed to list owned deployments: %w", err)
	}

	// Set conditions for each deployment and get summary
	summary := SetDeploymentConditions(buildService, deploymentList.Items)

	// Remove conditions for deployments that no longer exist
	CleanupStaleConditions(buildService, func(cond metav1.Condition) bool {
		return cond.Type == BuildServiceConditionTypeReady || summary.SeenConditionTypes[cond.Type]
	})

	// Set the overall Ready condition
	SetOverallReadyCondition(buildService, BuildServiceConditionTypeReady, summary)

	// Update the status subresource
	if err := r.Status().Update(ctx, buildService); err != nil {
		log.Error(err, "Failed to update KonfluxBuildService status")
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxBuildServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxBuildService{}).
		Named("konfluxbuildservice").
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
