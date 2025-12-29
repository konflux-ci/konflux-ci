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

// orphan_cleanup.go provides functionality to detect and remove orphaned resources
// after operator upgrades. When the operator is upgraded and its manifests change
// (resources removed or renamed), the old resources would remain in the cluster.
// This file implements a label-based approach to identify and clean up such orphans:
//
// 1. Each applied resource is labeled with the operator binary's SHA-256 digest
// 2. After applying current manifests, we query resources with our component label
// 3. Resources with a different (older) operator-version label are deleted
//
// This implementation uses the unstructured client to query resources dynamically
// by GroupVersionKind (GVK), avoiding the need for typed imports and switch statements.
// This approach is ideal for generic utilities like orphan cleanup where we only
// need to read labels and delete resources.

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/konflux-ci/konflux-ci/operator/pkg/version"
)

// resourceGVKs defines all resource types that should be checked for orphaned resources.
// These are the common Kubernetes resource types that the operator manages.
// To support additional resource types (e.g., CRDs), add their GVKs here.
var resourceGVKs = []schema.GroupVersionKind{
	// Namespaced resources
	{Group: "apps", Version: "v1", Kind: "Deployment"},
	{Group: "", Version: "v1", Kind: "Service"},
	{Group: "", Version: "v1", Kind: "ConfigMap"},
	{Group: "", Version: "v1", Kind: "Secret"},
	{Group: "", Version: "v1", Kind: "ServiceAccount"},
	// Cluster-scoped resources
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
}

// PruneOrphanedResources deletes resources that were created by a previous operator version
// but are no longer part of the current manifests. It identifies orphaned resources by
// comparing the operator-version label on existing resources against the current operator's
// binary digest.
//
// Resources are considered orphaned if:
// 1. They have the component label matching the specified component
// 2. They have an operator-version label that differs from the current operator version
//
// Resources without the operator-version label are skipped (could be user-created or from
// older operator versions before this feature was added).
func PruneOrphanedResources(ctx context.Context, k8sClient client.Client, component string) error {
	log := logf.FromContext(ctx)

	currentDigest, err := version.GetBinaryDigest()
	if err != nil {
		log.Error(err, "Failed to get operator binary digest, skipping orphan cleanup")
		return nil // Don't fail reconciliation if we can't get the digest
	}

	// Iterate over all resource types and prune orphaned resources
	for _, gvk := range resourceGVKs {
		if err := pruneResourcesOfKind(ctx, k8sClient, gvk, component, currentDigest); err != nil {
			return err
		}
	}

	return nil
}

// pruneResourcesOfKind lists resources of a specific GVK and deletes orphaned ones.
// It uses the unstructured client to dynamically query resources without requiring
// typed imports for each resource kind.
func pruneResourcesOfKind(ctx context.Context, k8sClient client.Client, gvk schema.GroupVersionKind, component string, currentDigest string) error {
	log := logf.FromContext(ctx)

	// Create an unstructured list with the List kind
	uList := &unstructured.UnstructuredList{}
	uList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	// List all resources with our component label
	if err := k8sClient.List(ctx, uList, client.MatchingLabels{
		KonfluxComponentLabel: component,
	}); err != nil {
		// If we can't list, just log and continue - don't fail the whole reconcile
		log.V(1).Info("Failed to list resources for orphan cleanup",
			"gvk", gvk.String(),
			"error", err,
		)
		return nil
	}

	// Iterate over items and delete orphaned ones
	for i := range uList.Items {
		item := &uList.Items[i]
		labels := item.GetLabels()

		// Skip resources without operator-version label (pre-existing or user-created)
		resourceVersion, hasLabel := labels[OperatorVersionLabel]
		if !hasLabel {
			continue
		}

		// Skip resources with matching version (current)
		if resourceVersion == currentDigest {
			continue
		}

		// This resource has an old version - it's orphaned
		log.Info("Deleting orphaned resource",
			"kind", gvk.Kind,
			"name", item.GetName(),
			"namespace", item.GetNamespace(),
			"oldVersion", resourceVersion,
			"currentVersion", currentDigest,
		)

		if err := k8sClient.Delete(ctx, item); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "Failed to delete orphaned resource",
				"kind", gvk.Kind,
				"name", item.GetName(),
				"namespace", item.GetNamespace(),
			)
			// Continue with other resources even if one fails
		}
	}

	return nil
}
