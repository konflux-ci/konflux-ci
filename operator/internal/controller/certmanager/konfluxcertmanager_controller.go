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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	// trustManagerGroup is the API group for trust-manager resources.
	trustManagerGroup = "trust.cert-manager.io"
	// trustedCABundleName is the cluster-scoped trust-manager Bundle that
	// populates trusted-ca ConfigMaps.
	trustedCABundleName = "trusted-ca"
	// trustManagerCRDRetryInterval is used when distributeClusterCABundle is
	// enabled but the Bundle CRD is not installed yet (Kind bootstrap race).
	trustManagerCRDRetryInterval = 30 * time.Second
)

var (
	clusterIssuerGVK = certmanagerv1.SchemeGroupVersion.WithKind("ClusterIssuer")
	certificateGVK   = certmanagerv1.SchemeGroupVersion.WithKind("Certificate")
	bundleGVK        = schema.GroupVersionKind{Group: trustManagerGroup, Version: "v1alpha1", Kind: "Bundle"}
)

// CertManagerCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. When createClusterIssuer is toggled off, cert-manager
// resources are automatically deleted. When distributeClusterCABundle is toggled off (or
// defaults to off on OpenShift), the trust-manager Bundle is deleted.
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
	// Bundle is only created when distributeClusterCABundle is true (or defaulted true on non-OpenShift)
	bundleGVK: sets.New(trustedCABundleName),
}

// KonfluxCertManagerReconciler reconciles a KonfluxCertManager object
type KonfluxCertManagerReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
	// ClusterInfo detects the cluster type. Nil means "assume non-OpenShift"
	// (vanilla Kubernetes), which causes distributeClusterCABundle to default
	// to true. Tests pass nil to simulate non-OpenShift environments.
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

	isOpenShift := r.ClusterInfo != nil && r.ClusterInfo.IsOpenShift()
	distributeBundle := certManager.Spec.ShouldDistributeClusterCABundle(isOpenShift)
	log.Info("Reconciling KonfluxCertManager",
		"name", certManager.Name,
		"isOpenShift", isOpenShift,
		"distributeClusterCABundle", distributeBundle,
	)

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

	// Apply PKI manifests (Certificate + ClusterIssuers) when createClusterIssuer is enabled.
	// The cert-manager namespace must already exist (created by whoever installs cert-manager);
	// if it does not, applyPKIManifests will fail and the error is reported via the status.
	if certManager.Spec.ShouldCreateClusterIssuer() {
		if err := r.applyPKIManifests(ctx, tc); err != nil {
			return errHandler.HandleApplyError(ctx, err)
		}
	} else {
		log.Info("Skipping PKI manifest application - createClusterIssuer is false")
	}

	// Apply the trust-manager Bundle when distributeClusterCABundle is effective.
	// The Bundle populates cluster-wide trusted-ca ConfigMaps consumed by Tekton.
	bundleCondition, requeueAfter, err := r.applyTrustBundle(ctx, tc, distributeBundle)
	if err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Cleanup orphaned resources - delete any resources with our owner label
	// that weren't applied during this reconcile. This handles toggling
	// createClusterIssuer or distributeClusterCABundle off.
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, certManager.Name, CertManagerCleanupGVKs,
		tracking.WithClusterScopedAllowList(CertManagerClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Update deployment-based conditions and set the ClusterCABundleDistributed condition.
	// WithExtraConditions ensures the custom condition is preserved across reconcile loops.
	if err := condition.UpdateComponentStatuses(ctx, r.Client, certManager,
		condition.WithExtraConditions(bundleCondition)); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update status
	if err := r.Status().Update(ctx, certManager); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxCertManager")
	if requeueAfter > 0 {
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}
	return ctrl.Result{}, nil
}

// isTrustManagerResource returns true for resources in the trust.cert-manager.io API group.
func isTrustManagerResource(obj client.Object) bool {
	return obj.GetObjectKind().GroupVersionKind().Group == trustManagerGroup
}

// applyPKIManifests applies PKI resources (Certificate, ClusterIssuers) from the embedded
// manifests, skipping any trust-manager resources.
func (r *KonfluxCertManagerReconciler) applyPKIManifests(ctx context.Context, tc *tracking.Client) error {
	objects, err := r.ObjectStore.GetForComponent(manifests.CertManager)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for CertManager: %w", err)
	}

	for _, obj := range objects {
		if isTrustManagerResource(obj) {
			continue
		}
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.CertManager, err)
		}
	}
	return nil
}

// applyTrustBundle applies the trust-manager Bundle resources from the embedded manifests.
// It returns a condition reflecting the outcome, an optional requeue delay, and an error
// for unexpected apply failures (those should abort reconcile before orphan cleanup):
//   - Disabled: distributeBundle is false; leftover trusted-ca is deleted if present
//   - Distributed: Bundle applied successfully
//   - NotDistributed: trust-manager CRD is not installed (requeue to retry)
func (r *KonfluxCertManagerReconciler) applyTrustBundle(ctx context.Context, tc *tracking.Client, distributeBundle bool) (metav1.Condition, time.Duration, error) {
	log := logf.FromContext(ctx)

	if !distributeBundle {
		if err := deleteTrustedCABundleIfPresent(ctx, tc); err != nil {
			return metav1.Condition{}, 0, err
		}
		log.Info("Skipping trust-manager Bundle - distributeClusterCABundle is disabled")
		return metav1.Condition{
			Type:    constant.ConditionTypeClusterCABundleDistributed,
			Status:  metav1.ConditionFalse,
			Reason:  condition.ReasonBundleDistributionDisabled,
			Message: "Trust-manager Bundle distribution is disabled by configuration",
		}, 0, nil
	}

	objects, err := r.ObjectStore.GetForComponent(manifests.CertManager)
	if err != nil {
		return metav1.Condition{}, 0, fmt.Errorf("failed to get parsed manifests for CertManager: %w", err)
	}

	applied := false
	for _, obj := range objects {
		if !isTrustManagerResource(obj) {
			continue
		}
		// SSA cannot replace another controller's ownerRef (merge key is uid),
		// so drop a leftover InternalRegistry controller ref before ApplyOwned.
		if err := releaseForeignBundleController(ctx, tc, obj); err != nil {
			if tracking.IsNoKindMatchError(err) {
				return bundleCRDMissingResult(ctx, obj.GetName())
			}
			return metav1.Condition{}, 0, err
		}
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			if tracking.IsNoKindMatchError(err) {
				return bundleCRDMissingResult(ctx, obj.GetName())
			}
			return metav1.Condition{}, 0, fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.CertManager, err)
		}
		applied = true
	}

	if !applied {
		log.Info("No trust-manager resources found in embedded manifests")
		return metav1.Condition{
			Type:    constant.ConditionTypeClusterCABundleDistributed,
			Status:  metav1.ConditionFalse,
			Reason:  condition.ReasonBundleNotDistributed,
			Message: "No trust-manager Bundle found in embedded manifests",
		}, 0, nil
	}

	return metav1.Condition{
		Type:    constant.ConditionTypeClusterCABundleDistributed,
		Status:  metav1.ConditionTrue,
		Reason:  condition.ReasonBundleDistributed,
		Message: "Trust-manager Bundle applied successfully",
	}, 0, nil
}

// deleteTrustedCABundleIfPresent removes the well-known trusted-ca Bundle when
// distribution is off. Ignores NotFound and a missing trust-manager CRD so
// OpenShift (no trust-manager) and clusters with no leftover Bundle stay quiet.
// Other errors fail reconcile. Deletes regardless of owner so a leftover
// InternalRegistry-owned Bundle is not left behind.
func deleteTrustedCABundleIfPresent(ctx context.Context, c client.Client) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(bundleGVK)
	obj.SetName(trustedCABundleName)
	if err := c.Delete(ctx, obj); err != nil {
		if client.IgnoreNotFound(err) == nil || tracking.IsNoKindMatchError(err) {
			return nil
		}
		return fmt.Errorf("failed to delete trust-manager Bundle %s: %w", trustedCABundleName, err)
	}
	logf.FromContext(ctx).Info("Deleted trust-manager Bundle; distribution is disabled",
		"name", trustedCABundleName)
	return nil
}

func bundleCRDMissingResult(ctx context.Context, name string) (metav1.Condition, time.Duration, error) {
	logf.FromContext(ctx).Info("trust-manager CRD not installed, Bundle cannot be applied - will retry",
		"name", name)
	return metav1.Condition{
		Type:    constant.ConditionTypeClusterCABundleDistributed,
		Status:  metav1.ConditionFalse,
		Reason:  condition.ReasonBundleNotDistributed,
		Message: "trust-manager CRD (Bundle) is not installed; install trust-manager to enable CA bundle distribution",
	}, trustManagerCRDRetryInterval, nil
}

// releaseForeignBundleController drops controller ownerRefs that are not
// KonfluxCertManager. Needed on upgrade from the InternalRegistry-owned Bundle:
// ApplyOwned SSA-merges ownerReferences by uid and would otherwise create two
// controller:true refs, which the API rejects.
func releaseForeignBundleController(ctx context.Context, c client.Client, obj client.Object) error {
	live := obj.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, client.ObjectKeyFromObject(obj), live); err != nil {
		return client.IgnoreNotFound(err)
	}
	kept, stripped := stripForeignControllerOwnerRefs(live.GetOwnerReferences())
	if len(stripped) == 0 {
		return nil
	}
	original := live.DeepCopyObject().(client.Object)
	live.SetOwnerReferences(kept)
	if err := c.Patch(ctx, live, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("failed to release foreign controller ownerRef on %s/%s (%s): %w",
			obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), err)
	}
	logf.FromContext(ctx).Info("Released foreign controller ownerRef on trust-manager Bundle",
		"name", obj.GetName(),
		"releasedKind", stripped[0].Kind,
		"releasedName", stripped[0].Name,
	)
	return nil
}

// stripForeignControllerOwnerRefs removes controller ownerRefs that do not
// point at this KonfluxCertManager. Non-controller refs are kept.
func stripForeignControllerOwnerRefs(refs []metav1.OwnerReference) (kept, stripped []metav1.OwnerReference) {
	for _, ref := range refs {
		if ref.Controller != nil && *ref.Controller && (ref.Kind != crKind || ref.Name != CRName) {
			stripped = append(stripped, ref)
			continue
		}
		kept = append(kept, ref)
	}
	return kept, stripped
}

// trustManagerBundleWatchObjectIfInstalled returns an unstructured Bundle watch
// object when the trust-manager CRD is discoverable. Skip Owns() when the CRD
// is absent so the controller can start on clusters that do not install
// trust-manager (the OpenShift default).
func trustManagerBundleWatchObjectIfInstalled(mapper meta.RESTMapper) (*unstructured.Unstructured, bool) {
	if mapper == nil {
		return nil, false
	}
	if _, err := mapper.RESTMapping(bundleGVK.GroupKind(), bundleGVK.Version); err != nil {
		if !meta.IsNoMatchError(err) {
			logf.Log.Error(err, "failed to resolve trust-manager Bundle REST mapping; skipping watch registration")
		}
		return nil, false
	}
	bundle := &unstructured.Unstructured{}
	bundle.SetGroupVersionKind(bundleGVK)
	return bundle, true
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxCertManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxCertManager{}).
		Named("konfluxcertmanager").
		Owns(&certmanagerv1.Certificate{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&certmanagerv1.ClusterIssuer{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate))

	// trust-manager Bundle uses Unstructured because importing the trust-manager
	// Go module would pull incompatible controller-runtime/k8s.io versions.
	// Gate the watch on CRD presence so OpenShift (no trust-manager) can start.
	if bundle, ok := trustManagerBundleWatchObjectIfInstalled(mgr.GetRESTMapper()); ok {
		b = b.Owns(bundle, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate))
	}

	return b.Complete(r)
}
