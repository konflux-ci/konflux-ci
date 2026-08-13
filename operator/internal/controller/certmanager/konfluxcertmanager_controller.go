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

package certmanager

import (
	"context"
	"fmt"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
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
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxCertManager CR.
	CRName = "konflux-cert-manager"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-cert-manager-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxCertManager"
)

var (
	clusterIssuerGVK = certmanagerv1.SchemeGroupVersion.WithKind("ClusterIssuer")
	certificateGVK   = certmanagerv1.SchemeGroupVersion.WithKind("Certificate")
	bundleGVK        = schema.GroupVersionKind{Group: "trust.cert-manager.io", Version: "v1alpha1", Kind: "Bundle"}
)

// CertManagerCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. When createClusterIssuer is false, cert-manager
// resources will be automatically deleted because they weren't applied during the reconcile
// but have the owner label. The trust-manager Bundle is also cleaned up when
// distributeClusterCABundle is disabled.
var CertManagerCleanupGVKs = []schema.GroupVersionKind{
	clusterIssuerGVK,
	certificateGVK,
	bundleGVK,
}

// CertManagerClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. This is a security measure to prevent attackers from
// triggering deletion of arbitrary cluster resources by adding the owner label.
// Only conditionally-created resources need to be listed here.
// Resources that are always applied don't need protection (they're always tracked).
var CertManagerClusterScopedAllowList = tracking.ClusterScopedAllowList{
	// ClusterIssuers are only created when spec.createClusterIssuer is true
	clusterIssuerGVK: sets.New(
		"konflux-bootstrap-issuer",
		"konflux-issuer",
		// Legacy names (pre-rename) kept so orphan cleanup deletes them on
		// upgrade from releases that used the old names.
		// TODO(pki-renames): remove after v1.5 ships.
		"self-signed-cluster-issuer",
		"ca-issuer",
	),
	// Bundle is only created when distributeClusterCABundle is effectively true
	bundleGVK: sets.New(
		"trusted-ca",
	),
}

// KonfluxCertManagerReconciler reconciles a KonfluxCertManager object
type KonfluxCertManagerReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
	ClusterInfo *clusterinfo.Info
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxcertmanagers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxcertmanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxcertmanagers/finalizers,verbs=update
// +kubebuilder:rbac:groups=cert-manager.io,resources=clusterissuers,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=trust.cert-manager.io,resources=bundles,verbs=get;list;watch;create;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *KonfluxCertManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxCertManager instance
	certManager := &konfluxv1alpha1.KonfluxCertManager{}
	if err := r.Get(ctx, req.NamespacedName, certManager); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxCertManager", "name", certManager.Name)

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), certManager, crKind)

	// Create a tracking client for this reconcile with ownership config.
	// Resources applied through this client are automatically tracked and have ownership set.
	// At the end of a successful reconcile, orphaned resources are cleaned up.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             certManager,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.CertManager),
		FieldManager:      FieldManager,
	})

	isOpenShift := r.ClusterInfo.IsOpenShift()

	// Apply PKI manifests only if createClusterIssuer is enabled (defaults to true).
	// The cert-manager namespace must already exist (created by whoever installs cert-manager);
	// if it does not, applyManifests will fail and the error is reported via the status.
	if certManager.Spec.ShouldCreateClusterIssuer() {
		if err := r.applyPKIManifests(ctx, tc); err != nil {
			return errHandler.HandleApplyError(ctx, err)
		}
	} else {
		log.Info("Skipping PKI manifest application - createClusterIssuer is false")
	}

	// Apply trust-manager Bundle if distributeClusterCABundle is effectively enabled.
	// Platform-aware default: true on non-OpenShift (Kind/upstream), false on OpenShift.
	var requeueForTrustManager bool
	if certManager.Spec.ShouldDistributeClusterCABundle(isOpenShift) {
		var err error
		requeueForTrustManager, err = r.applyTrustBundle(ctx, tc)
		if err != nil {
			return errHandler.HandleApplyError(ctx, err)
		}
	} else {
		log.Info("Skipping trust-manager Bundle - distributeClusterCABundle is disabled",
			"isOpenShift", isOpenShift,
			"explicitValue", certManager.Spec.DistributeClusterCABundle)
	}

	// Cleanup orphaned resources - delete any resources with our owner label
	// that weren't applied during this reconcile. This handles the case where
	// createClusterIssuer changes from true to false (resources are automatically deleted).
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, certManager.Name, CertManagerCleanupGVKs,
		tracking.WithClusterScopedAllowList(CertManagerClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Check the status of owned deployments and update KonfluxCertManager status
	// Note: cert-manager has no deployments, so this will set Ready=true with appropriate message
	if err := condition.UpdateComponentStatuses(ctx, r.Client, certManager); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update status
	if err := r.Status().Update(ctx, certManager); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxCertManager")

	// If trust-manager CRD was not installed but distribution is enabled,
	// requeue so the Bundle is applied once trust-manager becomes available.
	if requeueForTrustManager {
		log.Info("Requeuing to retry trust-manager Bundle application when CRD becomes available")
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

// applyPKIManifests loads and applies PKI manifests (ClusterIssuers, Certificates) to the
// cluster using the tracking client. Trust-manager Bundle resources are skipped here —
// they are handled separately by applyTrustBundle based on the distributeClusterCABundle field.
func (r *KonfluxCertManagerReconciler) applyPKIManifests(ctx context.Context, tc *tracking.Client) error {
	objects, err := r.ObjectStore.GetForComponent(manifests.CertManager)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for CertManager: %w", err)
	}

	for _, obj := range objects {
		// Skip Bundle resources — they are applied by applyTrustBundle
		if obj.GetObjectKind().GroupVersionKind() == bundleGVK {
			continue
		}
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.CertManager, err)
		}
	}
	return nil
}

// applyTrustBundle applies only the trust-manager Bundle resources from the cert-manager
// component manifests. If the trust-manager CRD is not installed, the error is logged
// and skipped — the caller should requeue to retry once trust-manager becomes available.
// Returns true if any Bundle was skipped due to a missing CRD.
func (r *KonfluxCertManagerReconciler) applyTrustBundle(ctx context.Context, tc *tracking.Client) (bool, error) {
	log := logf.FromContext(ctx)

	objects, err := r.ObjectStore.GetForComponent(manifests.CertManager)
	if err != nil {
		return false, fmt.Errorf("failed to get parsed manifests for CertManager: %w", err)
	}

	crdMissing := false
	for _, obj := range objects {
		if obj.GetObjectKind().GroupVersionKind() != bundleGVK {
			continue
		}
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			// Skip if the trust-manager CRD is not installed.
			// The reconciler will requeue to retry once trust-manager becomes available.
			if tracking.IsNoKindMatchError(err) {
				log.Info("Skipping trust-manager Bundle: CRD not installed — will requeue",
					"name", obj.GetName(),
					"apiVersion", bundleGVK.GroupVersion().String(),
				)
				crdMissing = true
				continue
			}
			return false, fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.CertManager, err)
		}
	}
	return crdMissing, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxCertManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxCertManager{}).
		Named("konfluxcertmanager").
		Owns(&certmanagerv1.Certificate{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&certmanagerv1.ClusterIssuer{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Complete(r)
}
