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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const (
	// ImageControllerConditionTypeReady is the condition type for overall readiness
	ImageControllerConditionTypeReady = "Ready"
)

// KonfluxImageControllerReconciler reconciles a KonfluxImageController object
type KonfluxImageControllerReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluximagecontrollers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluximagecontrollers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluximagecontrollers/finalizers,verbs=update

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

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, imageController); err != nil {
		log.Error(err, "Failed to apply manifests")
		SetFailedCondition(imageController, ImageControllerConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, imageController); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Check the status of owned deployments and update KonfluxImageController status
	if err := UpdateComponentStatuses(ctx, r.Client, imageController, ImageControllerConditionTypeReady); err != nil {
		log.Error(err, "Failed to update component statuses")
		SetFailedCondition(imageController, ImageControllerConditionTypeReady, "FailedToGetDeploymentStatus", err)
		if updateErr := r.Status().Update(ctx, imageController); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxImageController")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxImageControllerReconciler) applyManifests(ctx context.Context, owner *konfluxv1alpha1.KonfluxImageController) error {
	log := logf.FromContext(ctx)

	objects, err := r.ObjectStore.GetForComponent(manifests.ImageController)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for ImageController: %w", err)
	}

	for _, obj := range objects {
		// Set ownership labels and owner reference
		if err := setOwnership(obj, owner, string(manifests.ImageController), r.Scheme); err != nil {
			return fmt.Errorf("failed to set ownership for %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), getKind(obj), manifests.ImageController, err)
		}

		if err := applyObject(ctx, r.Client, obj); err != nil {
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
				obj.GetNamespace(), obj.GetName(), getKind(obj), manifests.ImageController, err)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxImageControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxImageController{}).
		Named("konfluximagecontroller").
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
