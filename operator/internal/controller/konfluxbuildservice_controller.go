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

	"github.com/go-logr/logr"
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
		r.setFailedToApplyCondition(ctx, buildService, err, log)
		return ctrl.Result{}, err
	}

	// Check the status of owned deployments and update KonfluxBuildService status
	if err := r.updateComponentStatuses(ctx, buildService); err != nil {
		log.Error(err, "Failed to update component statuses")
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

	// Track which deployment conditions we've seen (for cleanup)
	seenConditionTypes := make(map[string]bool)
	allReady := true
	var notReadyNames []string

	for _, deployment := range deploymentList.Items {
		// Create a condition type for this deployment
		conditionType := fmt.Sprintf("%s/%s", deployment.Namespace, deployment.Name)
		seenConditionTypes[conditionType] = true

		ready := deployment.Status.ReadyReplicas == deployment.Status.Replicas &&
			deployment.Status.Replicas > 0 &&
			deployment.Status.UpdatedReplicas == deployment.Status.Replicas

		if ready {
			r.setCondition(buildService, metav1.Condition{
				Type:    conditionType,
				Status:  metav1.ConditionTrue,
				Reason:  "DeploymentReady",
				Message: fmt.Sprintf("Deployment has %d/%d replicas ready", deployment.Status.ReadyReplicas, deployment.Status.Replicas),
			})
		} else {
			allReady = false
			notReadyNames = append(notReadyNames, deployment.Name)

			message := fmt.Sprintf("Ready: %d/%d, Updated: %d/%d",
				deployment.Status.ReadyReplicas, deployment.Status.Replicas,
				deployment.Status.UpdatedReplicas, deployment.Status.Replicas)

			// Check for specific conditions that indicate problems
			for _, cond := range deployment.Status.Conditions {
				if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
					message = fmt.Sprintf("%s - %s: %s", message, cond.Reason, cond.Message)
				}
				if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
					message = fmt.Sprintf("%s - ReplicaFailure: %s", message, cond.Message)
				}
			}

			r.setCondition(buildService, metav1.Condition{
				Type:    conditionType,
				Status:  metav1.ConditionFalse,
				Reason:  "DeploymentNotReady",
				Message: message,
			})
		}
	}

	// Remove conditions for deployments that no longer exist
	r.cleanupStaleConditions(buildService, seenConditionTypes)

	// Set the overall Ready condition
	if allReady {
		r.setCondition(buildService, metav1.Condition{
			Type:    BuildServiceConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  "AllComponentsReady",
			Message: fmt.Sprintf("All %d deployments are ready", len(deploymentList.Items)),
		})
	} else {
		r.setCondition(buildService, metav1.Condition{
			Type:    BuildServiceConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  "ComponentsNotReady",
			Message: fmt.Sprintf("Deployments not ready: %v", notReadyNames),
		})
	}

	// Update the status subresource
	if err := r.Status().Update(ctx, buildService); err != nil {
		log.Error(err, "Failed to update KonfluxBuildService status")
		return err
	}

	return nil
}

// cleanupStaleConditions removes conditions for deployments that no longer exist.
func (r *KonfluxBuildServiceReconciler) cleanupStaleConditions(buildService *konfluxv1alpha1.KonfluxBuildService, seenConditionTypes map[string]bool) {
	// Keep only the Ready condition and conditions for existing deployments
	var newConditions []metav1.Condition
	for _, cond := range buildService.Status.Conditions {
		if cond.Type == BuildServiceConditionTypeReady || seenConditionTypes[cond.Type] {
			newConditions = append(newConditions, cond)
		}
	}
	buildService.Status.Conditions = newConditions
}

// setFailedToApplyCondition sets the FailedToApply condition and updates the KonfluxBuildService status.
func (r *KonfluxBuildServiceReconciler) setFailedToApplyCondition(ctx context.Context, buildService *konfluxv1alpha1.KonfluxBuildService, err error, log logr.Logger) {
	log.Error(err, "Failed to apply manifests")
	// Update status to reflect the error
	r.setCondition(buildService, metav1.Condition{
		Type:    BuildServiceConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  "ApplyFailed",
		Message: err.Error(),
	})
	if updateErr := r.Status().Update(ctx, buildService); updateErr != nil {
		log.Error(updateErr, "Failed to update KonfluxBuildService status")
	}
}

// setCondition updates or adds a condition to the KonfluxBuildService status.
func (r *KonfluxBuildServiceReconciler) setCondition(buildService *konfluxv1alpha1.KonfluxBuildService, condition metav1.Condition) {
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = buildService.Generation

	// Find and update existing condition or append new one
	found := false
	for i, existing := range buildService.Status.Conditions {
		if existing.Type == condition.Type {
			// Only update LastTransitionTime if status changed
			if existing.Status == condition.Status {
				condition.LastTransitionTime = existing.LastTransitionTime
			}
			buildService.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		buildService.Status.Conditions = append(buildService.Status.Conditions, condition)
	}
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
