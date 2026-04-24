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

package buildservice

import (
	"context"
	"encoding/json"
	"fmt"

	securityv1 "github.com/openshift/api/security/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/common"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/predicate"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
	"github.com/konflux-ci/konflux-ci/operator/pkg/hashedconfigmap"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxBuildService CR.
	CRName = "konflux-build-service"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-buildservice-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxBuildService"

	// Deployment names
	buildControllerManagerDeploymentName = "build-service-controller-manager"

	// Container names
	buildManagerContainerName = "manager"

	// PAC_WEBHOOK_URL is used by the build-service both for triggering pipelines
	// (internal PaC endpoint) and as the default webhook URL for git repositories.
	// It is only set on non-OpenShift when no webhookURLs are configured, so that
	// Kind works out of the box. When webhookURLs are configured, PAC_WEBHOOK_URL
	// is left unset to avoid overriding the webhook config.
	// On OpenShift, the build-service auto-discovers the PaC Route.
	pacWebhookURLEnvName      = "PAC_WEBHOOK_URL"
	pacWebhookURLNonOpenShift = "http://pipelines-as-code-controller.pipelines-as-code.svc.cluster.local:8180"

	// Webhook config constants for the optional per-provider webhook URL mapping.
	// The volume, mount, and arg are baked into the manifest with optional: true.
	// The operator only needs to create the ConfigMap and update the volume reference.
	webhookConfigBaseName  = "webhook-config"
	webhookConfigNamespace = "build-service"
	webhookConfigDataKey   = "webhook-config.json"
	webhookConfigLabel     = "konflux.konflux-ci.dev/webhook-config"
	webhookConfigVolName   = "webhook-config"
)

// BuildServiceCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. All resources managed by this controller are always
// applied (SCC is platform-dependent but cluster type doesn't change at runtime),
// so no cleanup GVKs are needed.
var BuildServiceCleanupGVKs = []schema.GroupVersionKind{}

// BuildServiceClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. All cluster-scoped resources managed by this controller are always
// applied (SCC is only created on OpenShift, but cluster type doesn't change at runtime),
// so no allow list is needed.
var BuildServiceClusterScopedAllowList tracking.ClusterScopedAllowList = nil

// KonfluxBuildServiceReconciler reconciles a KonfluxBuildService object
type KonfluxBuildServiceReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
	ClusterInfo *clusterinfo.Info
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxbuildservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxbuildservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxbuildservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=services;secrets;serviceaccounts,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,resourceNames=build-service-leader-election-role,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,resourceNames=build-pipeline-config-read-only-binding;build-service-leader-election-rolebinding,verbs=bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=appstudio-pipelines-runner;build-service-manager-role;build-service-metrics-auth-role,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=build-pipeline-runner-rolebinding;build-service-manager-rolebinding;build-service-metrics-auth-rolebinding,verbs=bind
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;list;watch;create;patch

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

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), buildService, crKind)

	// Create a tracking client with ownership config for this reconcile.
	// Resources applied through this client are automatically tracked and owned.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             buildService,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.BuildService),
		FieldManager:      FieldManager,
	})

	// Ensure the build-service namespace exists before creating ConfigMaps in it.
	if err := common.EnsureNamespaceExists(ctx, r.ObjectStore, manifests.BuildService, webhookConfigNamespace, tc); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonNamespaceCreationFailed, "ensure namespace exists")
	}

	// Reconcile webhook config ConfigMap.
	// Must happen before applyManifests so the hashed ConfigMap name is available for the volume reference.
	webhookConfigMapName, err := r.reconcileWebhookConfig(ctx, buildService)
	if err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonConfigMapFailed, "reconcile webhook config")
	}

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc, buildService, webhookConfigMapName); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Cleanup orphaned resources - delete any resources with our owner label
	// that weren't applied during this reconcile.
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, buildService.Name, BuildServiceCleanupGVKs,
		tracking.WithClusterScopedAllowList(BuildServiceClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Check the status of owned deployments and update KonfluxBuildService status
	if err := condition.UpdateComponentStatuses(ctx, r.Client, buildService); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update status
	if err := r.Status().Update(ctx, buildService); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxBuildService")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
// webhookConfigMapName is the hashed ConfigMap name for the webhook config (empty if not configured).
func (r *KonfluxBuildServiceReconciler) applyManifests(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.KonfluxBuildService, webhookConfigMapName string) error {
	log := logf.FromContext(ctx)

	objects, err := r.ObjectStore.GetForComponent(manifests.BuildService)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for BuildService: %w", err)
	}

	for _, obj := range objects {
		// Apply customizations for deployments
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if err := applyBuildServiceDeploymentCustomizations(deployment, owner.Spec, r.ClusterInfo, webhookConfigMapName); err != nil {
				return fmt.Errorf("failed to apply customizations to deployment %s: %w", deployment.Name, err)
			}
		}

		// Skip OpenShift SecurityContextConstraints when not running on OpenShift
		if _, isSCC := obj.(*securityv1.SecurityContextConstraints); isSCC {
			if r.ClusterInfo == nil || !r.ClusterInfo.IsOpenShift() {
				log.V(1).Info("Skipping OpenShift-specific resource: not running on OpenShift",
					"kind", "SecurityContextConstraints",
					"name", obj.GetName(),
				)
				continue
			}
		}

		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.BuildService, err)
		}
	}
	return nil
}

// applyBuildServiceDeploymentCustomizations applies user-defined and platform-specific
// customizations to BuildService deployments.
func applyBuildServiceDeploymentCustomizations(deployment *appsv1.Deployment, spec konfluxv1alpha1.KonfluxBuildServiceSpec, clusterInfo *clusterinfo.Info, webhookConfigMapName string) error {
	switch deployment.Name {
	case buildControllerManagerDeploymentName:
		if spec.BuildControllerManager != nil {
			deployment.Spec.Replicas = &spec.BuildControllerManager.Replicas
		}
		if err := buildBuildControllerManagerOverlay(spec, clusterInfo, webhookConfigMapName).ApplyToDeployment(deployment); err != nil {
			return err
		}
	}
	return nil
}

// buildBuildControllerManagerOverlay builds the pod overlay for the controller-manager deployment.
// On non-OpenShift without webhookURLs, PAC_WEBHOOK_URL is set to the default PaC service
// endpoint so that Kind works out of the box. When webhookURLs are configured, PAC_WEBHOOK_URL
// is not set because it would override the webhook config JSON.
// On OpenShift, PAC_WEBHOOK_URL is never set — the build-service auto-discovers the PaC Route.
// When webhookConfigMapName is non-empty, the webhook-config volume (baked into the manifest
// with optional: true) is updated to reference the hashed ConfigMap.
// User-provided env vars from the CR spec always take precedence.
func buildBuildControllerManagerOverlay(spec konfluxv1alpha1.KonfluxBuildServiceSpec, clusterInfo *clusterinfo.Info, webhookConfigMapName string) *customization.PodOverlay {
	var containerOpts []customization.ContainerOption
	var podOpts []customization.PodOverlayOption

	// On non-OpenShift without webhookURLs, set PAC_WEBHOOK_URL so Kind works
	// without extra configuration. When webhookURLs are configured, leave it
	// unset so the webhook config JSON takes precedence.
	if len(spec.WebhookURLs) == 0 && (clusterInfo == nil || !clusterInfo.IsOpenShift()) {
		containerOpts = append(containerOpts, customization.WithEnv(corev1.EnvVar{
			Name:  pacWebhookURLEnvName,
			Value: pacWebhookURLNonOpenShift,
		}))
	}

	// Update the volume reference to the hashed ConfigMap.
	// The volume, mount, and arg are already in the manifest with optional: true.
	if webhookConfigMapName != "" {
		podOpts = append(podOpts, customization.WithConfigMapVolumeUpdate(webhookConfigVolName, webhookConfigMapName))
	}

	if spec.BuildControllerManager == nil {
		podOpts = append(podOpts,
			customization.WithContainerOpts(buildManagerContainerName, customization.DeploymentContext{}, containerOpts...),
		)
		return customization.NewPodOverlay(podOpts...)
	}

	containerOpts = append(containerOpts,
		customization.FromContainerSpec(spec.BuildControllerManager.Manager),
		customization.WithLeaderElection(),
	)

	podOpts = append(podOpts,
		customization.WithContainerBuilder(buildManagerContainerName, containerOpts...)(customization.DeploymentContext{Replicas: spec.BuildControllerManager.Replicas}),
	)
	return customization.NewPodOverlay(podOpts...)
}

// reconcileWebhookConfig ensures the webhook config ConfigMap exists.
// The ConfigMap is always created: with the webhookURLs mapping when configured,
// or with an empty JSON object when not configured. This guarantees the
// -webhook-config-path flag (baked into the manifest) always points to a valid file.
func (r *KonfluxBuildServiceReconciler) reconcileWebhookConfig(ctx context.Context, owner *konfluxv1alpha1.KonfluxBuildService) (string, error) {
	log := logf.FromContext(ctx)

	data := owner.Spec.WebhookURLs
	if data == nil {
		data = map[string]string{}
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal webhookURLs to JSON: %w", err)
	}

	hcm := hashedconfigmap.New(
		r.Client,
		r.Scheme,
		webhookConfigBaseName,
		webhookConfigNamespace,
		webhookConfigDataKey,
		webhookConfigLabel,
		FieldManager,
	)

	result, err := hcm.Apply(ctx, string(jsonData), owner)
	if err != nil {
		return "", err
	}

	log.Info("Applied webhook config ConfigMap", "name", result.ConfigMapName)
	return result.ConfigMapName, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxBuildServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxBuildService{}).
		Named("konfluxbuildservice").
		// Use predicates to filter out unnecessary updates and prevent reconcile loops
		// Deployments: watch spec changes AND readiness status changes
		Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.DeploymentReadinessPredicate)).
		Owns(&corev1.Service{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Namespace{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Complete(r)
}
