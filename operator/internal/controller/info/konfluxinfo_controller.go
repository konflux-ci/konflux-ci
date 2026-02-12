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

package info

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	ctrlpredicate "sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/yaml"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/predicate"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
	"github.com/konflux-ci/konflux-ci/operator/pkg/version"
)

const (
	// CRName is the singleton name for the KonfluxInfo CR.
	CRName = "konflux-info"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-info-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxInfo"
	// infoNamespace is the namespace for info resources
	infoNamespace = "konflux-info"
	// infoConfigMapName is the name of the info.json ConfigMap
	infoConfigMapName = "konflux-public-info"
	// bannerConfigMapName is the name of the banner ConfigMap
	bannerConfigMapName = "konflux-banner-configmap"
	// clusterConfigMapName is the name of the cluster-config ConfigMap
	clusterConfigMapName = "cluster-config"
)

// ConfigMap key constants for cluster-config.
// WARNING: Changing these key names is a BREAKING CHANGE.
// These keys are read by PipelineRuns via kubectl, so any changes will break
// existing pipelines that depend on these keys. If you need to change a key name,
// you must:
// 1. Add the new key alongside the old one
// 2. Deprecate the old key in documentation
// 3. Remove the old key only in a major version release
const (
	// ClusterConfigKeyDefaultOIDCIssuer is the ConfigMap key for default OIDC issuer URL.
	ClusterConfigKeyDefaultOIDCIssuer = "defaultOIDCIssuer"
	// ClusterConfigKeyEnableKeylessSigning is the ConfigMap key for enabling keyless signing.
	ClusterConfigKeyEnableKeylessSigning = "enableKeylessSigning"
	// ClusterConfigKeyFulcioInternalUrl is the ConfigMap key for internal Fulcio URL.
	ClusterConfigKeyFulcioInternalUrl = "fulcioInternalUrl"
	// ClusterConfigKeyFulcioExternalUrl is the ConfigMap key for external Fulcio URL.
	ClusterConfigKeyFulcioExternalUrl = "fulcioExternalUrl"
	// ClusterConfigKeyRekorInternalUrl is the ConfigMap key for internal Rekor URL.
	ClusterConfigKeyRekorInternalUrl = "rekorInternalUrl"
	// ClusterConfigKeyRekorExternalUrl is the ConfigMap key for external Rekor URL.
	ClusterConfigKeyRekorExternalUrl = "rekorExternalUrl"
	// ClusterConfigKeyTufInternalUrl is the ConfigMap key for internal TUF URL.
	ClusterConfigKeyTufInternalUrl = "tufInternalUrl"
	// ClusterConfigKeyTufExternalUrl is the ConfigMap key for external TUF URL.
	ClusterConfigKeyTufExternalUrl = "tufExternalUrl"
	// ClusterConfigKeyTrustifyServerInternalUrl is the ConfigMap key for internal Trustify server URL.
	ClusterConfigKeyTrustifyServerInternalUrl = "trustifyServerInternalUrl"
	// ClusterConfigKeyTrustifyServerExternalUrl is the ConfigMap key for external Trustify server URL.
	ClusterConfigKeyTrustifyServerExternalUrl = "trustifyServerExternalUrl"
)

// ClusterConfigDiscoverer is an interface for discovering cluster configuration values.
// Implementations can detect values from the cluster environment, service discovery,
// or other sources. Used for dependency injection in tests and production.
type ClusterConfigDiscoverer interface {
	Discover(ctx context.Context) konfluxv1alpha1.ClusterConfigData
}

// InfoCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. All resources managed by this controller are always
// applied, so no cleanup GVKs are needed (they're always tracked and never become orphans).
var InfoCleanupGVKs = []schema.GroupVersionKind{}

// InfoClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. This is a security measure to prevent attackers from
// triggering deletion of arbitrary cluster resources by adding the owner label.
// InfoClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. All cluster-scoped resources managed by this controller are always
// applied, so no allow list is needed (they're always tracked and never become orphans).
var InfoClusterScopedAllowList tracking.ClusterScopedAllowList = nil

// KonfluxInfoReconciler reconciles a KonfluxInfo object
type KonfluxInfoReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
	// DiscoverClusterConfig is an optional discoverer for cluster configuration values.
	// If nil, a defaultClusterConfigDiscoverer will be used (returns empty values).
	// This field allows injecting a custom discovery implementation for testing.
	DiscoverClusterConfig ClusterConfigDiscoverer
	ClusterInfo           *clusterinfo.Info
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxinfoes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxinfoes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxinfoes/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=list
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=get;list;watch

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

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), konfluxInfo, crKind)

	// Create a tracking client with ownership config for this reconcile.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             konfluxInfo,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.Info),
		FieldManager:      FieldManager,
	})

	// Ensure konflux-info namespace exists
	if err := r.ensureNamespaceExists(ctx, tc); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonNamespaceCreationFailed, "ensure namespace exists")
	}

	// Reconcile ConfigMaps first (if configured)
	// This must happen before applyManifests to ensure ConfigMaps exist
	if err := r.reconcileInfoConfigMap(ctx, tc, konfluxInfo); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonConfigMapFailed, "reconcile info ConfigMap")
	}

	if err := r.reconcileBannerConfigMap(ctx, tc, konfluxInfo); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonConfigMapFailed, "reconcile banner ConfigMap")
	}

	if err := r.reconcileClusterConfigConfigMap(ctx, tc, konfluxInfo); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonConfigMapFailed, "reconcile cluster-config ConfigMap")
	}

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Cleanup orphaned resources
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, konfluxInfo.Name, InfoCleanupGVKs,
		tracking.WithClusterScopedAllowList(InfoClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Update component status (sets Ready condition based on owned resources)
	// Note: konflux-info has no deployments, so this will set Ready=true
	if err := condition.UpdateComponentStatuses(ctx, r.Client, konfluxInfo); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
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
	objects, err := r.ObjectStore.GetForComponent(manifests.Info)
	if err != nil {
		return fmt.Errorf("failed to get manifests for Info: %w", err)
	}

	for _, obj := range objects {
		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.Info, err)
		}
	}
	return nil
}

// versionPollerInterval is how often the VersionPoller checks for cluster version changes.
const versionPollerInterval = 10 * time.Minute

// enqueueKonfluxInfoForVersionChange returns reconcile requests for all KonfluxInfo instances.
// Used when the version poller detects a cluster version change so the info ConfigMap is refreshed.
func (r *KonfluxInfoReconciler) enqueueKonfluxInfoForVersionChange(ctx context.Context, _ client.Object) []reconcile.Request {
	list := &konfluxv1alpha1.KonfluxInfoList{}
	if err := r.List(ctx, list); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, len(list.Items))
	for i := range list.Items {
		reqs[i] = reconcile.Request{NamespacedName: types.NamespacedName{Name: list.Items[i].Name}}
	}
	return reqs
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxInfoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Channel for version poller to trigger reconciles when cluster version changes (buffered so poller never blocks).
	upgradeEvents := make(chan event.TypedGenericEvent[client.Object], 1)

	if r.ClusterInfo != nil {
		if err := mgr.Add(&VersionPoller{
			ClusterInfo:  r.ClusterInfo,
			Interval:     versionPollerInterval,
			EventChannel: upgradeEvents,
		}); err != nil {
			return err
		}
	}

	channelSource := source.Channel(
		upgradeEvents,
		handler.EnqueueRequestsFromMapFunc(r.enqueueKonfluxInfoForVersionChange),
	)

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxInfo{}).
		Named("konfluxinfo").
		// Use predicates to filter out unnecessary updates and prevent reconcile loops
		Owns(&corev1.Namespace{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.Role{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.RoleBinding{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(predicate.LabelsOrAnnotationsChangedPredicate)).
		WatchesRawSource(channelSource)

	// Conditionally watch ClusterVersion only on OpenShift
	if r.ClusterInfo != nil && r.ClusterInfo.IsOpenShift() {
		controllerBuilder = controllerBuilder.Watches(
			&configv1.ClusterVersion{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueKonfluxInfoForVersionChange),
			builder.WithPredicates(ctrlpredicate.ResourceVersionChangedPredicate{}),
		)
	}

	return controllerBuilder.Complete(r)
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
// Provides defaults if fields are missing. k8sVersion is the current cluster Kubernetes version (non-cached).
func (r *KonfluxInfoReconciler) generateInfoJSON(config *konfluxv1alpha1.PublicInfo, k8sVersion, openShiftVersion string) ([]byte, error) {
	info := r.applyInfoDefaults(config, k8sVersion, openShiftVersion)
	return json.MarshalIndent(info, "", "    ")
}

// applyInfoDefaults applies default values to PublicInfo if not specified.
func (r *KonfluxInfoReconciler) applyInfoDefaults(config *konfluxv1alpha1.PublicInfo, k8sVersion, openShiftVersion string) *infoJSON {
	info := &infoJSON{
		Environment:       "development",
		Visibility:        "public",
		KonfluxVersion:    version.Version,
		KubernetesVersion: k8sVersion,
		OpenShiftVersion:  openShiftVersion,
		RBAC:              getDefaultRBACRoles(),
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

	k8sVersion := ""
	openShiftVersion := ""
	if r.ClusterInfo != nil {
		if v, err := r.ClusterInfo.K8sVersion(); err == nil && v != nil {
			k8sVersion = v.GitVersion
		}
		if r.ClusterInfo.IsOpenShift() {
			var err error
			openShiftVersion, err = clusterinfo.GetOpenShiftVersion(ctx, r.Client)
			if err != nil {
				return fmt.Errorf("failed to get OpenShift version: %w", err)
			}
		}
	}

	var infoJSON []byte
	var err error
	if info.Spec.PublicInfo != nil {
		infoJSON, err = r.generateInfoJSON(info.Spec.PublicInfo, k8sVersion, openShiftVersion)
		if err != nil {
			return fmt.Errorf("failed to generate info.json: %w", err)
		}
	} else {
		// Use default development config
		infoJSON, err = r.generateInfoJSON(nil, k8sVersion, openShiftVersion)
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

// reconcileClusterConfigConfigMap creates or updates the cluster-config ConfigMap.
// User-provided values from the CR take precedence over any auto-detected values.
func (r *KonfluxInfoReconciler) reconcileClusterConfigConfigMap(ctx context.Context, tc *tracking.Client, info *konfluxv1alpha1.KonfluxInfo) error {
	log := logf.FromContext(ctx)

	// Merge discovered and user-provided values (user-provided takes precedence)
	var discovered konfluxv1alpha1.ClusterConfigData
	if r.DiscoverClusterConfig != nil {
		discovered = r.DiscoverClusterConfig.Discover(ctx)
	}
	userProvided := info.Spec.GetClusterConfigData()
	configData := userProvided.MergeOver(discovered)

	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterConfigMapName,
			Namespace: infoNamespace,
		},
		Data: configData,
	}

	log.Info("Applying cluster-config ConfigMap", "name", configMap.Name, "namespace", configMap.Namespace)
	if err := tc.ApplyOwned(ctx, configMap); err != nil {
		return fmt.Errorf("failed to apply ConfigMap: %w", err)
	}

	return nil
}

// infoJSON is the internal representation of info.json for serialization
type infoJSON struct {
	Environment       string                              `json:"environment"`
	Visibility        string                              `json:"visibility"`
	KonfluxVersion    string                              `json:"konfluxVersion,omitempty"`
	KubernetesVersion string                              `json:"kubernetesVersion,omitempty"`
	OpenShiftVersion  string                              `json:"openshiftVersion,omitempty"`
	Integrations      *konfluxv1alpha1.IntegrationsConfig `json:"integrations,omitempty"`
	StatusPageUrl     string                              `json:"statusPageUrl,omitempty"`
	RBAC              []rbacRoleJSON                      `json:"rbac,omitempty"`
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
