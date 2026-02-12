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

package ui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/url"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/predicate"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/consolelink"
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
	"github.com/konflux-ci/konflux-ci/operator/pkg/dex"
	"github.com/konflux-ci/konflux-ci/operator/pkg/hashedconfigmap"
	"github.com/konflux-ci/konflux-ci/operator/pkg/ingress"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/oauth2proxy"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxUI CR.
	CRName = "konflux-ui"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-ui-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxUI"
	// uiNamespace is the namespace for UI resources
	uiNamespace = "konflux-ui"

	// Deployment names
	proxyDeploymentName = "proxy"
	dexDeploymentName   = "dex"

	// Service names
	proxyServiceName = "proxy"

	// Container names
	nginxContainerName       = "nginx"
	oauth2ProxyContainerName = "oauth2-proxy"
	dexContainerName         = "dex"

	// Dex ConfigMap constants
	dexConfigMapBaseName   = "dex"
	dexConfigKey           = "config.yaml"
	dexConfigMapLabel      = "app.kubernetes.io/managed-by-konflux-ui-reconciler"
	dexConfigMapVolumeName = "dex"
)

// UICleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. Only optional/conditional resources are listed here.
// Always-applied resources don't need cleanup (they're always tracked and never become orphans).
var UICleanupGVKs = []schema.GroupVersionKind{
	// Ingress is optional - only created when spec.ingress.enabled is true
	{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"},
	// ConsoleLink is optional - only created on OpenShift when ingress is enabled
	{Group: "console.openshift.io", Version: "v1", Kind: "ConsoleLink"},
	// ServiceAccount is optional - only created for OpenShift OAuth when configureLoginWithOpenShift is true
	{Group: "", Version: "v1", Kind: "ServiceAccount"},
	// Secret is optional - only created for OpenShift OAuth when configureLoginWithOpenShift is true
	{Group: "", Version: "v1", Kind: "Secret"},
}

// UIClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. This is a security measure to prevent attackers from
// triggering deletion of arbitrary cluster resources by adding the owner label.
// Only conditionally-created resources need to be listed here.
// Resources that are always applied don't need protection (they're always tracked).
var UIClusterScopedAllowList = tracking.ClusterScopedAllowList{
	// ConsoleLink is only created on OpenShift when ingress is enabled
	{Group: "console.openshift.io", Version: "v1", Kind: "ConsoleLink"}: sets.New(
		"konflux",
	),
}

// KonfluxUIReconciler reconciles a KonfluxUI object
type KonfluxUIReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
	ClusterInfo *clusterinfo.Info
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxuis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxuis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxuis/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;create;update;list;watch;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=dex;konflux-proxy;konflux-proxy-namespace-lister,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=dex;konflux-proxy;konflux-proxy-namespace-lister,verbs=bind
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=console.openshift.io,resources=consolelinks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=config.openshift.io,resources=ingresses,verbs=get
// +kubebuilder:rbac:groups=dex.coreos.com,resources=*,verbs=*
// +kubebuilder:rbac:groups=core,resources=users;groups,verbs=impersonate
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=localsubjectaccessreviews,verbs=create

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

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), ui, crKind)

	// Create a tracking client for this reconcile.
	// Resources applied through this client are automatically tracked.
	// At the end of a successful reconcile, orphaned resources are cleaned up.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             ui,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.UI),
		FieldManager:      FieldManager,
	})

	// Ensure konflux-ui namespace exists
	if err := r.ensureNamespaceExists(ctx, tc); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonNamespaceCreationFailed, "ensure namespace exists")
	}

	// Determine the endpoint URL for ingress, dex, and oauth2-proxy configuration
	endpoint, err := ingress.DetermineEndpointURL(ctx, r.Client, ui, uiNamespace, r.ClusterInfo)
	if err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonEndpointDeterminationFailed, "determine endpoint URL")
	}
	log.Info("Determined endpoint for KonfluxUI", "url", endpoint.String())

	// Reconcile Dex ConfigMap first (if configured) to get the ConfigMap name
	// This must happen before applyManifests so we can set the correct ConfigMap reference
	dexConfigMapName, err := r.reconcileDexConfigMap(ctx, ui, endpoint)
	if err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonConfigMapFailed, "reconcile Dex ConfigMap")
	}

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc, ui, dexConfigMapName, endpoint); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Reconcile Ingress if enabled (tracked automatically, deleted if not applied)
	// On OpenShift, also creates a ConsoleLink for the application menu
	if err := r.reconcileIngress(ctx, tc, ui, endpoint); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonIngressReconcileFailed, "reconcile Ingress")
	}

	// Reconcile OpenShift OAuth resources if enabled
	if err := r.reconcileOpenShiftOAuth(ctx, tc, ui, endpoint); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonOAuthFailed, "reconcile OpenShift OAuth resources")
	}

	// Ensure UI secrets are created
	if err := r.ensureUISecrets(ctx, tc); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSecretCreationFailed, "ensure UI secrets")
	}

	// Cleanup orphaned resources - delete any resources with our owner label
	// that weren't applied during this reconcile. This handles cases like
	// disabling Ingress (the Ingress resource is automatically deleted).
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, ui.Name, UICleanupGVKs,
		tracking.WithClusterScopedAllowList(UIClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Check the status of owned deployments and update KonfluxUI status
	if err := condition.UpdateComponentStatuses(ctx, r.Client, ui); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update ingress status
	isOnOpenShift := r.ClusterInfo != nil && r.ClusterInfo.IsOpenShift()
	updateIngressStatus(ui, isOnOpenShift, endpoint)

	// Final status update
	if err := r.Status().Update(ctx, ui); err != nil {
		log.Error(err, "Failed to update final status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxUI")
	return ctrl.Result{}, nil
}

// updateIngressStatus updates the ingress status fields on the KonfluxUI CR.
func updateIngressStatus(ui *konfluxv1alpha1.KonfluxUI, isOnOpenShift bool, endpoint *url.URL) {
	ui.Status.Ingress = &konfluxv1alpha1.IngressStatus{
		Enabled:  ptr.Deref(ui.GetIngressEnabledPreference(), isOnOpenShift),
		Hostname: endpoint.Hostname(),
		URL:      endpoint.String(),
	}
}

func (r *KonfluxUIReconciler) ensureNamespaceExists(ctx context.Context, tc *tracking.Client) error {
	objects, err := r.ObjectStore.GetForComponent(manifests.UI)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for UI: %w", err)
	}

	for _, obj := range objects {
		if namespace, ok := obj.(*corev1.Namespace); ok {
			// Validate that the namespace name matches the expected uiNamespace
			if namespace.Name != uiNamespace {
				return fmt.Errorf(
					"unexpected namespace name in manifest: expected %s, got %s", uiNamespace, namespace.Name)
			}
			if err := tc.ApplyOwned(ctx, namespace); err != nil {
				return fmt.Errorf("failed to apply namespace %s: %w", namespace.Name, err)
			}
		}
	}
	return nil
}

// applyManifests loads and applies all embedded manifests to the cluster.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
// dexConfigMapName is the name of the Dex ConfigMap to use (empty if not configured).
// endpoint is the base URL used to configure oauth2-proxy.
func (r *KonfluxUIReconciler) applyManifests(ctx context.Context, tc *tracking.Client, ui *konfluxv1alpha1.KonfluxUI, dexConfigMapName string, endpoint *url.URL) error {
	objects, err := r.ObjectStore.GetForComponent(manifests.UI)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for UI: %w", err)
	}

	for _, obj := range objects {
		// Apply customizations for deployments
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if err := applyUIDeploymentCustomizations(deployment, ui, r.ClusterInfo, dexConfigMapName, endpoint); err != nil {
				return fmt.Errorf("failed to apply customizations to deployment %s: %w", deployment.Name, err)
			}
		}

		// Apply customizations for services
		if service, ok := obj.(*corev1.Service); ok {
			applyUIServiceCustomizations(service, ui)
		}

		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s): %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), err)
		}
	}
	return nil
}

// applyUIDeploymentCustomizations applies user-defined customizations to UI deployments.
func applyUIDeploymentCustomizations(deployment *appsv1.Deployment, ui *konfluxv1alpha1.KonfluxUI, clusterInfo *clusterinfo.Info, dexConfigMapName string, endpoint *url.URL) error {
	openShiftLoginEnabled := isOpenShiftLoginEnabled(ui, clusterInfo)

	switch deployment.Name {
	case proxyDeploymentName:
		proxySpec := ui.Spec.GetProxy()
		deployment.Spec.Replicas = &proxySpec.Replicas
		// Build oauth2-proxy options based on endpoint URL and OpenShift login state
		oauth2ProxyOpts := buildOAuth2ProxyOptions(endpoint, openShiftLoginEnabled)
		if err := buildProxyOverlay(ui.Spec.Proxy, oauth2ProxyOpts...).ApplyToDeployment(deployment); err != nil {
			return err
		}
	case dexDeploymentName:
		dexSpec := ui.Spec.GetDex()
		deployment.Spec.Replicas = &dexSpec.Replicas
		if err := buildDexOverlay(ui.Spec.Dex, dexConfigMapName, openShiftLoginEnabled).ApplyToDeployment(deployment); err != nil {
			return err
		}
	}
	return nil
}

// applyUIServiceCustomizations applies user-defined customizations to UI services.
func applyUIServiceCustomizations(service *corev1.Service, ui *konfluxv1alpha1.KonfluxUI) {
	if service.Name != proxyServiceName {
		return
	}

	nodePortSpec := ui.Spec.GetNodePortService()
	if nodePortSpec == nil {
		return
	}

	// Change Service type to NodePort
	service.Spec.Type = corev1.ServiceTypeNodePort

	// Set the HTTPS NodePort if specified
	if nodePortSpec.HTTPSPort != nil {
		for i := range service.Spec.Ports {
			if service.Spec.Ports[i].Name == "web-tls" {
				service.Spec.Ports[i].NodePort = *nodePortSpec.HTTPSPort
				break
			}
		}
	}
}

// buildProxyOverlay builds the pod overlay for the proxy deployment.
// oauth2ProxyOpts are applied to the oauth2-proxy container before user-provided overrides.
func buildProxyOverlay(spec *konfluxv1alpha1.ProxyDeploymentSpec, oauth2ProxyOpts ...customization.ContainerOption) *customization.PodOverlay {
	// Create CA bundle volume that will be mounted in oauth2-proxy container.
	// The Secret is created by cert-manager from the oauth2-proxy-cert Certificate resource
	// (see operator/upstream-kustomizations/ui/dex/dex.yaml).
	// Uses Projected volume to:
	// 1. Only expose ca.crt (not tls.key or tls.crt) for security
	// 2. Enable automatic certificate rotation via symlinks (subPath blocks updates)
	caVolume := corev1.Volume{
		Name: oauth2proxy.CABundleVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: oauth2proxy.CABundleSecretName,
							},
							Items: []corev1.KeyToPath{
								{
									Key:  oauth2proxy.CABundleSecretKey, // "ca.crt"
									Path: oauth2proxy.CABundleFilename,  // Filename in mounted directory
								},
							},
						},
					},
				},
			},
		},
	}

	if spec == nil {
		return customization.NewPodOverlay(
			customization.WithVolumes(caVolume),
			customization.WithContainerBuilder(oauth2ProxyContainerName, oauth2ProxyOpts...)(
				customization.DeploymentContext{},
			),
		)
	}

	// Append user overrides after oauth2proxy options
	oauth2ProxyOpts = append(oauth2ProxyOpts, customization.FromContainerSpec(spec.OAuth2Proxy))

	return customization.NewPodOverlay(
		customization.WithVolumes(caVolume),
		customization.WithContainerBuilder(
			nginxContainerName,
			customization.FromContainerSpec(spec.Nginx),
		)(customization.DeploymentContext{Replicas: spec.Replicas}),
		customization.WithContainerBuilder(
			oauth2ProxyContainerName,
			oauth2ProxyOpts...,
		)(customization.DeploymentContext{Replicas: spec.Replicas}),
	)
}

// buildOAuth2ProxyOptions builds the container options for oauth2-proxy configuration.
// openShiftLoginEnabled controls whether to allow unverified emails (needed for OpenShift OAuth).
func buildOAuth2ProxyOptions(endpoint *url.URL, openShiftLoginEnabled bool) []customization.ContainerOption {
	opts := []customization.ContainerOption{
		oauth2proxy.WithProvider(),
		oauth2proxy.WithOIDCURLs(endpoint),
		oauth2proxy.WithInternalDexURLs(),
		oauth2proxy.WithCookieConfig(),
		oauth2proxy.WithAuthSettings(),
		oauth2proxy.WithCABundle(),
		oauth2proxy.WithWhitelistDomain(endpoint),
	}

	// Allow unverified emails when using OpenShift OAuth
	// OpenShift OAuth may not return email verification information
	if openShiftLoginEnabled {
		opts = append(opts, oauth2proxy.WithAllowUnverifiedEmail())
	}

	return opts
}

// buildDexOverlay builds the pod overlay for the dex deployment.
// openShiftLoginEnabled controls whether the OpenShift OAuth client secret env var is added.
func buildDexOverlay(spec *konfluxv1alpha1.DexDeploymentSpec, configMapName string, openShiftLoginEnabled bool) *customization.PodOverlay {
	opts := []customization.PodOverlayOption{
		customization.WithConfigMapVolumeUpdate(dexConfigMapVolumeName, configMapName),
	}

	// Build container options
	var containerOpts []customization.ContainerOption

	// Add OpenShift OAuth client secret env var if enabled
	if openShiftLoginEnabled {
		containerOpts = append(containerOpts, customization.WithEnv(dex.OpenShiftOAuthClientSecretEnv()))
	}

	// Add user-provided container customizations if spec is provided
	if spec != nil {
		containerOpts = append(containerOpts, customization.FromContainerSpec(spec.Dex))
		opts = append(opts, customization.WithContainerOpts(
			dexContainerName,
			customization.DeploymentContext{Replicas: spec.Replicas},
			containerOpts...,
		))
	} else if len(containerOpts) > 0 {
		// Only add container opts if we have any (e.g., OpenShift login enabled but no spec)
		opts = append(opts, customization.WithContainerOpts(
			dexContainerName,
			customization.DeploymentContext{},
			containerOpts...,
		))
	}

	return customization.NewPodOverlay(opts...)
}

// ensureUISecrets ensures that UI secrets exist and are properly configured.
// Only generates secret values if they don't already exist (preserves existing secrets).
// Uses the tracking client so secrets are tracked and not orphaned during cleanup.
func (r *KonfluxUIReconciler) ensureUISecrets(ctx context.Context, tc *tracking.Client) error {
	// Helper for the actual reconciliation logic
	ensureSecret := func(name, key string, length int, urlSafe bool) error {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: uiNamespace,
			},
		}

		// Use the tracking client's CreateOrUpdate which tracks the object
		// regardless of whether it was created, updated, or unchanged
		_, err := tc.CreateOrUpdate(ctx, secret, func() error {
			// 1. Ensure Ownership/Labels (Updates if missing)
			if err := tc.SetOwnership(secret); err != nil {
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

// reconcileDexConfigMap creates or updates the Dex ConfigMap based on the DexConfig in the CR.
// It generates a content-based hash suffix for the ConfigMap name (like kustomize),
// cleans up old ConfigMaps, and returns the new ConfigMap name.
// endpoint is used for the dex issuer URL configuration.
func (r *KonfluxUIReconciler) reconcileDexConfigMap(ctx context.Context, ui *konfluxv1alpha1.KonfluxUI, endpoint *url.URL) (string, error) {
	// Resolve whether OpenShift login should be enabled
	openShiftLoginEnabled := isOpenShiftLoginEnabled(ui, r.ClusterInfo)

	// Determine the effective endpoint: use CR spec values if provided, otherwise use the determined endpoint
	effectiveEndpoint := ui.ResolveDexEndpoint(endpoint)

	var dexConfig *dex.Config
	if ui.HasDexConfig() {
		dexParams := ui.Spec.GetDex().Config.DeepCopy()
		// Set the resolved OpenShift login value
		dexParams.ConfigureLoginWithOpenShift = &openShiftLoginEnabled
		dexConfig = dex.NewDexConfig(effectiveEndpoint, dexParams)
	} else {
		dexConfig = dex.NewDexConfig(
			effectiveEndpoint,
			&dex.DexParams{
				// EnablePasswordDB defaults to true when no connectors are configured
				ConfigureLoginWithOpenShift: &openShiftLoginEnabled,
			},
		)
	}

	configYAML, err := dexConfig.ToYAML()
	if err != nil {
		return "", fmt.Errorf("failed to marshal Dex config to YAML: %w", err)
	}

	// Use hashedconfigmap to apply the ConfigMap with content-based hash suffix
	hcm := hashedconfigmap.New(
		r.Client,
		r.Scheme,
		dexConfigMapBaseName,
		uiNamespace,
		dexConfigKey,
		dexConfigMapLabel,
		FieldManager,
	)

	result, err := hcm.Apply(ctx, string(configYAML), ui)
	if err != nil {
		return "", err
	}

	return result.ConfigMapName, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxUIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxUI{}).
		Named("konfluxui").
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
		Owns(&networkingv1.Ingress{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Complete(r)
}

// reconcileIngress creates or updates the Ingress resource for KonfluxUI when enabled.
// If ingress is disabled, the resource is not applied and will be automatically
// cleaned up by the tracking client's CleanupOrphans method.
func (r *KonfluxUIReconciler) reconcileIngress(ctx context.Context, tc *tracking.Client, ui *konfluxv1alpha1.KonfluxUI, endpoint *url.URL) error {
	log := logf.FromContext(ctx)

	// If ingress is not enabled, don't apply it.
	// The tracking client will delete it during CleanupOrphans since it wasn't applied.
	// Ingress defaults to enabled on OpenShift, disabled otherwise.
	isOnOpenShift := r.ClusterInfo != nil && r.ClusterInfo.IsOpenShift()
	if !ptr.Deref(ui.GetIngressEnabledPreference(), isOnOpenShift) {
		log.Info("Ingress is disabled, skipping (will be cleaned up if exists)")
		return nil
	}

	hostname := endpoint.Hostname()
	log.Info("Reconciling Ingress", "hostname", hostname)

	ingressResource := ingress.BuildForUI(ui, uiNamespace, hostname)

	if err := tc.ApplyOwned(ctx, ingressResource); err != nil {
		return fmt.Errorf("failed to apply ingress: %w", err)
	}

	// On OpenShift, also create a ConsoleLink for the application menu
	if isOnOpenShift {
		consoleLinkResource := consolelink.Build(endpoint)
		if err := tc.ApplyOwned(ctx, consoleLinkResource); err != nil {
			return fmt.Errorf("failed to apply ConsoleLink: %w", err)
		}
	}

	return nil
}

// reconcileOpenShiftOAuth creates or updates the ServiceAccount and Secret required for
// OpenShift OAuth integration when ConfigureLoginWithOpenShift is enabled.
// If not enabled, the resources are not applied and will be automatically
// cleaned up by the tracking client's CleanupOrphans method.
// endpoint is used to construct the OAuth redirect URI.
func (r *KonfluxUIReconciler) reconcileOpenShiftOAuth(ctx context.Context, tc *tracking.Client, ui *konfluxv1alpha1.KonfluxUI, endpoint *url.URL) error {
	log := logf.FromContext(ctx)

	// Check if OpenShift login is enabled (requires running on OpenShift and option not disabled)
	if !isOpenShiftLoginEnabled(ui, r.ClusterInfo) {
		log.Info("OpenShift login is disabled, skipping OAuth resources (will be cleaned up if exists)")
		return nil
	}

	log.Info("Reconciling OpenShift OAuth resources", "endpoint", endpoint.String())

	// Create the ServiceAccount for OAuth redirect with the full callback URI
	sa := dex.BuildOpenShiftOAuthServiceAccount(uiNamespace, endpoint)
	if err := tc.ApplyOwned(ctx, sa); err != nil {
		return fmt.Errorf("failed to apply OpenShift OAuth ServiceAccount: %w", err)
	}

	// Create the Secret for the ServiceAccount token
	secret := dex.BuildOpenShiftOAuthSecret(uiNamespace)
	if err := tc.ApplyOwned(ctx, secret); err != nil {
		return fmt.Errorf("failed to apply OpenShift OAuth Secret: %w", err)
	}

	return nil
}

// isOpenShiftLoginEnabled checks if OpenShift login should be enabled.
// Returns true if running on OpenShift AND the ConfigureLoginWithOpenShift option is nil or true.
// This means OpenShift login is enabled by default on OpenShift unless explicitly disabled.
func isOpenShiftLoginEnabled(ui *konfluxv1alpha1.KonfluxUI, clusterInfo *clusterinfo.Info) bool {
	// Must be running on OpenShift
	if clusterInfo == nil || !clusterInfo.IsOpenShift() {
		return false
	}

	// Default to true on OpenShift unless explicitly disabled
	return ptr.Deref(ui.GetOpenShiftLoginPreference(), true)
}
