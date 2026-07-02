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

package enterprisecontract

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	crdhandler "github.com/konflux-ci/konflux-ci/operator/internal/controller/handler"
	"github.com/konflux-ci/konflux-ci/operator/internal/predicate"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// CRName is the singleton name for the KonfluxEnterpriseContract CR.
	CRName = "konflux-enterprise-contract"
	// FieldManager is the field manager identifier for server-side apply.
	FieldManager = "konflux-enterprisecontract-controller"
	// crKind is used in error messages to identify this CR type.
	crKind = "KonfluxEnterpriseContract"
)

// ecPolicyGVK is the GVK for EnterpriseContractPolicy resources.
var ecPolicyGVK = schema.GroupVersionKind{Group: "appstudio.redhat.com", Version: "v1alpha1", Kind: "EnterpriseContractPolicy"}

// EnterpriseContractCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. EnterpriseContractPolicy resources may be skipped
// when spec.skipPolicies is true, so they must be listed here for orphan cleanup.
var EnterpriseContractCleanupGVKs = []schema.GroupVersionKind{ecPolicyGVK}

// EnterpriseContractClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. All cluster-scoped resources managed by this controller are always
// applied, so no allow list is needed (they're always tracked and never become orphans).
var EnterpriseContractClusterScopedAllowList tracking.ClusterScopedAllowList = nil

// KonfluxEnterpriseContractReconciler reconciles a KonfluxEnterpriseContract object
type KonfluxEnterpriseContractReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxenterprisecontracts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxenterprisecontracts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxenterprisecontracts/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=list
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings;clusterroles,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,resourceNames=public-ec-cm;public-ecp,verbs=bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=enterprisecontract-configmap-viewer-role;enterprisecontractpolicy-viewer-role,verbs=bind;escalate
// +kubebuilder:rbac:groups=appstudio.redhat.com,resources=enterprisecontractpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=console.openshift.io,resources=consoleyamlsamples,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *KonfluxEnterpriseContractReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the KonfluxEnterpriseContract instance
	konfluxEnterpriseContract := &konfluxv1alpha1.KonfluxEnterpriseContract{}
	if err := r.Get(ctx, req.NamespacedName, konfluxEnterpriseContract); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling KonfluxEnterpriseContract", "name", konfluxEnterpriseContract.Name)

	// Create error handler for consistent error reporting
	errHandler := condition.NewReconcileErrorHandler(log, r.Status(), konfluxEnterpriseContract, crKind)

	// Create a tracking client with ownership config for this reconcile.
	tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
		Owner:             konfluxEnterpriseContract,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.EnterpriseContract),
		FieldManager:      FieldManager,
	})

	// Apply embedded manifests (policies are skipped when spec.skipPolicies is true)
	if err := r.applyManifests(ctx, tc, konfluxEnterpriseContract.Spec); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Cleanup orphaned resources
	if err := tc.CleanupOrphans(ctx, constant.KonfluxOwnerLabel, konfluxEnterpriseContract.Name, EnterpriseContractCleanupGVKs,
		tracking.WithClusterScopedAllowList(EnterpriseContractClusterScopedAllowList)); err != nil {
		return errHandler.HandleCleanupError(ctx, err)
	}

	// Check the status of owned deployments and update KonfluxEnterpriseContract status
	if err := condition.UpdateComponentStatuses(ctx, r.Client, konfluxEnterpriseContract); err != nil {
		return errHandler.HandleStatusUpdateError(ctx, err)
	}

	// Update status
	if err := r.Status().Update(ctx, konfluxEnterpriseContract); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled KonfluxEnterpriseContract")
	return ctrl.Result{}, nil
}

// applyManifests loads and applies all embedded manifests to the cluster using the tracking client.
// When spec.skipPolicies is true, EnterpriseContractPolicy resources are not applied.
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxEnterpriseContractReconciler) applyManifests(ctx context.Context, tc *tracking.Client, spec konfluxv1alpha1.KonfluxEnterpriseContractSpec) error {
	log := logf.FromContext(ctx)

	objects, err := r.ObjectStore.GetForComponent(manifests.EnterpriseContract)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for EnterpriseContract: %w", err)
	}

	for _, obj := range objects {
		if spec.SkipPolicies && obj.GetObjectKind().GroupVersionKind() == ecPolicyGVK {
			log.Info("Skipping EnterpriseContractPolicy (spec.skipPolicies is true)",
				"name", obj.GetName(),
				"namespace", obj.GetNamespace(),
			)
			continue
		}

		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.EnterpriseContract, err)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxEnterpriseContractReconciler) SetupWithManager(mgr ctrl.Manager) error {
	crdMapFunc, err := crdhandler.MapCRDToRequest(r.ObjectStore, manifests.EnterpriseContract, CRName)
	if err != nil {
		return err
	}
	ecPolicy := &unstructured.Unstructured{}
	ecPolicy.SetGroupVersionKind(ecPolicyGVK)

	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxEnterpriseContract{}).
		Named("konfluxenterprisecontract").
		Owns(&corev1.Namespace{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&corev1.ConfigMap{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.RoleBinding{}).
		// Self-healing: external deletion or spec mutation of the ECP triggers re-reconcile and SSA restore.
		Owns(ecPolicy, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		// Watch CRDs so that out-of-band deletion triggers reconcile and re-apply.
		Watches(&apiextensionsv1.CustomResourceDefinition{},
			handler.EnqueueRequestsFromMapFunc(crdMapFunc)).
		Complete(r)
}
