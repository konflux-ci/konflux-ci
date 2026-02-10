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

// Package tracking provides a client wrapper that tracks applied resources during
// reconciliation and enables cleanup of orphaned resources. This implements a
// declarative reconciliation pattern where the desired state is implicitly defined
// by the resources that are applied during a reconcile loop.
//
// The Client type implements client.Client, so it can be used anywhere the
// standard controller-runtime client is expected.
//
// Usage with automatic ownership (recommended):
//
//	func (r *MyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
//	    // Create client with ownership config - sets labels and owner references automatically
//	    tc := tracking.NewClientWithOwnership(r.Client, tracking.OwnershipConfig{
//	        Owner:             myCR,
//	        OwnerLabelKey:     "example.com/owner",
//	        ComponentLabelKey: "example.com/component",
//	        Component:         "my-component",
//	        FieldManager:      "my-controller",
//	    })
//
//	    // Apply resources with ownership - automatically sets labels, owner ref, and tracks
//	    if err := tc.ApplyOwned(ctx, deployment); err != nil {
//	        return ctrl.Result{}, err
//	    }
//
//	    // For CreateOrUpdate patterns, use SetOwnership in the mutate function
//	    _, err := tc.CreateOrUpdate(ctx, secret, func() error {
//	        return tc.SetOwnership(secret)
//	    })
//
//	    // Only reached if all applies succeeded - cleanup orphans
//	    return ctrl.Result{}, tc.CleanupOrphans(ctx, ownerLabelKey, ownerName, gvksToClean)
//	}
//
// Usage without ownership (for simple tracking only):
//
//	tc := tracking.NewClient(r.Client)
//	if err := tc.ApplyObject(ctx, deployment, fieldManager); err != nil {
//	    return ctrl.Result{}, err
//	}
package tracking

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

var _ client.Client = &Client{}

// ResourceKey uniquely identifies a Kubernetes resource.
type ResourceKey struct {
	GVK       schema.GroupVersionKind
	Namespace string
	Name      string
}

// ClusterScopedAllowList defines which cluster-scoped resources are allowed to be
// deleted during orphan cleanup. This is a security measure to prevent attackers
// from triggering deletion of arbitrary cluster resources by adding the owner label.
//
// For each GVK, only resources with names in the allow list will be considered for
// deletion. If a GVK is not in the map, all resources of that type are allowed
// (backwards compatible behavior for namespaced resources).
//
// Example:
//
//	allowList := tracking.ClusterScopedAllowList{
//	    {Group: "", Version: "v1", Kind: "Namespace"}: sets.New("my-namespace"),
//	    {Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}: sets.New("my-role-1", "my-role-2"),
//	}
type ClusterScopedAllowList map[schema.GroupVersionKind]sets.Set[string]

// IsAllowed checks if a cluster-scoped resource is allowed to be deleted.
// Returns true if:
// - The resource is namespaced (namespace is not empty)
// - The GVK is not in the allow list (no restrictions for this type)
// - The resource name is in the allow list for this GVK
func (a ClusterScopedAllowList) IsAllowed(gvk schema.GroupVersionKind, namespace, name string) bool {
	// Namespaced resources are always allowed (not cluster-scoped)
	if namespace != "" {
		return true
	}

	// If no allow list defined, allow all (backwards compatible)
	if a == nil {
		return true
	}

	// Check if this GVK has restrictions
	allowedNames, hasRestriction := a[gvk]
	if !hasRestriction {
		// No restriction for this GVK, allow all
		return true
	}

	// Check if the name is in the allow list
	return allowedNames.Has(name)
}

// String returns a human-readable representation of the resource key.
func (k ResourceKey) String() string {
	if k.Namespace == "" {
		return fmt.Sprintf("%s/%s", k.GVK.Kind, k.Name)
	}
	return fmt.Sprintf("%s/%s/%s", k.GVK.Kind, k.Namespace, k.Name)
}

// OwnershipConfig holds configuration for automatic ownership management.
// When configured, ApplyOwned will automatically set labels and owner references.
type OwnershipConfig struct {
	// Owner is the owning resource (e.g., the CR being reconciled)
	Owner client.Object
	// OwnerLabelKey is the label key for the owner name (e.g., "konflux.konflux-ci.dev/owner")
	OwnerLabelKey string
	// ComponentLabelKey is the label key for the component (e.g., "konflux.konflux-ci.dev/component")
	ComponentLabelKey string
	// Component is the component value (e.g., "ui", "konflux")
	Component string
	// FieldManager identifies this controller for server-side apply
	FieldManager string
}

// Client wraps a controller-runtime client and tracks all resources that are
// applied during a reconciliation. This enables cleanup of orphaned resources
// at the end of a successful reconcile.
//
// Create a new Client at the start of each reconcile loop. The tracked state
// is intentionally ephemeral - it only lives for the duration of one reconcile.
type Client struct {
	client.Client
	ownership *OwnershipConfig
	tracked   map[ResourceKey]struct{}
	mu        sync.Mutex
}

// NewClient creates a new tracking client wrapping the given client.
// Call this at the start of each reconcile to get a fresh tracker.
func NewClient(c client.Client) *Client {
	return &Client{
		Client:  c,
		tracked: make(map[ResourceKey]struct{}),
	}
}

// NewClientWithOwnership creates a tracking client configured for automatic ownership management.
// Use ApplyOwned to apply objects with ownership automatically set.
func NewClientWithOwnership(c client.Client, cfg OwnershipConfig) *Client {
	return &Client{
		Client:    c,
		ownership: &cfg,
		tracked:   make(map[ResourceKey]struct{}),
	}
}

// Apply implements client.Writer.Apply for runtime.ApplyConfiguration objects.
// NOTE: This method does NOT track the applied resource. Use ApplyObject instead
// for server-side apply with tracking. This method exists only to satisfy the
// client.Client interface for code paths that use runtime.ApplyConfiguration.
//
// If tracking is needed for ApplyConfiguration objects, use ApplyObject with a
// client.Object instead, or implement tracking for your specific use case.
func (c *Client) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
	return c.Client.Apply(ctx, obj, opts...)
}

// ApplyObject applies an object using server-side apply and tracks it.
// This is the primary method for reconcilers - it uses Patch with client.Apply
// to perform server-side apply and automatically tracks the resource.
func (c *Client) ApplyObject(
	ctx context.Context,
	obj client.Object,
	fieldManager string,
	opts ...client.PatchOption,
) error {
	patchOpts := append([]client.PatchOption{client.FieldOwner(fieldManager), client.ForceOwnership}, opts...)
	if err := c.Client.Patch(ctx, obj, client.Apply, patchOpts...); err != nil {
		return err
	}
	c.track(obj)
	return nil
}

// ApplyOwned sets ownership (labels + owner reference) on the object and applies it
// using server-side apply. The client must be created with NewClientWithOwnership.
// This combines SetOwnership + ApplyObject into a single call for cleaner reconciler code.
func (c *Client) ApplyOwned(ctx context.Context, obj client.Object, opts ...client.PatchOption) error {
	if err := c.SetOwnership(obj); err != nil {
		return err
	}
	return c.ApplyObject(ctx, obj, c.ownership.FieldManager, opts...)
}

// SetOwnership sets ownership labels and owner reference on the object without applying it.
// This is useful for CreateOrUpdate patterns where ownership must be set in the mutate function.
// The client must be created with NewClientWithOwnership.
// Do not set controller reference on CRDs so they are not cascade-deleted when the CR is removed.
func (c *Client) SetOwnership(obj client.Object) error {
	if c.ownership == nil {
		return fmt.Errorf("SetOwnership called but client was not created with ownership config; use NewClientWithOwnership")
	}

	// Set ownership labels
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[c.ownership.OwnerLabelKey] = c.ownership.Owner.GetName()
	labels[c.ownership.ComponentLabelKey] = c.ownership.Component
	obj.SetLabels(labels)

	if kubernetes.IsCustomResourceDefinition(obj) {
		return nil
	}

	// Set owner reference for garbage collection and watch triggers
	if err := controllerutil.SetControllerReference(c.ownership.Owner, obj, c.Scheme()); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	return nil
}

// Patch applies a patch to an object and tracks it if the patch succeeds.
// This overrides the embedded client's Patch method to add tracking.
func (c *Client) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if err := c.Client.Patch(ctx, obj, patch, opts...); err != nil {
		return err
	}
	c.track(obj)
	return nil
}

// Create creates an object and tracks it if the creation succeeds or the object already exists.
// This overrides the embedded client's Create method to add tracking.
// Note: If the object already exists (AlreadyExists error), it is still tracked to prevent
// orphan cleanup from deleting it in subsequent reconcile attempts.
func (c *Client) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	err := c.Client.Create(ctx, obj, opts...)
	if err == nil || apierrors.IsAlreadyExists(err) {
		c.track(obj)
	}
	return err
}

// Update updates an object and tracks it if the update succeeds.
// This overrides the embedded client's Update method to add tracking.
func (c *Client) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if err := c.Client.Update(ctx, obj, opts...); err != nil {
		return err
	}
	c.track(obj)
	return nil
}

// CreateOrUpdate wraps controllerutil.CreateOrUpdate and tracks the object regardless
// of whether it was created, updated, or unchanged. This is necessary because
// controllerutil.CreateOrUpdate only calls Create/Update when changes are needed,
// but we always want to track the object to prevent orphan cleanup from deleting it.
func (c *Client) CreateOrUpdate(
	ctx context.Context,
	obj client.Object,
	f controllerutil.MutateFn,
) (controllerutil.OperationResult, error) {
	result, err := controllerutil.CreateOrUpdate(ctx, c.Client, obj, f)
	if err != nil {
		return result, err
	}
	c.track(obj)
	return result, nil
}

// track adds a resource to the tracked set.
func (c *Client) track(obj client.Object) {
	c.mu.Lock()
	defer c.mu.Unlock()

	gvk := obj.GetObjectKind().GroupVersionKind()

	// If GVK is not set on the object (common for typed objects after client operations),
	// derive it from the scheme.
	if gvk.Empty() {
		gvks, _, err := c.Scheme().ObjectKinds(obj)
		if err == nil && len(gvks) > 0 {
			gvk = gvks[0]
		}
	}

	key := ResourceKey{
		GVK:       gvk,
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
	c.tracked[key] = struct{}{}
}

// IsTracked returns true if the resource is in the tracked set.
// Useful for testing and debugging.
func (c *Client) IsTracked(gvk schema.GroupVersionKind, namespace, name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := ResourceKey{GVK: gvk, Namespace: namespace, Name: name}
	_, exists := c.tracked[key]
	return exists
}

// TrackedResources returns a copy of all tracked resource keys.
// Useful for testing and debugging.
func (c *Client) TrackedResources() []ResourceKey {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := make([]ResourceKey, 0, len(c.tracked))
	for key := range c.tracked {
		keys = append(keys, key)
	}
	return keys
}

// CleanupOptions configures the behavior of CleanupOrphans.
type CleanupOptions struct {
	// ClusterScopedAllowList restricts which cluster-scoped resources can be deleted.
	// If set, only cluster-scoped resources with names in the allow list will be deleted.
	// This is a security measure to prevent deletion of arbitrary cluster resources
	// that an attacker might have labeled with the owner label.
	ClusterScopedAllowList ClusterScopedAllowList
}

// CleanupOption is a functional option for configuring CleanupOrphans.
type CleanupOption func(*CleanupOptions)

// WithClusterScopedAllowList sets the allow list for cluster-scoped resources.
// Only cluster-scoped resources with names in the allow list will be considered
// for deletion during orphan cleanup.
func WithClusterScopedAllowList(allowList ClusterScopedAllowList) CleanupOption {
	return func(opts *CleanupOptions) {
		opts.ClusterScopedAllowList = allowList
	}
}

// CleanupOrphans deletes resources that have the specified owner label but were
// not applied during this reconcile. Only resources matching the provided GVKs
// are considered for cleanup.
//
// Parameters:
//   - ctx: Context for the operation
//   - ownerLabelKey: The label key that identifies ownership (e.g., "konflux.konflux-ci.dev/owner")
//   - ownerLabelValue: The value of the owner label to match (e.g., the CR name)
//   - gvks: List of GroupVersionKinds to check for orphaned resources
//   - opts: Optional configuration (e.g., WithClusterScopedAllowList)
//
// Returns an error if listing or deleting fails. NotFound errors during deletion
// are ignored (resource may have been deleted by another process).
//
// Security: For cluster-scoped resources (Namespace, ClusterRole, ClusterRoleBinding, etc.),
// use WithClusterScopedAllowList to restrict which resources can be deleted. This prevents
// attackers from triggering deletion of arbitrary resources by adding the owner label.
func (c *Client) CleanupOrphans(
	ctx context.Context,
	ownerLabelKey, ownerLabelValue string,
	gvks []schema.GroupVersionKind,
	opts ...CleanupOption,
) error {
	log := logf.FromContext(ctx)
	start := time.Now()

	// Apply options
	options := &CleanupOptions{}
	for _, opt := range opts {
		opt(options)
	}

	g, ctx := errgroup.WithContext(ctx)
	for _, gvk := range gvks {
		g.Go(func() error {
			if err := c.cleanupOrphansForGVK(ctx, ownerLabelKey, ownerLabelValue, gvk, options); err != nil {
				log.Error(err, "Failed to cleanup orphans", "gvk", gvk.String())
				return fmt.Errorf("failed to cleanup orphans for %s: %w", gvk.String(), err)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	log.Info("CleanupOrphans completed", "duration", time.Since(start), "gvkCount", len(gvks))
	return nil
}

// cleanupOrphansForGVK handles cleanup for a single GVK.
func (c *Client) cleanupOrphansForGVK(
	ctx context.Context,
	ownerLabelKey, ownerLabelValue string,
	gvk schema.GroupVersionKind,
	options *CleanupOptions,
) error {
	log := logf.FromContext(ctx)

	// Create an unstructured list to hold resources of this GVK
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	// List all resources with the owner label
	if err := c.List(ctx, list, client.MatchingLabels{
		ownerLabelKey: ownerLabelValue,
	}); err != nil {
		// If the CRD doesn't exist (e.g., ConsoleLink on non-OpenShift), skip cleanup
		if meta.IsNoMatchError(err) {
			log.V(1).Info("Skipping cleanup for GVK (CRD not installed)", "gvk", gvk.String())
			return nil
		}
		return fmt.Errorf("failed to list resources: %w", err)
	}

	// Delete resources that weren't tracked this reconcile
	for i := range list.Items {
		item := &list.Items[i]
		key := ResourceKey{
			GVK:       gvk,
			Namespace: item.GetNamespace(),
			Name:      item.GetName(),
		}

		c.mu.Lock()
		_, wasTracked := c.tracked[key]
		c.mu.Unlock()

		if !wasTracked {
			// We use metav1.IsControlledBy (not controllerutil.HasOwnerReference) because it
			// verifies both name AND UID, preventing spoofed owner references.
			if c.ownership != nil && !metav1.IsControlledBy(item, c.ownership.Owner) {
				log.V(1).Info("Skipping deletion of resource without matching owner reference",
					"gvk", gvk.String(),
					"resource", key.String(),
				)
				continue
			}

			// Security check: for cluster-scoped resources, verify the name is in the allow list
			if options != nil && !options.ClusterScopedAllowList.IsAllowed(gvk, item.GetNamespace(), item.GetName()) {
				log.Info("Skipping deletion of cluster-scoped resource not in allow list",
					"gvk", gvk.String(),
					"name", item.GetName(),
				)
				continue
			}

			log.Info("Deleting orphaned resource",
				"gvk", gvk.String(),
				"resource", key.String(),
			)
			if err := c.Delete(ctx, item); err != nil {
				if client.IgnoreNotFound(err) != nil {
					return fmt.Errorf("failed to delete %s: %w", key.String(), err)
				}
				// Resource already deleted, continue
			}
		}
	}

	return nil
}

// IsNoKindMatchError checks if an error is due to a missing CRD (NoKindMatchError).
// This is used to handle cleanup failures gracefully in test environments where
// certain CRDs may not be installed.
func IsNoKindMatchError(err error) bool {
	var noKindErr *meta.NoKindMatchError
	return errors.As(err, &noKindErr)
}

// GetKind returns the Kind of a client.Object.
// For unstructured objects, it uses the GVK directly.
// For typed objects, it uses the GVK from the object's metadata.
func GetKind(obj client.Object) string {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		return u.GetKind()
	}
	return obj.GetObjectKind().GroupVersionKind().Kind
}
