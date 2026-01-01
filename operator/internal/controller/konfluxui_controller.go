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
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
	"github.com/konflux-ci/konflux-ci/operator/pkg/dex"
	"github.com/konflux-ci/konflux-ci/operator/pkg/hashedconfigmap"
	"github.com/konflux-ci/konflux-ci/operator/pkg/ingress"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/oauth2proxy"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
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

	// Dex ConfigMap constants
	dexConfigMapBaseName   = "dex"
	dexConfigKey           = "config.yaml"
	dexConfigMapLabel      = "app.kubernetes.io/managed-by-konflux-ui-reconciler"
	dexConfigMapVolumeName = "dex"
)

// uiCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. For example, when Ingress is disabled,
// the Ingress resource will be automatically deleted because it wasn't applied
// during the reconcile but has the owner label.
var uiCleanupGVKs = []schema.GroupVersionKind{
	{Group: "networking.k8s.io", Version: "v1", Kind: "Ingress"},
	// Add other GVKs here as needed for automatic cleanup
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
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.openshift.io,resources=ingresses,verbs=get

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

	// Create a tracking client for this reconcile.
	// Resources applied through this client are automatically tracked.
	// At the end of a successful reconcile, orphaned resources are cleaned up.
	tc := tracking.NewClient(r.Client, r.Scheme)

	// Ensure konflux-ui namespace exists
	if err := r.ensureNamespaceExists(ctx, tc, ui); err != nil {
		log.Error(err, "Failed to ensure namespace")
		SetFailedCondition(ui, UIConditionTypeReady, "NamespaceCreationFailed", err)
		if updateErr := r.Status().Update(ctx, ui); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Determine the hostname for ingress, dex, and oauth2-proxy configuration
	hostname, port, err := ingress.DetermineHostnameAndPort(ctx, r.Client, ui, uiNamespace, r.ClusterInfo)
	if err != nil {
		log.Error(err, "Failed to determine hostname")
		SetFailedCondition(ui, UIConditionTypeReady, "HostnameDeterminationFailed", err)
		if updateErr := r.Status().Update(ctx, ui); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	log.Info("Determined hostname for KonfluxUI", "hostname", hostname, "port", port)

	// Reconcile Dex ConfigMap first (if configured) to get the ConfigMap name
	// This must happen before applyManifests so we can set the correct ConfigMap reference
	dexConfigMapName, err := r.reconcileDexConfigMap(ctx, ui, hostname, port)
	if err != nil {
		log.Error(err, "Failed to reconcile Dex ConfigMap")
		SetFailedCondition(ui, UIConditionTypeReady, "DexConfigMapFailed", err)
		if updateErr := r.Status().Update(ctx, ui); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc, ui, dexConfigMapName, hostname, port); err != nil {
		log.Error(err, "Failed to apply manifests")
		SetFailedCondition(ui, UIConditionTypeReady, "ApplyFailed", err)
		if updateErr := r.Status().Update(ctx, ui); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Reconcile Ingress if enabled (tracked automatically, deleted if not applied)
	if err := r.reconcileIngress(ctx, tc, ui, hostname); err != nil {
		log.Error(err, "Failed to reconcile Ingress")
		SetFailedCondition(ui, UIConditionTypeReady, "IngressReconcileFailed", err)
		if updateErr := r.Status().Update(ctx, ui); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Ensure UI secrets are created
	if err := r.ensureUISecrets(ctx, tc, ui); err != nil {
		log.Error(err, "Failed to ensure UI secrets")
		SetFailedCondition(ui, UIConditionTypeReady, "SecretCreationFailed", err)
		if updateErr := r.Status().Update(ctx, ui); updateErr != nil {
			log.Error(updateErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Cleanup orphaned resources - delete any resources with our owner label
	// that weren't applied during this reconcile. This handles cases like
	// disabling Ingress (the Ingress resource is automatically deleted).
	if err := tc.CleanupOrphans(ctx, KonfluxOwnerLabel, ui.Name, uiCleanupGVKs); err != nil {
		log.Error(err, "Failed to cleanup orphaned resources")
		SetFailedCondition(ui, UIConditionTypeReady, "CleanupFailed", err)
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

	// Update ingress status
	updateIngressStatus(ui, hostname, port)

	// Final status update
	if err := r.Status().Update(ctx, ui); err != nil {
		log.Error(err, "Failed to update final status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxUI")
	return ctrl.Result{}, nil
}

// updateIngressStatus updates the ingress status fields on the KonfluxUI CR.
func updateIngressStatus(ui *konfluxv1alpha1.KonfluxUI, hostname, port string) {
	ingressEnabled := ui.Spec.Ingress != nil && ui.Spec.Ingress.Enabled

	// Build the URL
	var url string
	if hostname != "" {
		if port != "" {
			url = fmt.Sprintf("https://%s:%s", hostname, port)
		} else {
			url = fmt.Sprintf("https://%s", hostname)
		}
	}

	ui.Status.Ingress = &konfluxv1alpha1.IngressStatus{
		Enabled:  ingressEnabled,
		Hostname: hostname,
		URL:      url,
	}
}

func (r *KonfluxUIReconciler) ensureNamespaceExists(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.KonfluxUI) error {
	objects, err := r.ObjectStore.GetForComponent(manifests.UI)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for UI: %w", err)
	}

	for _, obj := range objects {
		// Apply customizations for deployments
		if namespace, ok := obj.(*corev1.Namespace); ok {
			// Set ownership labels and owner reference
			if err := setOwnership(namespace, owner, string(manifests.UI), r.Scheme); err != nil {
				return fmt.Errorf("failed to set ownership for %s/%s (%s) from %s: %w",
					namespace.GetNamespace(), namespace.GetName(), getKind(namespace), manifests.UI, err)
			}
			if err := tc.ApplyObject(ctx, namespace, FieldManagerUI); err != nil {
				return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
					namespace.GetNamespace(), namespace.GetName(), getKind(namespace), manifests.UI, err)
			}
		}
	}
	return nil
}

// applyManifests loads and applies all embedded manifests to the cluster.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
// dexConfigMapName is the name of the Dex ConfigMap to use (empty if not configured).
// hostname and port are used to configure oauth2-proxy.
func (r *KonfluxUIReconciler) applyManifests(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.KonfluxUI, dexConfigMapName, hostname, port string) error {
	log := logf.FromContext(ctx)

	objects, err := r.ObjectStore.GetForComponent(manifests.UI)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for UI: %w", err)
	}

	for _, obj := range objects {
		// Apply customizations for deployments
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if err := applyUIDeploymentCustomizations(deployment, owner.Spec, dexConfigMapName, hostname, port); err != nil {
				return fmt.Errorf("failed to apply customizations to deployment %s: %w", deployment.Name, err)
			}
		}

		// Set ownership labels and owner reference
		if err := setOwnership(obj, owner, string(manifests.UI), r.Scheme); err != nil {
			return fmt.Errorf("failed to set ownership for %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), getKind(obj), manifests.UI, err)
		}

		if err := tc.ApplyObject(ctx, obj, FieldManagerUI); err != nil {
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
func applyUIDeploymentCustomizations(deployment *appsv1.Deployment, spec konfluxv1alpha1.KonfluxUISpec, dexConfigMapName, hostname, port string) error {
	switch deployment.Name {
	case proxyDeploymentName:
		if spec.Proxy != nil {
			deployment.Spec.Replicas = &spec.Proxy.Replicas
		}
		// Build oauth2-proxy options based on hostname and port
		oauth2ProxyOpts := buildOAuth2ProxyOptions(hostname, port)
		if err := buildProxyOverlay(spec.Proxy, oauth2ProxyOpts...).ApplyToDeployment(deployment); err != nil {
			return err
		}
	case dexDeploymentName:
		if spec.Dex != nil {
			deployment.Spec.Replicas = &spec.Dex.Replicas
		}
		if err := buildDexOverlay(spec.Dex, dexConfigMapName).ApplyToDeployment(deployment); err != nil {
			return err
		}
	}
	return nil
}

// buildProxyOverlay builds the pod overlay for the proxy deployment.
// oauth2ProxyOpts are applied to the oauth2-proxy container before user-provided overrides.
func buildProxyOverlay(spec *konfluxv1alpha1.ProxyDeploymentSpec, oauth2ProxyOpts ...customization.ContainerOption) *customization.PodOverlay {
	if spec == nil {
		return customization.BuildPodOverlay(
			customization.DeploymentContext{},
			customization.WithContainerBuilder(oauth2ProxyContainerName, oauth2ProxyOpts...),
		)
	}

	// Append user overrides after oauth2proxy options
	oauth2ProxyOpts = append(oauth2ProxyOpts, customization.FromContainerSpec(spec.OAuth2Proxy))

	return customization.BuildPodOverlay(
		customization.DeploymentContext{Replicas: spec.Replicas},
		customization.WithContainerBuilder(
			nginxContainerName,
			customization.FromContainerSpec(spec.Nginx),
		),
		customization.WithContainerBuilder(
			oauth2ProxyContainerName,
			oauth2ProxyOpts...,
		),
	)
}

// buildOAuth2ProxyOptions builds the container options for oauth2-proxy configuration.
func buildOAuth2ProxyOptions(hostname, port string) []customization.ContainerOption {
	return []customization.ContainerOption{
		oauth2proxy.WithProvider(),
		oauth2proxy.WithOIDCURLs(hostname, port),
		oauth2proxy.WithInternalDexURLs(),
		oauth2proxy.WithCookieConfig(),
		oauth2proxy.WithAuthSettings(),
		oauth2proxy.WithTLSSkipVerify(),
		oauth2proxy.WithWhitelistDomain(hostname, port),
	}
}

// buildDexOverlay builds the pod overlay for the dex deployment.
func buildDexOverlay(spec *konfluxv1alpha1.DexDeploymentSpec, configMapName string) *customization.PodOverlay {
	opts := []customization.PodOverlayOption{
		customization.WithConfigMapVolumeUpdate(dexConfigMapVolumeName, configMapName),
	}

	// Add container customizations if spec is provided
	if spec != nil {
		opts = append(opts, customization.WithContainerOpts(
			dexContainerName,
			customization.DeploymentContext{Replicas: spec.Replicas},
			customization.FromContainerSpec(spec.Dex),
		))
	}

	return customization.NewPodOverlay(opts...)
}

// ensureUISecrets ensures that UI secrets exist and are properly configured.
// Only generates secret values if they don't already exist (preserves existing secrets).
// Uses the tracking client so secrets are tracked and not orphaned during cleanup.
func (r *KonfluxUIReconciler) ensureUISecrets(ctx context.Context, tc *tracking.Client, ui *konfluxv1alpha1.KonfluxUI) error {
	// Helper for the actual reconciliation logic
	ensureSecret := func(name, key string, length int, urlSafe bool) error {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: uiNamespace,
			},
		}

		// Use the tracking client so Create/Update operations are tracked
		_, err := controllerutil.CreateOrUpdate(ctx, tc, secret, func() error {
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

// reconcileDexConfigMap creates or updates the Dex ConfigMap based on the DexConfig in the CR.
// It generates a content-based hash suffix for the ConfigMap name (like kustomize),
// cleans up old ConfigMaps, and returns the new ConfigMap name.
// hostname and port are used for the dex issuer URL configuration.
func (r *KonfluxUIReconciler) reconcileDexConfigMap(ctx context.Context, ui *konfluxv1alpha1.KonfluxUI, hostname, port string) (string, error) {
	var dexConfig *dex.Config
	// Check if DexConfig is configured
	if ui.Spec.Dex != nil && ui.Spec.Dex.Config != nil {
		dexParams := ui.Spec.Dex.Config.DeepCopy()
		// Use ingress-determined hostname and port if not explicitly provided in dexParams
		if dexParams.Hostname == "" {
			dexParams.Hostname = hostname
		}
		if dexParams.Port == "" {
			dexParams.Port = port
		}
		dexConfig = dex.NewDexConfig(dexParams)
	} else {
		dexConfig = dex.NewDexConfig(
			&dex.DexParams{
				Hostname: hostname,
				Port:     port,
				// password db must be enabled when the connector
				// list is empty
				EnablePasswordDB: true,
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
		FieldManagerUI,
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
		Owns(&appsv1.Deployment{}, builder.WithPredicates(deploymentReadinessPredicate)).
		Owns(&corev1.Service{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(labelsOrAnnotationsChangedPredicate)).
		Owns(&corev1.Secret{}, builder.WithPredicates(labelsOrAnnotationsChangedPredicate)).
		Owns(&corev1.Namespace{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&rbacv1.Role{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&rbacv1.RoleBinding{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&rbacv1.ClusterRole{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&rbacv1.ClusterRoleBinding{}, builder.WithPredicates(generationChangedPredicate)).
		Owns(&networkingv1.Ingress{}, builder.WithPredicates(generationChangedPredicate)).
		Complete(r)
}

// reconcileIngress creates or updates the Ingress resource for KonfluxUI when enabled.
// If ingress is disabled, the resource is not applied and will be automatically
// cleaned up by the tracking client's CleanupOrphans method.
func (r *KonfluxUIReconciler) reconcileIngress(ctx context.Context, tc *tracking.Client, ui *konfluxv1alpha1.KonfluxUI, hostname string) error {
	log := logf.FromContext(ctx)

	// If ingress is not enabled, don't apply it.
	// The tracking client will delete it during CleanupOrphans since it wasn't applied.
	if ui.Spec.Ingress == nil || !ui.Spec.Ingress.Enabled {
		log.Info("Ingress is disabled, skipping (will be cleaned up if exists)")
		return nil
	}

	log.Info("Reconciling Ingress", "hostname", hostname)

	ingressResource := ingress.BuildForUI(ui, uiNamespace, hostname)

	// Set ownership
	if err := setOwnership(ingressResource, ui, string(manifests.UI), r.Scheme); err != nil {
		return fmt.Errorf("failed to set ownership for ingress: %w", err)
	}

	if err := tc.ApplyObject(ctx, ingressResource, FieldManagerUI); err != nil {
		return fmt.Errorf("failed to apply ingress: %w", err)
	}

	return nil
}
