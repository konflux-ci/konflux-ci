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

package namespacelister

import (
	"context"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	"github.com/konflux-ci/konflux-ci/operator/internal/predicate"
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxNamespaceLister CR.
	CRName = "konflux-namespace-lister"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-namespacelister-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxNamespaceLister"

	namespaceListerNamespace            = "namespace-lister"
	namespaceListerTLSSecretName          = "namespace-lister-tls"
	namespaceListerServiceMonitorName     = "namespace-lister"
	namespaceListerContainerName          = "namespace-lister"
	namespaceListerMetricsReaderRole      = "namespace-lister-metrics-reader"
	namespaceListerMetricsReaderBinding = "prometheus-namespace-lister-metrics-reader"

	envCacheResyncPeriod = "CACHE_RESYNC_PERIOD"
	envLogLevel          = "LOG_LEVEL"

	argEnableMetrics      = "-enable-metrics"
	argDisableMetrics     = "-enable-metrics=false"
	argMetricsAddress     = "-metrics-address=:9100"
)

// logLevelToEnvValue maps the CRD enum to the integer value the upstream namespace-lister expects.
var logLevelToEnvValue = map[konfluxv1alpha1.LogLevel]string{
	konfluxv1alpha1.LogLevelDebug: "-4",
	konfluxv1alpha1.LogLevelInfo:  "0",
	konfluxv1alpha1.LogLevelWarn:  "4",
	konfluxv1alpha1.LogLevelError: "8",
}

// resolveLogLevelEnvValue converts a LogLevel to the env var value.
func resolveLogLevelEnvValue(level konfluxv1alpha1.LogLevel) (string, error) {
	if level == "" {
		return "", nil
	}
	v, ok := logLevelToEnvValue[level]
	if !ok {
		return "", fmt.Errorf("unsupported logLevel %q", level)
	}
	return v, nil
}

// NamespaceListerCleanupGVKs defines resource types cleaned up when no longer desired.
var NamespaceListerCleanupGVKs = append([]schema.GroupVersionKind(nil), kubernetes.ComponentMetricsOrphanCleanupGVKs...)

// NamespaceListerClusterScopedAllowList restricts cluster-scoped orphan cleanup to metrics scrape RBAC.
var NamespaceListerClusterScopedAllowList = tracking.ClusterScopedAllowList{
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}: sets.New(
		namespaceListerMetricsReaderRole,
	),
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"}: sets.New(
		namespaceListerMetricsReaderBinding,
	),
}

// KonfluxNamespaceListerReconciler reconciles a KonfluxNamespaceLister object
type KonfluxNamespaceListerReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	ObjectStore         *manifests.ObjectStore
	TokenCreator        kubernetes.TokenCreator
	Clock               clock.Clock
	TokenRotationEvents <-chan event.TypedGenericEvent[client.Object]
	SecretReader        client.Reader
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxnamespacelisters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxnamespacelisters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxnamespacelisters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=core,resources=services;serviceaccounts,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts/token,verbs=create
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=namespace-lister-authorizer;namespace-lister-metrics-reader,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=namespace-lister-authorizer;namespace-lister-auth-delegator;prometheus-namespace-lister-metrics-reader,verbs=bind
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;patch;delete

func (r *KonfluxNamespaceListerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	konfluxNamespaceLister := &konfluxv1alpha1.KonfluxNamespaceLister{}
	if err := r.Get(ctx, req.NamespacedName, konfluxNamespaceLister); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxNamespaceLister", "name", konfluxNamespaceLister.Name)

	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), konfluxNamespaceLister, crKind)

	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             konfluxNamespaceLister,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.NamespaceLister),
		FieldManager:      FieldManager,
	})

	if err := r.applyManifests(ctx, tc, konfluxNamespaceLister); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	scrapeResult := reconcile.Result{}
	if konfluxNamespaceLister.Spec.ComponentMetrics.IsEnabled() && r.TokenCreator != nil {
		scraper := kubernetes.OperandMetricsScraperSA(namespaceListerNamespace)
		var scrapeErr error
		scrapeResult, scrapeErr = common.ReconcilePrometheusScrapeToken(ctx, common.ScrapeTokenReconcilerConfig{
			Client:               r.Client,
			SecretReader:         r.SecretReader,
			Clock:                r.Clock,
			TokenCreator:         r.TokenCreator,
			Scraper:              scraper,
			OperandNamespace:     namespaceListerNamespace,
			ServiceMonitorName:   namespaceListerServiceMonitorName,
			MetricsTLSSecretName: namespaceListerTLSSecretName,
			Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
				return tc.ApplyOwned(applyCtx, secret)
			},
			ApplyServiceMonitor: func(applyCtx context.Context) error {
				objects, storeErr := r.ObjectStore.GetForComponent(manifests.NamespaceLister)
				if storeErr != nil {
					return fmt.Errorf("get manifests for ServiceMonitor apply: %w", storeErr)
				}
				sm, ok := common.OperandServiceMonitorFromObjects(objects, namespaceListerNamespace, namespaceListerServiceMonitorName)
				if !ok {
					return fmt.Errorf("operand ServiceMonitor %s/%s not found in embedded manifests",
						namespaceListerNamespace, namespaceListerServiceMonitorName)
				}
				if err := common.ApplyMetricsScraperBindingSubjects(namespaceListerNamespace, sm); err != nil {
					return fmt.Errorf("apply metrics scraper binding subjects for ServiceMonitor: %w", err)
				}
				return tc.ApplyOwned(applyCtx, sm)
			},
		})
		if scrapeErr != nil {
			return errHandler.HandleWithReason(ctx, scrapeErr, condition.ReasonApplyFailed, "reconcile prometheus scrape token")
		}
	}

	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, konfluxNamespaceLister.Name, NamespaceListerCleanupGVKs,
		tracking.WithClusterScopedAllowList(NamespaceListerClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	if err := condition.UpdateComponentStatuses(ctx, r.Client, konfluxNamespaceLister); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	if err := r.Status().Update(ctx, konfluxNamespaceLister); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxNamespaceLister")
	return scrapeResult, nil
}

func (r *KonfluxNamespaceListerReconciler) applyManifests(ctx context.Context, tc *tracking.Client, owner *konfluxv1alpha1.KonfluxNamespaceLister) error {
	log := logf.FromContext(ctx)

	metricsEnabled := owner.Spec.ComponentMetrics.IsEnabled()
	deferServiceMonitor := metricsEnabled && r.TokenCreator != nil

	objects, err := r.ObjectStore.GetForComponent(manifests.NamespaceLister)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for NamespaceLister: %w", err)
	}

	for _, obj := range objects {
		if deferServiceMonitor && kubernetes.IsComponentMetricsServiceMonitor(obj) {
			log.V(2).Info("Deferring operand ServiceMonitor apply until scrape token and metrics TLS are ready",
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

		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if err := applyNamespaceListerCustomizations(deployment, owner.Spec); err != nil {
				return fmt.Errorf("failed to apply customizations to deployment %s: %w", deployment.Name, err)
			}
		}

		if err := common.ApplyMetricsScraperBindingSubjects(namespaceListerNamespace, obj); err != nil {
			return fmt.Errorf("apply metrics scraper binding subjects for %s: %w", obj.GetName(), err)
		}

		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.NamespaceLister, err)
		}
	}
	return nil
}

func applyNamespaceListerCustomizations(deployment *appsv1.Deployment, spec konfluxv1alpha1.KonfluxNamespaceListerSpec) error {
	var containerSpec *konfluxv1alpha1.ContainerSpec
	if spec.NamespaceLister != nil {
		if spec.NamespaceLister.Replicas > 0 {
			deployment.Spec.Replicas = &spec.NamespaceLister.Replicas
		}
		containerSpec = spec.NamespaceLister.NamespaceLister
	}

	logLevelValue, err := resolveLogLevelEnvValue(spec.LogLevel)
	if err != nil {
		return fmt.Errorf("invalid namespace-lister spec: %w", err)
	}

	containerOpts := []customization.ContainerOption{
		customization.FromContainerSpec(containerSpec),
		customization.WithOptionalEnvOverride(envCacheResyncPeriod, spec.CacheResyncPeriod),
		customization.WithOptionalEnvOverride(envLogLevel, logLevelValue),
	}

	podOpts := []customization.PodOverlayOption{
		customization.WithContainerOpts(namespaceListerContainerName, customization.DeploymentContext{}, containerOpts...),
	}

	if spec.ComponentMetrics.IsEnabled() {
		podOpts = append(podOpts,
			customization.WithArgReplace(namespaceListerContainerName, argEnableMetrics, argMetricsAddress),
		)
	} else {
		podOpts = append(podOpts,
			customization.WithArgReplace(namespaceListerContainerName, argDisableMetrics),
		)
	}

	return customization.NewPodOverlay(podOpts...).ApplyToDeployment(deployment)
}

func (r *KonfluxNamespaceListerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxNamespaceLister{}).
		Named("konfluxnamespacelister").
		Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.DeploymentReadinessPredicate)).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Namespace{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&certmanagerv1.Certificate{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate))

	if r.TokenCreator != nil {
		controllerBuilder = controllerBuilder.Owns(
			&corev1.Secret{},
			builder.WithPredicates(predicate.PrometheusScrapeTokenSecretPredicate),
		)
		if sm, ok := common.OperandServiceMonitorWatchObjectIfInstalled(mgr.GetRESTMapper()); ok {
			controllerBuilder = controllerBuilder.Owns(sm)
		}
		controllerBuilder = controllerBuilder.Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				if obj.GetNamespace() != namespaceListerNamespace {
					return nil
				}
				return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: CRName}}}
			}),
			builder.WithPredicates(predicate.MetricsTLSSecretNamedPredicate(namespaceListerTLSSecretName)),
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
