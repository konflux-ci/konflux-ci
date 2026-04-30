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
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

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

// EnterpriseContractCleanupGVKs defines which resource types should be cleaned up when they are
// no longer part of the desired state. All resources managed by this controller are always
// applied, so no cleanup GVKs are needed (they're always tracked and never become orphans).
var EnterpriseContractCleanupGVKs = []schema.GroupVersionKind{}

// EnterpriseContractClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. This is a security measure to prevent attackers from
// triggering deletion of arbitrary cluster resources by adding the owner label.
// EnterpriseContractClusterScopedAllowList restricts which cluster-scoped resources can be deleted
// during orphan cleanup. All cluster-scoped resources managed by this controller are always
// applied, so no allow list is needed (they're always tracked and never become orphans).
var EnterpriseContractClusterScopedAllowList tracking.ClusterScopedAllowList = nil

// ecPolicyGVK is the GroupVersionKind for the EnterpriseContractPolicy CRD
// managed by this controller.
var ecPolicyGVK = schema.GroupVersionKind{
	Group:   "appstudio.redhat.com",
	Version: "v1alpha1",
	Kind:    "EnterpriseContractPolicy",
}

// KonfluxEnterpriseContractReconciler reconciles a KonfluxEnterpriseContract object
type KonfluxEnterpriseContractReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ObjectStore *manifests.ObjectStore

	// Fields for dynamic watch of EnterpriseContractPolicy resources.
	// The CRD is deployed by this controller, so the watch cannot be
	// registered at startup (the CRD does not exist yet). Instead, we
	// start the watch on the first successful reconcile.
	ctrl       controller.Controller
	cache      cache.Cache
	restMapper meta.RESTMapper
	mu         sync.Mutex
	watching   bool
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
// TODO(user): Modify the Reconcile function to compare the state specified by
// the KonfluxEnterpriseContract object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
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

	// Apply all embedded manifests (including the EnterpriseContractPolicy CRD and CRs)
	if err := r.applyManifests(ctx, tc); err != nil {
		return errHandler.HandleApplyError(ctx, err)
	}

	// Start watching EnterpriseContractPolicy CRs once the CRD has been applied.
	if err := r.startECPolicyWatch(ctx); err != nil {
		return ctrl.Result{}, err
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
// Manifests are parsed once and cached; deep copies are used during reconciliation.
func (r *KonfluxEnterpriseContractReconciler) applyManifests(ctx context.Context, tc *tracking.Client) error {
	objects, err := r.ObjectStore.GetForComponent(manifests.EnterpriseContract)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for EnterpriseContract: %w", err)
	}

	for _, obj := range objects {
		// Apply with ownership using the tracking client
		if err := tc.ApplyOwned(ctx, obj); err != nil {
			return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
				obj.GetNamespace(), obj.GetName(), tracking.GetKind(obj), manifests.EnterpriseContract, err)
		}
	}
	return nil
}

// startECPolicyWatch registers a dynamic watch for EnterpriseContractPolicy
// CRs. The watch is deferred because this controller creates the ECP CRD
// itself; registering the watch at startup would deadlock the manager
// (WaitForCacheSync blocks until the informer syncs, but the CRD does not
// exist until Reconcile runs). Instead, we start the watch after the first
// successful manifest apply, when the CRD is guaranteed to exist.
func (r *KonfluxEnterpriseContractReconciler) startECPolicyWatch(ctx context.Context) error {
	if r.ctrl == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.watching {
		return nil
	}

	log := logf.FromContext(ctx)

	ecPolicy := &unstructured.Unstructured{}
	ecPolicy.SetGroupVersionKind(ecPolicyGVK)

	if err := r.ctrl.Watch(
		source.Kind(
			r.cache,
			ecPolicy,
			handler.TypedEnqueueRequestForOwner[*unstructured.Unstructured](
				r.Scheme, r.restMapper,
				&konfluxv1alpha1.KonfluxEnterpriseContract{}),
		),
	); err != nil {
		log.Error(err, "Failed to start watch for EnterpriseContractPolicy, will retry next reconcile")
		return nil
	}

	r.watching = true
	log.Info("Started dynamic watch for EnterpriseContractPolicy resources")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxEnterpriseContractReconciler) SetupWithManager(mgr ctrl.Manager) error {
	crdMapFunc, err := crdhandler.MapCRDToRequest(r.ObjectStore, manifests.EnterpriseContract, CRName)
	if err != nil {
		return err
	}

	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.KonfluxEnterpriseContract{}).
		Named("konfluxenterprisecontract").
		Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.DeploymentReadinessPredicate)).
		Owns(&corev1.Service{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Namespace{}, builder.WithPredicates(predicate.IgnoreStatusUpdatesPredicate)).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		// EnterpriseContractPolicy is NOT watched here; its CRD is created by
		// this controller, so the watch is started dynamically in Reconcile
		// via startECPolicyWatch to avoid a startup deadlock.
		Watches(&apiextensionsv1.CustomResourceDefinition{},
			handler.EnqueueRequestsFromMapFunc(crdMapFunc)).
		Build(r)
	if err != nil {
		return err
	}

	r.ctrl = c
	r.cache = mgr.GetCache()
	r.restMapper = mgr.GetRESTMapper()
	return nil
}
