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

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

// OperandServiceMonitorFromObjects returns the embedded operand ServiceMonitor matching
// namespace and name from a component manifest object list. Used by ApplyServiceMonitor
// callbacks in deferred ServiceMonitor apply (SM is skipped in applyManifests, applied here).
func OperandServiceMonitorFromObjects(objects []client.Object, namespace, name string) (client.Object, bool) {
	for _, obj := range objects {
		if !kubernetes.IsComponentMetricsServiceMonitor(obj) {
			continue
		}
		if obj.GetNamespace() == namespace && obj.GetName() == name {
			return obj, true
		}
	}
	return nil, false
}
