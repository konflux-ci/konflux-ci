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

package kubernetes

import (
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SSAPatch is a client.Patch implementation for server-side apply.
// It replaces the deprecated client.Apply constant removed in
// controller-runtime v0.23.x.
var SSAPatch client.Patch = ssaPatch{}

type ssaPatch struct{}

// Type returns the ApplyPatchType for server-side apply.
func (p ssaPatch) Type() types.PatchType {
	return types.ApplyPatchType
}

// Data serializes the object to JSON for server-side apply.
func (p ssaPatch) Data(obj client.Object) ([]byte, error) {
	return json.Marshal(obj)
}
