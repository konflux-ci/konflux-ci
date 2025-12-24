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
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"reflect"
	"strings"

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
	// KonfluxBuildServiceCRName is the name for the KonfluxBuildService CR.
	KonfluxBuildServiceCRName = "konflux-build-service"
	// KonfluxIntegrationServiceCRName is the name for the KonfluxIntegrationService CR.
	KonfluxIntegrationServiceCRName = "konflux-integration-service"
	// KonfluxReleaseServiceCRName is the name for the KonfluxReleaseService CR.
	KonfluxReleaseServiceCRName = "konflux-release-service"
	// uiNamespace is the namespace for UI resources
	uiNamespace = "konflux-ui"
	// certManagerGroup is the API group for cert-manager resources
	certManagerGroup = "cert-manager.io"
	// kyvernoGroup is the API group for Kyverno resources
	kyvernoGroup = "kyverno.io"
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

	// Ensure UI secrets are created
	if err := r.ensureUISecrets(ctx, konflux); err != nil {
		log.Error(err, "Failed to ensure UI secrets")
		return ctrl.Result{}, err
	}

	// Check the status of owned deployments (doesn't set overall Ready yet)
	deploymentSummary, err := r.updateComponentStatuses(ctx, konflux)
	if err != nil {
		log.Error(err, "Failed to update component statuses")
		SetFailedCondition(konflux, ConditionTypeReady, "FailedToGetDeploymentStatus", err)
		if updateErr := r.Status().Update(ctx, konflux); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Collect status from all sub-CRs
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

	// Set overall Ready condition considering deployments and all sub-CRs
	SetAggregatedReadyCondition(konflux, ConditionTypeReady, deploymentSummary, subCRStatuses)

	// Update the status subresource with all collected conditions
	if err := r.Status().Update(ctx, konflux); err != nil {
		log.Error(err, "Failed to update Konflux status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled Konflux")
	return ctrl.Result{}, nil
}

// applyAllManifests loads and applies all embedded manifests to the cluster.
func (r *KonfluxReconciler) applyAllManifests(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	return manifests.WalkManifests(func(info manifests.ManifestInfo) error {
		if info.Component == manifests.BuildService {
			log.Info("Skipping BuildService manifest, deferring to its own reconciler")
			return nil
		}
		if info.Component == manifests.Integration {
			log.Info("Skipping Integration manifest, deferring to its own reconciler")
			return nil
		}
		if info.Component == manifests.Release {
			log.Info("Skipping Release manifest, deferring to its own reconciler")
			return nil
		}

		objects, err := parseManifests(info.Content)
		if err != nil {
			return fmt.Errorf("failed to parse manifests for %s: %w", info.Component, err)
		}
		objects = transformObjectsForComponent(objects, info.Component, owner)
		for _, obj := range objects {
			// Set ownership labels and owner reference

			if err := setOwnership(obj, owner, string(info.Component), r.Scheme); err != nil {
				return fmt.Errorf("failed to set ownership for %s/%s (%s) from %s: %w",
					obj.GetNamespace(), obj.GetName(), obj.GetKind(), info.Component, err)
			}

			if err := applyObject(ctx, r.Client, obj); err != nil {
				if obj.GroupVersionKind().Group == certManagerGroup || obj.GroupVersionKind().Group == kyvernoGroup {
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

// applyKonfluxBuildService creates or updates the KonfluxBuildService CR.
func (r *KonfluxReconciler) applyKonfluxBuildService(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

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
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, buildService, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxBuildService: %w", err)
	}

	log.Info("Applying KonfluxBuildService CR", "name", buildService.Name)
	return r.Patch(ctx, buildService, client.Apply, client.FieldOwner("konflux-operator"), client.ForceOwnership)
}

// applyKonfluxIntegrationService creates or updates the KonfluxIntegrationService CR.
func (r *KonfluxReconciler) applyKonfluxIntegrationService(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

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
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, integrationService, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxIntegrationService: %w", err)
	}

	log.Info("Applying KonfluxIntegrationService CR", "name", integrationService.Name)
	return r.Patch(ctx, integrationService, client.Apply, client.FieldOwner("konflux-operator"), client.ForceOwnership)
}

// applyKonfluxReleaseService creates or updates the KonfluxReleaseService CR.
func (r *KonfluxReconciler) applyKonfluxReleaseService(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

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
	}

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(owner, releaseService, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference for KonfluxReleaseService: %w", err)
	}

	log.Info("Applying KonfluxReleaseService CR", "name", releaseService.Name)
	return r.Patch(ctx, releaseService, client.Apply, client.FieldOwner("konflux-operator"), client.ForceOwnership)
}

func transformObjectsForComponent(objects []*unstructured.Unstructured, component manifests.Component, konflux *konfluxv1alpha1.Konflux) []*unstructured.Unstructured {
	switch component {
	case manifests.ApplicationAPI:
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

// ensureUISecrets ensures that UI secrets exist and are properly configured.
// Only generates secret values if they don't already exist (preserves existing secrets).
func (r *KonfluxReconciler) ensureUISecrets(ctx context.Context, konflux *konfluxv1alpha1.Konflux) error {
	// Helper for the actual reconciliation logic
	ensureSecret := func(name, key string, length int, urlSafe bool) error {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: uiNamespace,
			},
		}

		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
			// 1. Ensure Ownership/Labels (Updates if missing)
			if err := setOwnership(secret, konflux, "ui", r.Scheme); err != nil {
				return err
			}

			// 2. Only generate data if it doesn't already exist
			if secret.Data == nil {
				secret.Data = make(map[string][]byte)
			}

			if len(secret.Data[key]) == 0 {
				val, err := r.generateRandomBytes(length, urlSafe)
				if err != nil {
					return err
				}
				secret.Data[key] = val
			}
			return nil
		})
		return err
	}

	// Execute for both secrets
	if err := ensureSecret("oauth2-proxy-client-secret", "client-secret", 20, true); err != nil {
		return fmt.Errorf("client-secret: %w", err)
	}
	return ensureSecret("oauth2-proxy-cookie-secret", "cookie-secret", 16, false)
}

// generateRandomBytes generates a random secret value with the specified encoding.
func (r *KonfluxReconciler) generateRandomBytes(length int, urlSafe bool) ([]byte, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if urlSafe {
		return []byte(base64.RawURLEncoding.EncodeToString(b)), nil
	}
	return []byte(base64.StdEncoding.EncodeToString(b)), nil
}

// applyObject applies a single unstructured object to the cluster using server-side apply.
// Server-side apply is idempotent and only triggers updates when there are actual changes,
// preventing reconcile loops when watching owned resources.
func applyObject(ctx context.Context, k8sClient client.Client, obj *unstructured.Unstructured) error {
	return k8sClient.Patch(ctx, obj, client.Apply, client.FieldOwner("konflux-operator"), client.ForceOwnership)
}

// updateComponentStatuses checks the status of all owned Deployments and updates conditions on Konflux.
// It returns the deployment status summary for use in computing the overall Ready condition.
func (r *KonfluxReconciler) updateComponentStatuses(ctx context.Context, konflux *konfluxv1alpha1.Konflux) (DeploymentStatusSummary, error) {
	// List all deployments owned by this Konflux instance
	deploymentList := &appsv1.DeploymentList{}
	if err := r.List(ctx, deploymentList, client.MatchingLabels{
		KonfluxOwnerLabel: konflux.Name,
	}); err != nil {
		return DeploymentStatusSummary{}, fmt.Errorf("failed to list owned deployments: %w", err)
	}

	// Set conditions for each deployment and get summary
	summary := SetDeploymentConditions(konflux, deploymentList.Items)

	// Remove conditions for deployments that no longer exist
	CleanupStaleConditions(konflux, func(cond metav1.Condition) bool {
		return cond.Type == ConditionTypeReady ||
			summary.SeenConditionTypes[cond.Type] ||
			strings.HasPrefix(cond.Type, "build-service.") ||
			strings.HasPrefix(cond.Type, "integration-service.") ||
			strings.HasPrefix(cond.Type, "release-service.")
	})

	return summary, nil
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
		// Watch KonfluxBuildService for any changes to copy conditions to Konflux CR
		// No predicate needed - the For() GenerationChangedPredicate prevents self-triggering loops
		Owns(&konfluxv1alpha1.KonfluxBuildService{}).
		// Watch KonfluxIntegrationService for any changes to copy conditions to Konflux CR
		Owns(&konfluxv1alpha1.KonfluxIntegrationService{}).
		// Watch KonfluxReleaseService for any changes to copy conditions to Konflux CR
		Owns(&konfluxv1alpha1.KonfluxReleaseService{}).
		Complete(r)
}
