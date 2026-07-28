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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

// OperandServiceMonitorFromStore returns the embedded operand ServiceMonitor matching
// namespace and name for a component. Used by ApplyServiceMonitor callbacks in deferred
// ServiceMonitor apply (SM is skipped in applyManifests, applied here).
func OperandServiceMonitorFromStore(store *manifests.ObjectStore, component manifests.Component, namespace, name string) (client.Object, bool, error) {
	objects, err := store.GetByGVK(component, operandServiceMonitorGVK)
	if err != nil {
		return nil, false, err
	}

	for _, obj := range objects {
		if obj.GetNamespace() == namespace && obj.GetName() == name {
			return obj, true, nil
		}
	}
	return nil, false, nil
}
