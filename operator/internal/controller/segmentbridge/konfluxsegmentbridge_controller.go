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

package segmentbridge

import (
	"context"
	"fmt"
	"net/url"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/predicate"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/segment"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxSegmentBridge CR.
	CRName = "konflux-segment-bridge"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-segmentbridge-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxSegmentBridge"

	segmentBridgeNamespace  = "segment-bridge"
	segmentBridgeSecretName = "segment-bridge-config"

	// tektonResultsAPIAddrK8s is the in-cluster gRPC address of the Tekton Results
	// API on vanilla Kubernetes (targetNamespace=tekton-pipelines).
	tektonResultsAPIAddrK8s = "tekton-results-api-service.tekton-pipelines.svc.cluster.local:8080"
	// tektonResultsAPIAddrOpenShift is the in-cluster gRPC address of the Tekton
	// Results API on OpenShift (targetNamespace=openshift-pipelines).
	tektonResultsAPIAddrOpenShift = "tekton-results-api-service.openshift-pipelines.svc.cluster.local:8080"
)

// SegmentBridgeCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. Only optional/conditional resources are listed here.
// All segment-bridge resources (including the Secret) are always applied, so no cleanup
// candidates exist.
var SegmentBridgeCleanupGVKs []schema.GroupVersionKind

// SegmentBridgeClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. All cluster-scoped resources managed by this controller are always
// applied, so no allow list is needed (they're always tracked and never become orphans).
var SegmentBridgeClusterScopedAllowList tracking.ClusterScopedAllowList = nil

// KonfluxSegmentBridgeReconciler reconciles a KonfluxSegmentBridge object
type KonfluxSegmentBridgeReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	ObjectStore          *manifests.ObjectStore
	ClusterInfo          *clusterinfo.Info
	GetDefaultSegmentKey func() string
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxsegmentbridges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxsegmentbridges/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxsegmentbridges/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=segment-bridge,verbs=bind;escalate
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=segment-bridge,verbs=bind

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *KonfluxSegmentBridgeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	segmentBridge := &konfluxv1alpha1.KonfluxSegmentBridge{}
	if err := r.Get(ctx, req.NamespacedName, segmentBridge); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxSegmentBridge", "name", segmentBridge.Name)

	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), segmentBridge, crKind)

	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             segmentBridge,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.SegmentBridge),
		FieldManager:      FieldManager,
	})

	if err := r.applyManifests(ctx, tc); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	if err := r.reconcileSegmentBridgeSecret(ctx, tc, &segmentBridge.Spec); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSecretCreationFailed, "reconcile segment-bridge secret")
	}

	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, segmentBridge.Name, SegmentBridgeCleanupGVKs,
		tracking.WithClusterScopedAllowList(SegmentBridgeClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	if err := condition.UpdateComponentStatuses(ctx, r.Client, segmentBridge); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	if err := r.Status().Update(ctx, segmentBridge); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxSegmentBridge")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
func (r *KonfluxSegmentBridgeReconciler) applyManifests(ctx context.Context, tc *tracking.Client) error {
	objects, err := r.ObjectStore.GetForComponent(manifests.SegmentBridge)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for SegmentBridge: %w", err)
	}

	for _, obj := range objects {
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.SegmentBridge, err)
		}
	}
	return nil
}

// tektonResultsAPIAddr returns the in-cluster gRPC address of the Tekton Results API.
// On OpenShift the Tekton namespace is openshift-pipelines; everywhere else it is
// tekton-pipelines. The same logic is used by the UI proxy (see proxy.yaml).
func (r *KonfluxSegmentBridgeReconciler) tektonResultsAPIAddr() string {
	if r.ClusterInfo != nil && r.ClusterInfo.IsOpenShift() {
		return tektonResultsAPIAddrOpenShift
	}
	return tektonResultsAPIAddrK8s
}

// reconcileSegmentBridgeSecret creates the Secret in the segment-bridge namespace that the
// CronJob reads via envFrom. Always contains TEKTON_RESULTS_API_ADDR (in-cluster gRPC
// endpoint), SEGMENT_WRITE_KEY, and SEGMENT_BATCH_API.
//
// Key resolution precedence:
//  1. CR inline spec.segmentKey (admin override)
//  2. Build-time default from GetDefaultSegmentKey (baked into binary via ldflags)
//  3. Empty -- Secret is still created so the CronJob can reach the Results API;
//     the segment-bridge scripts handle the missing key gracefully by skipping the
//     upload step.
func (r *KonfluxSegmentBridgeReconciler) reconcileSegmentBridgeSecret(ctx context.Context, tc *tracking.Client, spec *konfluxv1alpha1.KonfluxSegmentBridgeSpec) error {
	log := logf.FromContext(ctx)

	segmentKey, source := segment.ResolveWriteKey(spec.GetSegmentKey(), r.GetDefaultSegmentKey())
	segment.LogWriteKeyResolution(log, segmentKey, source)

	batchURL, err := url.JoinPath(spec.GetSegmentAPIURL(), "batch")
	if err != nil {
		return fmt.Errorf("invalid segment API URL: %w", err)
	}

	data := map[string]string{
		"TEKTON_RESULTS_API_ADDR": r.tektonResultsAPIAddr(),
		"SEGMENT_WRITE_KEY":       segmentKey,
		"SEGMENT_BATCH_API":       batchURL,
	}

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      segmentBridgeSecretName,
			Namespace: segmentBridgeNamespace,
		},
		StringData: data,
	}

	log.V(1).Info("Applying segment-bridge Secret", "name", secret.Name, "namespace", secret.Namespace, "hasWriteKey", segmentKey != "")
	if err := tc.ApplyOwned(ctx, secret); err != nil {
		return fmt.Errorf("failed to apply Secret: %w", err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxSegmentBridgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxSegmentBridge{}).
		Named("konfluxsegmentbridge").
		Owns(&corev1.Namespace{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&corev1.ServiceAccount{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&corev1.Secret{}, builder.WithPredicates(predicate.LabelsOrAnnotationsChangedPredicate)).
		Owns(&batchv1.CronJob{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.ClusterRole{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.ClusterRoleBinding{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Complete(r)
}
