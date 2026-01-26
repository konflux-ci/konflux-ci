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

package integrationservice

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/ui"
	"github.com/konflux-ci/konflux-ci/operator/internal/predicate"
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxIntegrationService CR.
	CRName = "konflux-integration-service"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-integrationservice-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxIntegrationService"

	// Deployment names
	controllerManagerDeploymentName = "integration-service-controller-manager"

	// Container names
	managerContainerName = "manager"
)

// IntegrationServiceCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. All resources managed by this controller are always
// applied, so no cleanup GVKs are needed (they're always tracked and never become orphans).
var IntegrationServiceCleanupGVKs = []schema.GroupVersionKind{}

// IntegrationServiceClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. All cluster-scoped resources managed by this controller are always
// applied, so no allow list is needed (they're always tracked and never become orphans).
var IntegrationServiceClusterScopedAllowList tracking.ClusterScopedAllowList = nil

// KonfluxIntegrationServiceReconciler reconciles a KonfluxIntegrationService object
type KonfluxIntegrationServiceReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxintegrationservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxintegrationservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxintegrationservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxuis,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=services;secrets;serviceaccounts,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,resourceNames=integration-service-leader-election-role,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,resourceNames=integration-service-leader-election-rolebinding,verbs=bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=integration-service-integrationtestscenario-admin-role;integration-service-integrationtestscenario-editor-role;integration-service-integrationtestscenario-viewer-role;integration-service-manager-role;integration-service-metrics-auth-role;integration-service-snapshot-garbage-collector;integration-service-tekton-editor-role;konflux-integration-runner,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=integration-service-manager-rolebinding;integration-service-metrics-auth-rolebinding;integration-service-snapshot-garbage-collector;integration-service-tekton-role-binding;kyverno-background-controller-konflux-integration-runner,verbs=bind
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates;issuers,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=kyverno.io,resources=clusterpolicies,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;create;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;watch;create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *KonfluxIntegrationServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxIntegrationService instance
	integrationService := &konfluxv1alpha1.KonfluxIntegrationService{}
	if err := r.Get(ctx, req.NamespacedName, integrationService); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxIntegrationService", "name", integrationService.Name)

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), integrationService, crKind)

	// Create a tracking client with ownership config for this reconcile.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             integrationService,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.Integration),
		FieldManager:      FieldManager,
	})

	// Fetch KonfluxUI to get console URL
	konfluxUI := &konfluxv1alpha1.KonfluxUI{}
	consoleURL := ""
	if err := r.Get(ctx, types.NamespacedName{Name: ui.CRName}, konfluxUI); err != nil {
		// Log warning but don't fail - URL might not be available yet
		log.Info("KonfluxUI not found, console URL will not be set", "error", err)
	} else if konfluxUI.Status.Ingress != nil && konfluxUI.Status.Ingress.URL != "" {
		consoleURL = konfluxUI.Status.Ingress.URL
		log.Info("Found console URL from KonfluxUI", "url", consoleURL)
	}

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc, integrationService, consoleURL); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Cleanup orphaned resources
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, integrationService.Name, IntegrationServiceCleanupGVKs,
		tracking.WithClusterScopedAllowList(IntegrationServiceClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Check the status of owned deployments and update KonfluxIntegrationService status
	if err := condition.UpdateComponentStatuses(ctx, r.Client, integrationService); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update status
	if err := r.Status().Update(ctx, integrationService); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxIntegrationService")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxIntegrationServiceReconciler) applyManifests(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.KonfluxIntegrationService, consoleURL string) error {
	log := logf.FromContext(ctx)

	objects, err := r.ObjectStore.GetForComponent(manifests.Integration)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for Integration: %w", err)
	}

	for _, obj := range objects {
		// Apply customizations for deployments
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if err := applyIntegrationServiceDeploymentCustomizations(deployment, owner.Spec, consoleURL); err != nil {
				return fmt.Errorf("failed to apply customizations to deployment %s: %w", deployment.Name, err)
			}
		}

		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			gvk := obj.GetObjectKind().GroupVersionKind()
			// TODO: Remove this once we decide if we want to have a dependency on Kyverno
			if gvk.Group == constant.KyvernoGroup {
				log.Info("Skipping resource: CRD not installed",
					"kind", gvk.Kind,
					"apiVersion", gvk.GroupVersion().String(),
					"namespace", obj.GetNamespace(),
					"name", obj.GetName(),
				)
				continue
			}
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.Integration, err)
		}
	}
	return nil
}

// applyIntegrationServiceDeploymentCustomizations applies user-defined customizations to IntegrationService deployments.
func applyIntegrationServiceDeploymentCustomizations(deployment *appsv1.Deployment, spec konfluxv1alpha1.KonfluxIntegrationServiceSpec, consoleURL string) error {
	switch deployment.Name {
	case controllerManagerDeploymentName:
		if spec.IntegrationControllerManager != nil {
			deployment.Spec.Replicas = &spec.IntegrationControllerManager.Replicas
		}
		if err := buildControllerManagerOverlay(spec.IntegrationControllerManager, consoleURL).ApplyToDeployment(deployment); err != nil {
			return err
		}
	}
	return nil
}

// buildControllerManagerOverlay builds the pod overlay for the controller-manager deployment.
func buildControllerManagerOverlay(spec *konfluxv1alpha1.ControllerManagerDeploymentSpec, consoleURL string) *customization.PodOverlay {
	// Build console URL template for pipeline run links in the UI.
	// Format: https://<host>/ns/{{ .Namespace }}/pipelinerun/{{ .PipelineRunName }}
	consoleURLTemplate := ""
	if consoleURL != "" {
		consoleURLTemplate = fmt.Sprintf("%s/ns/{{ .Namespace }}/pipelinerun/{{ .PipelineRunName }}",
			strings.TrimSuffix(consoleURL, "/"))
	}

	// Determine replicas and manager spec (default replicas to 1 if no spec)
	replicas := int32(1)
	var managerSpec *konfluxv1alpha1.ContainerSpec
	if spec != nil {
		replicas = spec.Replicas
		managerSpec = spec.Manager
	}

	return customization.BuildPodOverlay(
		customization.DeploymentContext{Replicas: replicas},
		customization.WithContainerBuilder(
			managerContainerName,
			customization.FromContainerSpec(managerSpec),
			customization.WithLeaderElection(),
			customization.WithEnvOverride("CONSOLE_URL", consoleURLTemplate),
		),
	)
}

// mapKonfluxUIToIntegrationService maps KonfluxUI events to KonfluxIntegrationService reconcile requests.
func (r *KonfluxIntegrationServiceReconciler) mapKonfluxUIToIntegrationService(_ context.Context, _ client.Object) []ctrl.Request {
	// Return reconcile request for the singleton KonfluxIntegrationService CR
	return []ctrl.Request{{NamespacedName: types.NamespacedName{Name: CRName}}}
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxIntegrationServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxIntegrationService{}).
		Named("konfluxintegrationservice").
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
		// Watch KonfluxUI CR for ingress status changes to update console URL
		Watches(&konfluxv1alpha1.KonfluxUI{},
			handler.EnqueueRequestsFromMapFunc(r.mapKonfluxUIToIntegrationService),
			builder.WithPredicates(predicate.KonfluxUIIngressStatusChangedPredicate)).
		Complete(r)
}
