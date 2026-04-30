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

// Package kubernetes provides shared helpers for working with Kubernetes API objects.
package kubernetes

import (
	"encoding/json"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsCustomResourceDefinition returns true if obj is a CustomResourceDefinition.
// It is used by pkg/manifests (GetCRDNamesForComponent) and pkg/tracking (SetOwnership
// skips controller reference on CRDs so they are not cascade-deleted when the CR is removed).
func IsCustomResourceDefinition(obj client.Object) bool {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if !gvk.Empty() {
		return gvk.Group == "apiextensions.k8s.io" && gvk.Kind == "CustomResourceDefinition"
	}
	// Fallback for typed CRD when GVK is not set (e.g. struct literal).
	_, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	return ok
}

// SSAApplyPatch is a client.Patch that uses server-side apply.
// Use this instead of the deprecated client.Apply constant.
var SSAApplyPatch client.Patch = ssaPatch{}

type ssaPatch struct{}

func (p ssaPatch) Type() types.PatchType {
	return types.ApplyPatchType
}

func (p ssaPatch) Data(obj client.Object) ([]byte, error) {
	return json.Marshal(obj)
}
