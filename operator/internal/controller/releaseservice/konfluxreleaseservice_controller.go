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

package releaseservice

import (
	"context"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	"github.com/konflux-ci/konflux-ci/operator/internal/predicate"
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxReleaseService CR.
	CRName = "konflux-release-service"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-releaseservice-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxReleaseService"

	// Deployment names
	releaseControllerManagerDeploymentName = "release-service-controller-manager"

	// Container names
	releaseManagerContainerName = "manager"

	// releaseServiceNamespace is the operand namespace for release-service resources.
	releaseServiceNamespace = "release-service"

	// ReleaseServiceConfig identification
	releaseServiceConfigKind  = "ReleaseServiceConfig"
	releaseServiceConfigGroup = "appstudio.redhat.com"
)

// releaseServiceConfigGVK is the GVK for ReleaseServiceConfig resources.
var releaseServiceConfigGVK = schema.GroupVersionKind{Group: releaseServiceConfigGroup, Version: "v1alpha1", Kind: releaseServiceConfigKind}

// ReleaseServiceCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. Metrics scrape resources may be skipped during apply
// (componentMetrics disabled) or removed across releases while metrics stay enabled.
var ReleaseServiceCleanupGVKs = append([]schema.GroupVersionKind(nil), kubernetes.ComponentMetricsOrphanCleanupGVKs...)

// ReleaseServiceClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. Only metrics scrape ClusterRoles and ClusterRoleBindings are listed;
// other cluster-scoped RBAC managed by this controller is always applied and stays tracked.
var ReleaseServiceClusterScopedAllowList = tracking.ClusterScopedAllowList{
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}: sets.New(
		"release-service-metrics-reader",
	),
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"}: sets.New(
		"prometheus-release-service-metrics-reader",
	),
}

// KonfluxReleaseServiceReconciler reconciles a KonfluxReleaseService object
type KonfluxReleaseServiceReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	ObjectStore  *manifests.ObjectStore
	TokenCreator kubernetes.TokenCreator
	// SecretReader loads metrics TLS Secrets; prefer mgr.GetAPIReader() to avoid stale cache.
	SecretReader        client.Reader
	Clock               clock.Clock
	TokenRotationEvents <-chan event.TypedGenericEvent[client.Object]
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxreleaseservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxreleaseservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxreleaseservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=services;secrets;serviceaccounts,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts/token,verbs=create
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,resourceNames=release-service-leader-election-role,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,resourceNames=release-service-leader-election-rolebinding;releaseserviceconfigs-rolebinding,verbs=bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=release-pipeline-resource-role;release-service-application-role;release-service-component-role;release-service-environment-viewer-role;release-service-manager-role;release-service-metrics-auth-role;release-service-metrics-reader;release-service-release-editor-role;release-service-release-viewer-role;release-service-releaseplan-editor-role;release-service-releaseplan-viewer-role;release-service-releaseplanadmission-editor-role;release-service-releaseplanadmission-viewer-role;release-service-snapshot-editor-role;release-service-snapshot-viewer-role;release-service-snapshotenvironmentbinding-editor-role;release-service-tekton-role;releaseserviceconfig-role,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=prometheus-release-service-metrics-reader;release-service-application-role-binding;release-service-component-role-binding;release-service-environment-role-binding;release-service-manager-rolebinding;release-service-metrics-auth-rolebinding;release-service-release-role-binding;release-service-releaseplan-role-binding;release-service-releaseplanadmission-role-binding;release-service-snapshot-role-binding;release-service-snapshotenvironmentbinding-role-binding;release-service-tekton-role-binding,verbs=bind
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates;issuers,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=appstudio.redhat.com,resources=releaseserviceconfigs,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;watch;create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *KonfluxReleaseServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxReleaseService instance
	releaseService := &konfluxv1alpha1.KonfluxReleaseService{}
	if err := r.Get(ctx, req.NamespacedName, releaseService); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxReleaseService", "name", releaseService.Name)

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), releaseService, crKind)

	// Create a tracking client with ownership config for this reconcile.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             releaseService,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.Release),
		FieldManager:      FieldManager,
	})

	// Apply all embedded manifests
	if err := r.applyManifests(ctx, tc, releaseService); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	scrapeResult := reconcile.Result{}
	if releaseService.Spec.ComponentMetrics.IsEnabled() && r.TokenCreator != nil {
		// Deferred ServiceMonitor apply: mint scrape token, wait for metrics TLS, apply SM.
		scraper := kubernetes.OperandMetricsScraperSA(releaseServiceNamespace)
		var scrapeErr error
		scrapeResult, scrapeErr = common.ReconcilePrometheusScrapeToken(ctx, common.ScrapeTokenReconcilerConfig{
			Client:             r.Client,
			SecretReader:       r.SecretReader,
			Clock:              r.Clock,
			TokenCreator:       r.TokenCreator,
			Scraper:            scraper,
			OperandNamespace:   releaseServiceNamespace,
			ServiceMonitorName: "release-service",
			Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
				return tc.ApplyOwned(applyCtx, secret)
			},
			ApplyServiceMonitor: func(applyCtx context.Context) error {
				objects, storeErr := r.ObjectStore.GetForComponent(manifests.Release)
				if storeErr != nil {
					return fmt.Errorf("get manifests for ServiceMonitor apply: %w", storeErr)
				}
				sm, ok := common.OperandServiceMonitorFromObjects(objects, releaseServiceNamespace, "release-service")
				if !ok {
					return fmt.Errorf("operand ServiceMonitor %s/%s not found in embedded manifests",
						releaseServiceNamespace, "release-service")
				}
				if err := common.ApplyMetricsScraperBindingSubjects(releaseServiceNamespace, sm); err != nil {
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
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, releaseService.Name, ReleaseServiceCleanupGVKs,
		tracking.WithClusterScopedAllowList(ReleaseServiceClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Check the status of owned deployments and update KonfluxReleaseService status
	if err := condition.UpdateComponentStatuses(ctx, r.Client, releaseService); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update status
	if err := r.Status().Update(ctx, releaseService); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxReleaseService")
	return scrapeResult, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxReleaseServiceReconciler) applyManifests(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.KonfluxReleaseService) error {
	log := logf.FromContext(ctx)

	metricsEnabled := owner.Spec.ComponentMetrics.IsEnabled()
	deferServiceMonitor := metricsEnabled && r.TokenCreator != nil

	objects, err := r.ObjectStore.GetForComponent(manifests.Release)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for Release: %w", err)
	}

	for _, obj := range objects {
		// Deferred ServiceMonitor apply: skip operand SM until ReconcilePrometheusScrapeToken
		// applies it after prometheus-scrape-token and metrics TLS are ready.
		if deferServiceMonitor && kubernetes.IsComponentMetricsServiceMonitor(obj) {
			log.V(1).Info("Deferring operand ServiceMonitor apply until scrape token and metrics TLS are ready",
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
			if err := applyReleaseServiceDeploymentCustomizations(deployment, owner.Spec); err != nil {
				return fmt.Errorf("failed to apply customizations to deployment %s: %w", deployment.Name, err)
			}
		}

		// Apply customizations for ReleaseServiceConfig
		if isReleaseServiceConfig(obj) {
			if err := applyReleaseServiceConfigCustomizations(obj, owner.Spec); err != nil {
				return fmt.Errorf("failed to apply customizations to ReleaseServiceConfig %s: %w", obj.GetName(), err)
			}
		}

		if err := common.ApplyMetricsScraperBindingSubjects(releaseServiceNamespace, obj); err != nil {
			return fmt.Errorf("apply metrics scraper binding subjects for %s: %w", obj.GetName(), err)
		}

		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.Release, err)
		}
	}
	return nil
}

// applyReleaseServiceDeploymentCustomizations applies user-defined customizations to ReleaseService deployments.
func applyReleaseServiceDeploymentCustomizations(deployment *appsv1.Deployment, spec konfluxv1alpha1.KonfluxReleaseServiceSpec) error {
	switch deployment.Name {
	case releaseControllerManagerDeploymentName:
		if spec.ReleaseControllerManager != nil {
			deployment.Spec.Replicas = &spec.ReleaseControllerManager.Replicas
		}
		if err := buildReleaseControllerManagerOverlay(spec.ReleaseControllerManager).ApplyToDeployment(deployment); err != nil {
			return err
		}
	}
	return nil
}

// buildReleaseControllerManagerOverlay builds the pod overlay for the controller-manager deployment.
func buildReleaseControllerManagerOverlay(spec *konfluxv1alpha1.ControllerManagerDeploymentSpec) *customization.PodOverlay {
	if spec == nil {
		return customization.NewPodOverlay()
	}

	deployCtx := customization.DeploymentContext{Replicas: spec.Replicas}
	return customization.NewPodOverlay(
		customization.WithContainerOpts(releaseManagerContainerName, deployCtx,
			customization.FromContainerSpec(spec.Manager),
		),
		customization.WithLeaderElection(releaseManagerContainerName, spec.Replicas),
	)
}

// isReleaseServiceConfig returns true if the object is a ReleaseServiceConfig CR.
func isReleaseServiceConfig(obj client.Object) bool {
	gvk := obj.GetObjectKind().GroupVersionKind()
	return gvk.Kind == releaseServiceConfigKind && gvk.Group == releaseServiceConfigGroup
}

// applyReleaseServiceConfigCustomizations applies user-defined customizations to the
// ReleaseServiceConfig CR. It modifies the unstructured object's spec based on
// the KonfluxReleaseServiceSpec fields (debug, emptyDirOverrides).
func applyReleaseServiceConfigCustomizations(obj client.Object, spec konfluxv1alpha1.KonfluxReleaseServiceSpec) error {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("expected *unstructured.Unstructured, got %T", obj)
	}

	currentSpec, _ := u.Object["spec"].(map[string]interface{})
	if currentSpec == nil {
		currentSpec = map[string]interface{}{}
	}

	currentSpec["debug"] = spec.Debug

	if len(spec.EmptyDirOverrides) > 0 {
		overrides := make([]interface{}, len(spec.EmptyDirOverrides))
		for i, o := range spec.EmptyDirOverrides {
			overrides[i] = map[string]interface{}{
				"url":        o.URL,
				"revision":   o.Revision,
				"pathInRepo": o.PathInRepo,
			}
		}
		currentSpec["EmptyDirOverrides"] = overrides
	} else {
		delete(currentSpec, "EmptyDirOverrides")
	}

	u.Object["spec"] = currentSpec
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxReleaseServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	crdMapFunc, err := crdhandler.MapCRDToRequest(r.ObjectStore, manifests.Release, CRName)
	if err != nil {
		return err
	}
	rsc := &unstructured.Unstructured{}
	rsc.SetGroupVersionKind(releaseServiceConfigGVK)

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxReleaseService{}).
		Named("konfluxreleaseservice").
		// Use predicates to filter out unnecessary updates and prevent reconcile loops
		Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.DeploymentReadinessPredicate)).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Namespace{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&certmanagerv1.Certificate{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&certmanagerv1.Issuer{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&admissionregistrationv1.MutatingWebhookConfiguration{}).
		Owns(&admissionregistrationv1.ValidatingWebhookConfiguration{}).
		Owns(rsc, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Watches(&apiextensionsv1.CustomResourceDefinition{},
			handler.EnqueueRequestsFromMapFunc(crdMapFunc))

	if r.TokenCreator != nil {
		controllerBuilder = controllerBuilder.Owns(
			&corev1.Secret{},
			builder.WithPredicates(predicate.PrometheusScrapeTokenSecretPredicate),
		)
		// metrics-server-cert is created by cert-manager (not CR ownerRefs).
		controllerBuilder = controllerBuilder.Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				if obj.GetNamespace() != releaseServiceNamespace {
					return nil
				}
				return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: CRName}}}
			}),
			builder.WithPredicates(predicate.MetricsTLSSecretPredicate),
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
