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

package common

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// GetKind returns the Kind of a client.Object.
// For unstructured objects, it uses the GVK directly.
// For typed objects, it uses the GVK from the object's metadata.
func GetKind(obj client.Object) string {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		return u.GetKind()
	}
	return obj.GetObjectKind().GroupVersionKind().Kind
}

// SetOwnership sets owner reference and labels on the object to establish ownership.
func SetOwnership(obj client.Object, owner client.Object, component string, scheme *runtime.Scheme) error {
	// Set ownership labels
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[KonfluxOwnerLabel] = owner.GetName()
	labels[KonfluxComponentLabel] = component
	obj.SetLabels(labels)

	// Set owner reference for garbage collection and watch triggers
	// Since Konflux CR is cluster-scoped, it can own both cluster-scoped and namespaced resources
	if err := controllerutil.SetControllerReference(owner, obj, scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	return nil
}

// ApplyObject applies a single object to the cluster using server-side apply.
// Server-side apply is idempotent and only triggers updates when there are actual changes,
// preventing reconcile loops when watching owned resources.
func ApplyObject(ctx context.Context, k8sClient client.Client, obj client.Object) error {
	return k8sClient.Patch(ctx, obj, client.Apply, client.FieldOwner("konflux-operator"), client.ForceOwnership)
}

