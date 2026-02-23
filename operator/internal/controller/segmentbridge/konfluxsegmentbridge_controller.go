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
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxSegmentBridge CR.
	CRName = "konflux-segment-bridge"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-segmentbridge-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxSegmentBridge"

	segmentBridgeNamespace = "segment-bridge"
	segmentBridgeSecretName = "segment-bridge-config"
	uiNamespace            = "konflux-info"
	uiSecretName           = "segment-bridge-key"
	uiConfigMapName        = "cluster-info"
	uiRoleName             = "segment-bridge-info-reader"
	uiRoleBindingName      = "segment-bridge-info-reader"
)

// SegmentBridgeCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. All resources managed by this controller are always
// applied, so no cleanup GVKs are needed (they're always tracked and never become orphans).
var SegmentBridgeCleanupGVKs = []schema.GroupVersionKind{}

// SegmentBridgeClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. All cluster-scoped resources managed by this controller are always
// applied, so no allow list is needed (they're always tracked and never become orphans).
var SegmentBridgeClusterScopedAllowList tracking.ClusterScopedAllowList = nil

// KonfluxSegmentBridgeReconciler reconciles a KonfluxSegmentBridge object
type KonfluxSegmentBridgeReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxsegmentbridges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxsegmentbridges/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxsegmentbridges/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=secrets;serviceaccounts,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;patch
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

	// Apply all embedded manifests (Namespace, SA, ClusterRole, ClusterRoleBinding, CronJob)
	if err := r.applyManifests(ctx, tc); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Create the Secret for the CronJob (contains SEGMENT_WRITE_KEY)
	if err := r.reconcileSegmentBridgeSecret(ctx, tc, segmentBridge); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSecretCreationFailed, "reconcile segment-bridge secret")
	}

	// Create the Secret for the UI (contains the segment key for frontend)
	if err := r.reconcileUISecret(ctx, tc, segmentBridge); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonSecretCreationFailed, "reconcile UI secret")
	}

	// Create the ConfigMap for the UI (contains telemetry-api-url)
	if err := r.reconcileUIConfigMap(ctx, tc, segmentBridge); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonConfigMapFailed, "reconcile UI ConfigMap")
	}

	// Create Role + RoleBinding in konflux-info NS for system:authenticated read access
	if err := r.reconcileUIRBAC(ctx, tc); err != nil {
		return errHandler.HandleWithReason(ctx, err, condition.ReasonApplyFailed, "reconcile UI RBAC")
	}

	// Cleanup orphaned resources
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, segmentBridge.Name, SegmentBridgeCleanupGVKs,
		tracking.WithClusterScopedAllowList(SegmentBridgeClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Update component status (sets Ready condition based on owned resources)
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

// reconcileSegmentBridgeSecret creates the Secret in the segment-bridge namespace
// that the CronJob reads via envFrom. Contains the Segment write key.
func (r *KonfluxSegmentBridgeReconciler) reconcileSegmentBridgeSecret(ctx context.Context, tc *tracking.Client, cr *konfluxv1alpha1.KonfluxSegmentBridge) error {
	log := logf.FromContext(ctx)

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      segmentBridgeSecretName,
			Namespace: segmentBridgeNamespace,
		},
		StringData: map[string]string{
			"SEGMENT_WRITE_KEY": cr.Spec.SegmentKey,
		},
	}

	log.Info("Applying segment-bridge Secret", "name", secret.Name, "namespace", secret.Namespace)
	if err := tc.ApplyOwned(ctx, secret); err != nil {
		return fmt.Errorf("failed to apply Secret: %w", err)
	}
	return nil
}

// reconcileUISecret creates the Secret in the konflux-info namespace
// that the frontend UI reads to get the Segment write key.
func (r *KonfluxSegmentBridgeReconciler) reconcileUISecret(ctx context.Context, tc *tracking.Client, cr *konfluxv1alpha1.KonfluxSegmentBridge) error {
	log := logf.FromContext(ctx)

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      uiSecretName,
			Namespace: uiNamespace,
		},
		StringData: map[string]string{
			"key": cr.Spec.SegmentKey,
		},
	}

	log.Info("Applying UI Secret", "name", secret.Name, "namespace", secret.Namespace)
	if err := tc.ApplyOwned(ctx, secret); err != nil {
		return fmt.Errorf("failed to apply Secret: %w", err)
	}
	return nil
}

// reconcileUIConfigMap creates the ConfigMap in the konflux-info namespace
// that the frontend UI reads to get the Segment API URL.
func (r *KonfluxSegmentBridgeReconciler) reconcileUIConfigMap(ctx context.Context, tc *tracking.Client, cr *konfluxv1alpha1.KonfluxSegmentBridge) error {
	log := logf.FromContext(ctx)

	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      uiConfigMapName,
			Namespace: uiNamespace,
		},
		Data: map[string]string{
			"telemetry-api-url": cr.Spec.SegmentAPIURL,
		},
	}

	log.Info("Applying UI ConfigMap", "name", configMap.Name, "namespace", configMap.Namespace)
	if err := tc.ApplyOwned(ctx, configMap); err != nil {
		return fmt.Errorf("failed to apply ConfigMap: %w", err)
	}
	return nil
}

// reconcileUIRBAC creates a Role and RoleBinding in the konflux-info namespace
// that allows system:authenticated users to read the segment-bridge-key Secret
// and the cluster-info ConfigMap.
func (r *KonfluxSegmentBridgeReconciler) reconcileUIRBAC(ctx context.Context, tc *tracking.Client) error {
	log := logf.FromContext(ctx)

	role := &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      uiRoleName,
			Namespace: uiNamespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{""},
				Resources:     []string{"secrets"},
				ResourceNames: []string{uiSecretName},
				Verbs:         []string{"get"},
			},
			{
				APIGroups:     []string{""},
				Resources:     []string{"configmaps"},
				ResourceNames: []string{uiConfigMapName},
				Verbs:         []string{"get"},
			},
		},
	}

	log.Info("Applying UI Role", "name", role.Name, "namespace", role.Namespace)
	if err := tc.ApplyOwned(ctx, role); err != nil {
		return fmt.Errorf("failed to apply Role: %w", err)
	}

	roleBinding := &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      uiRoleBindingName,
			Namespace: uiNamespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     uiRoleName,
		},
		Subjects: []rbacv1.Subject{
			{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Group",
				Name:     "system:authenticated",
			},
		},
	}

	log.Info("Applying UI RoleBinding", "name", roleBinding.Name, "namespace", roleBinding.Namespace)
	if err := tc.ApplyOwned(ctx, roleBinding); err != nil {
		return fmt.Errorf("failed to apply RoleBinding: %w", err)
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
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(predicate.LabelsOrAnnotationsChangedPredicate)).
		Owns(&batchv1.CronJob{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.Role{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.RoleBinding{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.ClusterRole{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Owns(&rbacv1.ClusterRoleBinding{}, builder.WithPredicates(predicate.GenerationChangedPredicate)).
		Complete(r)
}
