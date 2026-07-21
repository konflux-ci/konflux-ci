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

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/common"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	crdhandler "github.com/konflux-ci/konflux-ci/operator/internal/controller/handler"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/ui"
	"github.com/konflux-ci/konflux-ci/operator/internal/predicate"
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
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

	// CronJob names
	snapshotGCCronJobName = "integration-service-snapshot-garbage-collector"

	// Container names
	managerContainerName    = "manager"
	snapshotGCContainerName = "test-gc"

	// integrationServiceNamespace is the operand namespace for integration-service resources.
	integrationServiceNamespace = "integration-service"

	// Env var names for pipeline run timeout configuration.
	envPipelineTimeout = "PIPELINE_TIMEOUT"
	envTasksTimeout    = "TASKS_TIMEOUT"
	envFinallyTimeout  = "FINALLY_TIMEOUT"

	// Env var names for snapshot garbage collector retention configuration.
	// The binary reads these after flag.Parse(), so they override the command-line args.
	envPRSnapshotsToKeep              = "PR_SNAPSHOTS_TO_KEEP"
	envNonPRSnapshotsToKeep           = "NON_PR_SNAPSHOTS_TO_KEEP"
	envMinSnapshotsToKeepPerComponent = "MIN_SNAPSHOTS_TO_KEEP_PER_COMPONENT"
)

// IntegrationServiceCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. Metrics scrape resources may be skipped during apply
// (componentMetrics disabled) or removed across releases while metrics stay enabled.
var IntegrationServiceCleanupGVKs = append([]schema.GroupVersionKind(nil), kubernetes.ComponentMetricsOrphanCleanupGVKs...)

// IntegrationServiceClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. Only metrics scrape ClusterRoles and ClusterRoleBindings are listed;
// other cluster-scoped RBAC managed by this controller is always applied and stays tracked.
var IntegrationServiceClusterScopedAllowList = tracking.ClusterScopedAllowList{
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}: sets.New(
		"integration-service-metrics-reader",
	),
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"}: sets.New(
		"prometheus-integration-service-metrics-reader",
	),
}

// KonfluxIntegrationServiceReconciler reconciles a KonfluxIntegrationService object
type KonfluxIntegrationServiceReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	ObjectStore         *manifests.ObjectStore
	TokenCreator        kubernetes.TokenCreator
	Clock               clock.Clock
	TokenRotationEvents <-chan event.TypedGenericEvent[client.Object]
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxintegrationservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxintegrationservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxintegrationservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxuis,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=services;secrets;serviceaccounts,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts/token,verbs=create
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,resourceNames=integration-service-leader-election-role,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,resourceNames=integration-service-leader-election-rolebinding,verbs=bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=integration-service-integrationtestscenario-admin-role;integration-service-integrationtestscenario-editor-role;integration-service-integrationtestscenario-viewer-role;integration-service-manager-role;integration-service-metrics-auth-role;integration-service-metrics-reader;integration-service-snapshot-garbage-collector;integration-service-tekton-editor-role;konflux-integration-runner,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=integration-service-manager-rolebinding;integration-service-metrics-auth-rolebinding;integration-service-snapshot-garbage-collector;integration-service-tekton-role-binding;prometheus-integration-service-metrics-reader,verbs=bind
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates;issuers,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;patch
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

	scrapeResult := reconcile.Result{}
	if integrationService.Spec.ComponentMetrics.IsEnabled() && r.TokenCreator != nil {
		// Deferred ServiceMonitor apply: mint scrape token, apply SM after token, resync nudges.
		scraper := kubernetes.OperandMetricsScraperSA(integrationServiceNamespace)
		var scrapeErr error
		scrapeResult, scrapeErr = common.ReconcilePrometheusScrapeToken(ctx, common.ScrapeTokenReconcilerConfig{
			Client:             r.Client,
			Clock:              r.Clock,
			TokenCreator:       r.TokenCreator,
			Scraper:            scraper,
			OperandNamespace:   integrationServiceNamespace,
			ServiceMonitorName: "integration-service",
			Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
				return tc.ApplyOwned(applyCtx, secret)
			},
			ApplyServiceMonitor: func(applyCtx context.Context) error {
				objects, storeErr := r.ObjectStore.GetForComponent(manifests.Integration)
				if storeErr != nil {
					return fmt.Errorf("get manifests for ServiceMonitor apply: %w", storeErr)
				}
				sm, ok := common.OperandServiceMonitorFromObjects(objects, integrationServiceNamespace, "integration-service")
				if !ok {
					return fmt.Errorf("operand ServiceMonitor %s/%s not found in embedded manifests",
						integrationServiceNamespace, "integration-service")
				}
				if err := common.ApplyMetricsScraperBindingSubjects(integrationServiceNamespace, sm); err != nil {
					return fmt.Errorf("apply metrics scraper binding subjects for ServiceMonitor: %w", err)
				}
				return tc.ApplyOwned(applyCtx, sm)
			},
		})
		if scrapeErr != nil {
			return errHandler.HandleWithReason(ctx, scrapeErr, condition.ReasonApplyFailed, "reconcile prometheus scrape token")
		}
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
	return scrapeResult, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxIntegrationServiceReconciler) applyManifests(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.KonfluxIntegrationService, consoleURL string) error {
	log := logf.FromContext(ctx)

	metricsEnabled := owner.Spec.ComponentMetrics.IsEnabled()
	deferServiceMonitor := metricsEnabled && r.TokenCreator != nil

	objects, err := r.ObjectStore.GetForComponent(manifests.Integration)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for Integration: %w", err)
	}

	for _, obj := range objects {
		// Deferred ServiceMonitor apply: skip operand SM until ReconcilePrometheusScrapeToken
		// applies it after prometheus-scrape-token is readable.
		if deferServiceMonitor && kubernetes.IsComponentMetricsServiceMonitor(obj) {
			log.V(1).Info("Deferring operand ServiceMonitor apply until scrape token is ready",
				"kind", tracking.GetKind(obj),
				"name", obj.GetName(),
				"namespace", obj.GetNamespace(),
			)
			continue
		}
		if !metricsEnabled && kubernetes.IsComponentMetricsScrapeResource(obj) {
			log.V(1).Info("Skipping component metrics scrape resource",
				"kind", tracking.GetKind(obj),
				"name", obj.GetName(),
				"namespace", obj.GetNamespace(),
			)
			continue
		}

		// Apply customizations for deployments
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if err := applyIntegrationServiceDeploymentCustomizations(deployment, owner.Spec.KonfluxIntegrationServiceConfigSpec, consoleURL); err != nil {
				return fmt.Errorf("failed to apply customizations to deployment %s: %w", deployment.Name, err)
			}
		}

		// Apply customizations for the snapshot GC CronJob
		if cronJob, ok := obj.(*batchv1.CronJob); ok && cronJob.Name == snapshotGCCronJobName {
			if err := applySnapshotGCCustomizations(cronJob, owner.Spec.KonfluxIntegrationServiceConfigSpec); err != nil {
				return fmt.Errorf("failed to apply customizations to CronJob %s: %w", cronJob.Name, err)
			}
		}

		if err := common.ApplyMetricsScraperBindingSubjects(integrationServiceNamespace, obj); err != nil {
			return fmt.Errorf("apply metrics scraper binding subjects for %s: %w", obj.GetName(), err)
		}

		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.Integration, err)
		}
	}
	return nil
}

// applyIntegrationServiceDeploymentCustomizations applies user-defined customizations to IntegrationService deployments.
func applyIntegrationServiceDeploymentCustomizations(deployment *appsv1.Deployment, spec konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec, consoleURL string) error {
	switch deployment.Name {
	case controllerManagerDeploymentName:
		if spec.IntegrationControllerManager != nil {
			deployment.Spec.Replicas = &spec.IntegrationControllerManager.Replicas
		}
		if err := buildControllerManagerOverlay(spec.IntegrationControllerManager, consoleURL, spec).ApplyToDeployment(deployment); err != nil {
			return err
		}
	}
	return nil
}

// buildControllerManagerOverlay builds the pod overlay for the controller-manager deployment.
// Typed timeout fields (PipelineTimeout, TasksTimeout, FinallyTimeout) are applied last and
// take precedence over any env entry with the same name in integrationControllerManager.manager.env.
// When not set in the CRD, the upstream integration-service defaults apply.
func buildControllerManagerOverlay(spec *konfluxv1alpha1.ControllerManagerDeploymentSpec, consoleURL string, integrationSpec konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec) *customization.PodOverlay {
	consoleURLTemplate := ""
	consoleURLTasklogTemplate := ""
	if consoleURL != "" {
		base := strings.TrimSuffix(consoleURL, "/")
		consoleURLTemplate = base + "/ns/{{ .Namespace }}/pipelinerun/{{ .PipelineRunName }}"
		consoleURLTasklogTemplate = consoleURLTemplate + "/logs/{{ .TaskName }}"
	}

	replicas := int32(1)
	var managerSpec *konfluxv1alpha1.ContainerSpec
	if spec != nil {
		replicas = spec.Replicas
		managerSpec = spec.Manager
	}

	deployCtx := customization.DeploymentContext{Replicas: replicas}
	return customization.NewPodOverlay(
		customization.WithContainerOpts(managerContainerName, deployCtx,
			customization.FromContainerSpec(managerSpec),
			customization.WithEnvOverride("CONSOLE_URL", consoleURLTemplate),
			customization.WithEnvOverride("CONSOLE_URL_TASKLOG", consoleURLTasklogTemplate),
			customization.WithOptionalEnvOverride(envPipelineTimeout, integrationSpec.PipelineTimeout),
			customization.WithOptionalEnvOverride(envTasksTimeout, integrationSpec.TasksTimeout),
			customization.WithOptionalEnvOverride(envFinallyTimeout, integrationSpec.FinallyTimeout),
		),
		customization.WithLeaderElection(managerContainerName, replicas),
	)
}

// applySnapshotGCCustomizations applies user-defined customizations to the snapshot GC CronJob.
// The ContainerSpec (resources, env) is applied first via ApplyCronJobContainerSpec, which
// validates the container exists. Typed retention fields are validated separately and applied
// last, taking precedence over any same-named entry in snapshotGarbageCollector.env.
func applySnapshotGCCustomizations(cj *batchv1.CronJob, integrationSpec konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec) error {
	if err := customization.ApplyCronJobContainerSpec(cj, snapshotGCContainerName, integrationSpec.SnapshotGarbageCollector); err != nil {
		return err
	}

	hasTypedFields := integrationSpec.PRSnapshotsToKeep != "" ||
		integrationSpec.NonPRSnapshotsToKeep != "" ||
		integrationSpec.MinSnapshotsToKeepPerComponent != ""

	if !hasTypedFields {
		return nil
	}

	// Validate again: ApplyCronJobContainerSpec skips validation when
	// SnapshotGarbageCollector is nil, so typed-fields-only configs still
	// need the container existence check.
	if err := customization.ValidateCronJobContainer(cj, snapshotGCContainerName); err != nil {
		return err
	}

	envOverlay := customization.BuildPodOverlay(
		customization.DeploymentContext{},
		customization.WithContainerBuilder(
			snapshotGCContainerName,
			customization.WithOptionalEnvOverride(envPRSnapshotsToKeep, integrationSpec.PRSnapshotsToKeep),
			customization.WithOptionalEnvOverride(envNonPRSnapshotsToKeep, integrationSpec.NonPRSnapshotsToKeep),
			customization.WithOptionalEnvOverride(envMinSnapshotsToKeepPerComponent, integrationSpec.MinSnapshotsToKeepPerComponent),
		),
	)
	return envOverlay.ApplyToCronJob(cj)
}

// mapKonfluxUIToIntegrationService maps KonfluxUI events to KonfluxIntegrationService reconcile requests.
func (r *KonfluxIntegrationServiceReconciler) mapKonfluxUIToIntegrationService(_ context.Context, _ client.Object) []ctrl.Request {
	// Return reconcile request for the singleton KonfluxIntegrationService CR
	return []ctrl.Request{{NamespacedName: types.NamespacedName{Name: CRName}}}
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxIntegrationServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	crdMapFunc, err := crdhandler.MapCRDToRequest(r.ObjectStore, manifests.Integration, CRName)
	if err != nil {
		return err
	}
	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxIntegrationService{}).
		Named("konfluxintegrationservice").
		// Use predicates to filter out unnecessary updates and prevent reconcile loops
		// Deployments: watch spec changes AND readiness status changes
		Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.DeploymentReadinessPredicate)).
		Owns(&batchv1.CronJob{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Namespace{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&certmanagerv1.Certificate{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&certmanagerv1.Issuer{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&admissionregistrationv1.MutatingWebhookConfiguration{}).
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		// Watch CRDs so that out-of-band deletion triggers reconcile and re-apply.
		Watches(&apiextensionsv1.CustomResourceDefinition{},
			handler.EnqueueRequestsFromMapFunc(crdMapFunc)).
		// Watch KonfluxUI CR for ingress status changes to update console URL
		Watches(&konfluxv1alpha1.KonfluxUI{},
			handler.EnqueueRequestsFromMapFunc(r.mapKonfluxUIToIntegrationService),
			builder.WithPredicates(predicate.KonfluxUIIngressStatusChangedPredicate))

	if r.TokenCreator != nil {
		controllerBuilder = controllerBuilder.Owns(
			&corev1.Secret{},
			builder.WithPredicates(predicate.PrometheusScrapeTokenSecretPredicate),
		)
	}
	if r.TokenRotationEvents != nil && r.TokenCreator != nil {
		controllerBuilder = controllerBuilder.WatchesRawSource(source.Channel(
			r.TokenRotationEvents,
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, _ client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: CRName}}}
			}),
		))
	}

	return controllerBuilder.Complete(r)
}
