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
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// InfoConditionTypeReady is the condition type for overall readiness
	InfoConditionTypeReady = "Ready"
	// infoNamespace is the namespace for info resources
	infoNamespace = "konflux-info"
	// infoConfigMapName is the name of the info.json ConfigMap
	infoConfigMapName = "konflux-public-info"
	// bannerConfigMapName is the name of the banner ConfigMap
	bannerConfigMapName = "konflux-banner-configmap"
)

// infoCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state for the Info component.
var infoCleanupGVKs = []schema.GroupVersionKind{
	{Group: "", Version: "v1", Kind: "ConfigMap"},
	{Group: "", Version: "v1", Kind: "Namespace"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
}

// KonfluxInfoReconciler reconciles a KonfluxInfo object
type KonfluxInfoReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxinfoes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxinfoes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxinfoes/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=list
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KonfluxInfoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxInfo instance
	konfluxInfo := &konfluxv1alpha1.KonfluxInfo{}
	if err := r.Get(ctx, req.NamespacedName, konfluxInfo); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxInfo", "name", konfluxInfo.Name)

	// Create a tracking client with ownership config for this reconcile.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             konfluxInfo,
		OwnerLabelKey:     KonfluxOwnerLabel,
		ComponentLabelKey: KonfluxComponentLabel,
		Component:         string(manifests.Info),
		FieldManager:      FieldManagerInfo,
	})

	// Ensure konflux-info namespace exists
	if err := r.ensureNamespaceExists(ctx, tc); err != nil {
		log.Error(err, "Failed to ensure namespace")
		SetFailedCondition(konfluxInfo, InfoConditionTypeReady, "NamespaceCreationFailed", err)
		if updateErr := r.Status().Update(ctx, konfluxInfo); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Reconcile ConfigMaps first (if configured)
	// This must happen before applyManifests to ensure ConfigMaps exist
	if err := r.reconcileInfoConfigMap(ctx, tc, konfluxInfo); err != nil {
		log.Error(err, "Failed to reconcile info ConfigMap")
		SetFailedCondition(konfluxInfo, InfoConditionTypeReady, "InfoConfigMapFailed", err)
		if updateErr := r.Status().Update(ctx, konfluxInfo); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	if err := r.reconcileBannerConfigMap(ctx, tc, konfluxInfo); err != nil {
		log.Error(err, "Failed to reconcile banner ConfigMap")
		SetFailedCondition(konfluxInfo, InfoConditionTypeReady, "BannerConfigMapFailed", err)
		if updateErr := r.Status().Update(ctx, konfluxInfo); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc); err != nil {
		log.Error(err, "Failed to apply manifests")
		SetFailedCondition(konfluxInfo, InfoConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, konfluxInfo); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Cleanup orphaned resources
	if err := tc.CleanupOrphans(ctx, KonfluxOwnerLabel, konfluxInfo.Name, infoCleanupGVKs); err != nil {
		log.Error(err, "Failed to cleanup orphaned resources")
		SetFailedCondition(konfluxInfo, InfoConditionTypeReady, "CleanupFailed", err)
		if updateErr := r.Status().Update(ctx, konfluxInfo); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Update component status (sets Ready condition based on owned resources)
	// Note: konflux-info has no deployments, so this will set Ready=true
	if err := UpdateComponentStatuses(ctx, r.Client, konfluxInfo, InfoConditionTypeReady); err != nil {
		log.Error(err, "Failed to update component statuses")
		SetFailedCondition(konfluxInfo, InfoConditionTypeReady, "FailedToGetDeploymentStatus", err)
		if updateErr := r.Status().Update(ctx, konfluxInfo); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Update status
	if err := r.Status().Update(ctx, konfluxInfo); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxInfo")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
func (r *KonfluxInfoReconciler) applyManifests(ctx context.Context, tc *tracking.Client) error {
	log := logf.FromContext(ctx)

	objects, err := r.ObjectStore.GetForComponent(manifests.Info)
	if err != nil {
		return fmt.Errorf("failed to get manifests for Info: %w", err)
	}

	for _, obj := range objects {
		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			gvk := obj.GetObjectKind().GroupVersionKind()
			// TODO: Remove this once we decide how to install cert-manager crds in envtest
			// TODO: Remove this once we decide if we want to have a dependency on Kyverno
			if gvk.Group == CertManagerGroup || gvk.Group == KyvernoGroup {
				log.Info("Skipping resource: CRD not installed",
					"kind", gvk.Kind,
					"apiVersion", gvk.GroupVersion().String(),
					"namespace", obj.GetNamespace(),
					"name", obj.GetName(),
				)
				continue
			}
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.Info, err)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxInfoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxInfo{}).
		Named("konfluxinfo").
		// Use predicates to filter out unnecessary updates and prevent reconcile loops
		Owns(&corev1.Namespace{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&rbacv1.Role{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&rbacv1.RoleBinding{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(labelsOrAnnotationsChangedPredicate)).
		Complete(r)
}

// ensureNamespaceExists ensures the konflux-info namespace exists before creating ConfigMaps.
func (r *KonfluxInfoReconciler) ensureNamespaceExists(ctx context.Context, tc *tracking.Client) error {
	objects, err := r.ObjectStore.GetForComponent(manifests.Info)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for Info: %w", err)
	}

	for _, obj := range objects {
		if namespace, ok := obj.(*corev1.Namespace); ok {
			// Validate that the namespace name matches the expected infoNamespace
			if namespace.Name != infoNamespace {
				return fmt.Errorf(
					"unexpected namespace name in manifest: expected %s, got %s", infoNamespace, namespace.Name)
			}
			// Apply with ownership using the tracking client
			if err := tc.ApplyOwned(ctx, namespace); err != nil {
				return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
					namespace.GetNamespace(), namespace.GetName(), tracking.GetKind(namespace), manifests.Info, err)
			}
		}
	}
	return nil
}

// generateInfoJSON generates info.json content from PublicInfo.
// Provides defaults if fields are missing.
func (r *KonfluxInfoReconciler) generateInfoJSON(config *konfluxv1alpha1.PublicInfo) ([]byte, error) {
	info := r.applyInfoDefaults(config)
	return json.MarshalIndent(info, "", "    ")
}

// applyInfoDefaults applies default values to PublicInfo if not specified.
func (r *KonfluxInfoReconciler) applyInfoDefaults(config *konfluxv1alpha1.PublicInfo) *infoJSON {
	info := &infoJSON{
		Environment: "development",
		Visibility:  "public",
		RBAC:        getDefaultRBACRoles(),
	}

	if config == nil {
		return info
	}

	if config.Environment != "" {
		info.Environment = config.Environment
	}
	if config.Visibility != "" {
		info.Visibility = config.Visibility
	}
	if config.StatusPageUrl != "" {
		info.StatusPageUrl = config.StatusPageUrl
	}
	if len(config.RBAC) > 0 {
		info.RBAC = convertRBACRoles(config.RBAC)
	} else {
		info.RBAC = getDefaultRBACRoles()
	}
	if config.Integrations != nil {
		info.Integrations = config.Integrations
	}

	return info
}

// generateBannerYAML generates banner-content.yaml from Banner.
// Returns an empty array if banner is nil, items is nil, or items is empty.
func (r *KonfluxInfoReconciler) generateBannerYAML(config *konfluxv1alpha1.Banner) ([]byte, error) {
	var banners []konfluxv1alpha1.BannerItem
	if config != nil && config.Items != nil && len(*config.Items) > 0 {
		banners = *config.Items
	}
	// If config is nil, Items is nil, or Items is empty, banners remains empty slice
	// This handles all cases: nil banner, banner with nil Items, and banner with empty Items array
	return yaml.Marshal(banners)
}

// reconcileInfoConfigMap creates or updates the info.json ConfigMap.
func (r *KonfluxInfoReconciler) reconcileInfoConfigMap(ctx context.Context, tc *tracking.Client, info *konfluxv1alpha1.KonfluxInfo) error {
	log := logf.FromContext(ctx)

	var infoJSON []byte
	var err error
	if info.Spec.PublicInfo != nil {
		infoJSON, err = r.generateInfoJSON(info.Spec.PublicInfo)
		if err != nil {
			return fmt.Errorf("failed to generate info.json: %w", err)
		}
	} else {
		// Use default development config
		infoJSON, err = r.generateInfoJSON(nil)
		if err != nil {
			return fmt.Errorf("failed to generate default info.json: %w", err)
		}
	}

	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      infoConfigMapName,
			Namespace: infoNamespace,
		},
		Data: map[string]string{
			"info.json": string(infoJSON),
		},
	}

	log.Info("Applying info ConfigMap", "name", configMap.Name, "namespace", configMap.Namespace)
	if err := tc.ApplyOwned(ctx, configMap); err != nil {
		return fmt.Errorf("failed to apply ConfigMap: %w", err)
	}

	return nil
}

// reconcileBannerConfigMap creates or updates the banner-content.yaml ConfigMap.
func (r *KonfluxInfoReconciler) reconcileBannerConfigMap(ctx context.Context, tc *tracking.Client, info *konfluxv1alpha1.KonfluxInfo) error {
	log := logf.FromContext(ctx)

	bannerYAML, err := r.generateBannerYAML(info.Spec.Banner)
	if err != nil {
		return fmt.Errorf("failed to generate banner-content.yaml: %w", err)
	}

	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      bannerConfigMapName,
			Namespace: infoNamespace,
		},
		Data: map[string]string{
			"banner-content.yaml": string(bannerYAML),
		},
	}

	log.Info("Applying banner ConfigMap", "name", configMap.Name, "namespace", configMap.Namespace)
	if err := tc.ApplyOwned(ctx, configMap); err != nil {
		return fmt.Errorf("failed to apply ConfigMap: %w", err)
	}

	return nil
}

// infoJSON is the internal representation of info.json for serialization
type infoJSON struct {
	Environment   string                              `json:"environment"`
	Visibility    string                              `json:"visibility"`
	Integrations  *konfluxv1alpha1.IntegrationsConfig `json:"integrations,omitempty"`
	StatusPageUrl string                              `json:"statusPageUrl,omitempty"`
	RBAC          []rbacRoleJSON                      `json:"rbac,omitempty"`
}

type rbacRoleJSON struct {
	DisplayName string      `json:"displayName"`
	Description string      `json:"description"`
	RoleRef     roleRefJSON `json:"roleRef"`
}

type roleRefJSON struct {
	APIGroup string `json:"apiGroup"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
}

// convertRBACRoles converts API types to JSON types
func convertRBACRoles(roles []konfluxv1alpha1.RBACRole) []rbacRoleJSON {
	result := make([]rbacRoleJSON, len(roles))
	for i, role := range roles {
		displayName := role.DisplayName
		if displayName == "" {
			displayName = role.Name
		}
		result[i] = rbacRoleJSON{
			DisplayName: displayName,
			Description: role.Description,
			RoleRef: roleRefJSON{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     role.Name,
			},
		}
	}
	return result
}

// getDefaultRBACRoles returns the default RBAC roles
func getDefaultRBACRoles() []rbacRoleJSON {
	return []rbacRoleJSON{
		{
			DisplayName: "admin",
			Description: "Full access to Konflux resources including secrets",
			RoleRef: roleRefJSON{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "konflux-admin-user-actions",
			},
		},
		{
			DisplayName: "maintainer",
			Description: "Partial access to Konflux resources without access to secrets",
			RoleRef: roleRefJSON{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "konflux-maintainer-user-actions",
			},
		},
		{
			DisplayName: "contributor",
			Description: "View access to Konflux resources without access to secrets",
			RoleRef: roleRefJSON{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "konflux-contributor-user-actions",
			},
		},
	}
}
