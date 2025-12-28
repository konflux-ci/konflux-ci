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
	"crypto/rand"
	"encoding/base64"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const (
	// UIConditionTypeReady is the condition type for overall readiness
	UIConditionTypeReady = "Ready"
	// uiNamespace is the namespace for UI resources
	uiNamespace = "konflux-ui"

	// Deployment names
	proxyDeploymentName = "proxy"
	dexDeploymentName   = "dex"

	// Container names
	nginxContainerName       = "nginx"
	oauth2ProxyContainerName = "oauth2-proxy"
	dexContainerName         = "dex"
)

// KonfluxUIReconciler reconciles a KonfluxUI object
type KonfluxUIReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxuis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxuis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxuis/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *KonfluxUIReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxUI instance
	ui := &konfluxv1alpha1.KonfluxUI{}
	if err := r.Get(ctx, req.NamespacedName, ui); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxUI", "name", ui.Name)

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, ui); err != nil {
		log.Error(err, "Failed to apply manifests")
		SetFailedCondition(ui, UIConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, ui); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Ensure UI secrets are created
	if err := r.ensureUISecrets(ctx, ui); err != nil {
		log.Error(err, "Failed to ensure UI secrets")
		SetFailedCondition(ui, UIConditionTypeReady, "SecretCreationFailed", err)
		if updateErr := r.Status().Update(ctx, ui); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Check the status of owned deployments and update KonfluxUI status
	if err := UpdateComponentStatuses(ctx, r.Client, ui, UIConditionTypeReady); err != nil {
		log.Error(err, "Failed to update component statuses")
		SetFailedCondition(ui, UIConditionTypeReady, "FailedToGetDeploymentStatus", err)
		if updateErr := r.Status().Update(ctx, ui); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxUI")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxUIReconciler) applyManifests(ctx context.Context, owner *konfluxv1alpha1.KonfluxUI) error {
	log := logf.FromContext(ctx)

	objects, err := r.ObjectStore.GetForComponent(manifests.UI)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for UI: %w", err)
	}

	for _, obj := range objects {
		// Apply customizations for deployments
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if err := applyUIDeploymentCustomizations(deployment, owner.Spec); err != nil {
				return fmt.Errorf("failed to apply customizations to deployment %s: %w", deployment.Name, err)
			}
		}

		// Set ownership labels and owner reference
		if err := setOwnership(obj, owner, string(manifests.UI), r.Scheme); err != nil {
			return fmt.Errorf("failed to set ownership for %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), getKind(obj), manifests.UI, err)
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
				obj.GetNamespace(), obj.GetName(), getKind(obj), manifests.UI, err)
		}
	}
	return nil
}

// applyUIDeploymentCustomizations applies user-defined customizations to UI deployments.
func applyUIDeploymentCustomizations(deployment *appsv1.Deployment, spec konfluxv1alpha1.KonfluxUISpec) error {
	switch deployment.Name {
	case proxyDeploymentName:
		if spec.Proxy != nil {
			deployment.Spec.Replicas = &spec.Proxy.Replicas
		}
		if err := buildProxyOverlay(spec.Proxy).ApplyToDeployment(deployment); err != nil {
			return err
		}
	case dexDeploymentName:
		if spec.Dex != nil {
			deployment.Spec.Replicas = &spec.Dex.Replicas
		}
		if err := buildDexOverlay(spec.Dex).ApplyToDeployment(deployment); err != nil {
			return err
		}
	}
	return nil
}

// buildProxyOverlay builds the pod overlay for the proxy deployment.
func buildProxyOverlay(spec *konfluxv1alpha1.ProxyDeploymentSpec) *customization.PodOverlay {
	if spec == nil {
		return customization.NewPodOverlay()
	}

	return customization.BuildPodOverlay(
		customization.DeploymentContext{Replicas: spec.Replicas},
		customization.WithContainerBuilder(
			nginxContainerName,
			customization.FromContainerSpec(spec.Nginx),
		),
		customization.WithContainerBuilder(
			oauth2ProxyContainerName,
			customization.FromContainerSpec(spec.OAuth2Proxy),
		),
	)
}

// buildDexOverlay builds the pod overlay for the dex deployment.
func buildDexOverlay(spec *konfluxv1alpha1.DexDeploymentSpec) *customization.PodOverlay {
	if spec == nil {
		return customization.NewPodOverlay()
	}

	return customization.BuildPodOverlay(
		customization.DeploymentContext{Replicas: spec.Replicas},
		customization.WithContainerBuilder(
			dexContainerName,
			customization.FromContainerSpec(spec.Dex),
		),
	)
}

// ensureUISecrets ensures that UI secrets exist and are properly configured.
// Only generates secret values if they don't already exist (preserves existing secrets).
func (r *KonfluxUIReconciler) ensureUISecrets(ctx context.Context, ui *konfluxv1alpha1.KonfluxUI) error {
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
			if err := setOwnership(secret, ui, string(manifests.UI), r.Scheme); err != nil {
				return err
			}

			// 2. Only generate data if it doesn't already exist
			if secret.Data == nil {
				secret.Data = make(map[string][]byte)
			}

			if len(secret.Data[key]) == 0 {
				val, err := generateRandomBytes(length, urlSafe)
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
func generateRandomBytes(length int, urlSafe bool) ([]byte, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if urlSafe {
		return []byte(base64.RawURLEncoding.EncodeToString(b)), nil
	}
	return []byte(base64.StdEncoding.EncodeToString(b)), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxUIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxUI{}).
		Named("konfluxui").
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
