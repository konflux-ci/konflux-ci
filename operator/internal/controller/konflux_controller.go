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
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const (
	// KonfluxOwnerLabel is the label used to identify resources owned by the Konflux operator.
	KonfluxOwnerLabel = "konflux.konflux-ci.dev/owner"
	// KonfluxComponentLabel is the label used to identify which component a resource belongs to.
	KonfluxComponentLabel = "konflux.konflux-ci.dev/component"
	// KonfluxCRName is the singleton name for the Konflux CR.
	KonfluxCRName = "konflux"
	// ConditionTypeReady is the condition type for overall readiness
	ConditionTypeReady = "Ready"
)

// KonfluxReconciler reconciles a Konflux object
type KonfluxReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=*,resources=*,verbs=*

// TODO: Set proper RBAC rules for the controller

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KonfluxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Konflux instance
	konflux := &konfluxv1alpha1.Konflux{}
	if err := r.Get(ctx, req.NamespacedName, konflux); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling Konflux", "name", konflux.Name)

	// Apply all embedded manifests
	if err := r.applyAllManifests(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply manifests")
		// Update status to reflect the error
		r.setCondition(konflux, metav1.Condition{
			Type:    ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  "ApplyFailed",
			Message: err.Error(),
		})
		_ = r.Status().Update(ctx, konflux)
		return ctrl.Result{}, err
	}

	// Check the status of owned deployments and update Konflux status
	if err := r.updateComponentStatuses(ctx, konflux); err != nil {
		log.Error(err, "Failed to update component statuses")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled Konflux")
	return ctrl.Result{}, nil
}

// applyAllManifests loads and applies all embedded manifests to the cluster.
func (r *KonfluxReconciler) applyAllManifests(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	return manifests.WalkManifests(func(info manifests.ManifestInfo) error {

		objects, err := parseManifests(info.Content)
		if err != nil {
			return fmt.Errorf("failed to parse manifests for %s: %w", info.Component, err)
		}
		objects = transformObjectsForComponent(objects, info.Component, owner)
		for _, obj := range objects {
			// Set ownership labels and owner reference

			if err := r.setOwnership(obj, owner, string(info.Component)); err != nil {
				return fmt.Errorf("failed to set ownership for %s/%s (%s) from %s: %w",
					obj.GetNamespace(), obj.GetName(), obj.GetKind(), info.Component, err)
			}

			if err := r.applyObject(ctx, obj); err != nil {
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
					obj.GetNamespace(), obj.GetName(), obj.GetKind(), info.Component, err)
			}
		}

		return nil
	})
}

func transformObjectsForComponent(objects []*unstructured.Unstructured, component manifests.Component, konflux *konfluxv1alpha1.Konflux) []*unstructured.Unstructured {
	switch component {
	case manifests.ApplicationAPI:
		return objects
	case manifests.BuildService:
		return objects
	case manifests.EnterpriseContract:
		return objects
	case manifests.ImageController:
		return transformObjectsForImageController(objects, konflux)
	case manifests.Integration:
		return objects
	case manifests.NamespaceLister:
		return objects
	case manifests.RBAC:
		return objects
	case manifests.Release:
		return objects
	case manifests.UI:
		return objects
	default:
		return objects
	}
}

func transformObjectsForImageController(_ []*unstructured.Unstructured, _ *konfluxv1alpha1.Konflux) []*unstructured.Unstructured {
	return []*unstructured.Unstructured{}
}

// parseManifests parses YAML content into a slice of unstructured objects.
func parseManifests(content []byte) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured

	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(content), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode manifest: %w", err)
		}

		// Skip empty documents
		if len(obj.Object) == 0 {
			continue
		}

		objects = append(objects, obj)
	}

	return objects, nil
}

// setOwnership sets owner reference and labels on the object to establish ownership.
func (r *KonfluxReconciler) setOwnership(obj *unstructured.Unstructured, owner *konfluxv1alpha1.Konflux, component string) error {
	// Set ownership labels
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[KonfluxOwnerLabel] = owner.Name
	labels[KonfluxComponentLabel] = component
	obj.SetLabels(labels)

	// Set owner reference for garbage collection and watch triggers
	// Since Konflux CR is cluster-scoped, it can own both cluster-scoped and namespaced resources
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	return nil
}

// applyObject applies a single unstructured object to the cluster using server-side apply.
// Server-side apply is idempotent and only triggers updates when there are actual changes,
// preventing reconcile loops when watching owned resources.
func (r *KonfluxReconciler) applyObject(ctx context.Context, obj *unstructured.Unstructured) error {
	return r.Patch(ctx, obj, client.Apply, client.FieldOwner("konflux-operator"), client.ForceOwnership)
}

// updateComponentStatuses checks the status of all owned Deployments and updates the Konflux status.
func (r *KonfluxReconciler) updateComponentStatuses(ctx context.Context, konflux *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	// List all deployments owned by this Konflux instance
	deploymentList := &appsv1.DeploymentList{}
	if err := r.List(ctx, deploymentList, client.MatchingLabels{
		KonfluxOwnerLabel: konflux.Name,
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
			r.setCondition(konflux, metav1.Condition{
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

			r.setCondition(konflux, metav1.Condition{
				Type:    conditionType,
				Status:  metav1.ConditionFalse,
				Reason:  "DeploymentNotReady",
				Message: message,
			})
		}
	}

	// Remove conditions for deployments that no longer exist
	r.cleanupStaleConditions(konflux, seenConditionTypes)

	// Set the overall Ready condition
	if allReady {
		r.setCondition(konflux, metav1.Condition{
			Type:    ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  "AllComponentsReady",
			Message: fmt.Sprintf("All %d deployments are ready", len(deploymentList.Items)),
		})
	} else {
		r.setCondition(konflux, metav1.Condition{
			Type:    ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  "ComponentsNotReady",
			Message: fmt.Sprintf("Deployments not ready: %v", notReadyNames),
		})
	}

	// Update the status subresource
	if err := r.Status().Update(ctx, konflux); err != nil {
		log.Error(err, "Failed to update Konflux status")
		return err
	}

	return nil
}

// cleanupStaleConditions removes conditions for deployments that no longer exist.
func (r *KonfluxReconciler) cleanupStaleConditions(konflux *konfluxv1alpha1.Konflux, seenConditionTypes map[string]bool) {
	// Keep only the Ready condition and conditions for existing deployments
	var newConditions []metav1.Condition
	for _, cond := range konflux.Status.Conditions {
		if cond.Type == ConditionTypeReady || seenConditionTypes[cond.Type] {
			newConditions = append(newConditions, cond)
		}
	}
	konflux.Status.Conditions = newConditions
}

// setCondition updates or adds a condition to the Konflux status.
func (r *KonfluxReconciler) setCondition(konflux *konfluxv1alpha1.Konflux, condition metav1.Condition) {
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = konflux.Generation

	// Find and update existing condition or append new one
	found := false
	for i, existing := range konflux.Status.Conditions {
		if existing.Type == condition.Type {
			// Only update LastTransitionTime if status changed
			if existing.Status == condition.Status {
				condition.LastTransitionTime = existing.LastTransitionTime
			}
			konflux.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		konflux.Status.Conditions = append(konflux.Status.Conditions, condition)
	}
}

// generationChangedPredicate filters out events where the generation hasn't changed
// (i.e., status-only updates that shouldn't trigger reconciliation)
var generationChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return true
		}
		// Only reconcile if the generation changed (spec was modified)
		// This filters out status-only updates
		return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return true
	},
}

// deploymentReadinessPredicate triggers reconciliation when:
// - Spec changes (generation changed)
// - Readiness status changes (ReadyReplicas, AvailableReplicas, UnavailableReplicas)
// This allows us to react to deployment health changes without polling
var deploymentReadinessPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return true
		}
		// Always reconcile on spec changes
		if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
			return true
		}
		// Check for meaningful status changes
		oldDep, ok1 := e.ObjectOld.(*appsv1.Deployment)
		newDep, ok2 := e.ObjectNew.(*appsv1.Deployment)
		if !ok1 || !ok2 {
			return true
		}
		// Trigger on readiness changes
		return oldDep.Status.ReadyReplicas != newDep.Status.ReadyReplicas ||
			oldDep.Status.AvailableReplicas != newDep.Status.AvailableReplicas ||
			oldDep.Status.UnavailableReplicas != newDep.Status.UnavailableReplicas ||
			oldDep.Status.UpdatedReplicas != newDep.Status.UpdatedReplicas ||
			oldDep.Status.Replicas != newDep.Status.Replicas
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return true
	},
}

// labelsOrAnnotationsChangedPredicate triggers reconciliation when labels or annotations change
// Used for resources like ConfigMaps that don't have a generation field that changes on data updates
var labelsOrAnnotationsChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return true
		}
		// Check if generation changed
		if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
			return true
		}
		// Also check labels and annotations for resources without generation updates
		return !reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) ||
			!reflect.DeepEqual(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations())
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return true
	},
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.Konflux{}).
		Named("konflux").
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
