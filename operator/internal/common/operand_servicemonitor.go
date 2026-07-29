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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

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

// OperandServiceMonitorWatchObjectIfInstalled returns an unstructured ServiceMonitor
// watch object when the CRD is discoverable in mapper. Controllers can call Owns()
// with the returned object to reconcile promptly on out-of-band ServiceMonitor
// delete/mutate without requiring a hard startup dependency on the optional
// ServiceMonitor CRD. Pass mgr.GetRESTMapper() from SetupWithManager.
func OperandServiceMonitorWatchObjectIfInstalled(mapper meta.RESTMapper) (*unstructured.Unstructured, bool) {
	if mapper == nil {
		return nil, false
	}
	// RESTMapper discovery is backed by the API server discovery endpoints.
	// Use RESTMapping here to gate watch registration on runtime CRD availability
	// without wiring a dedicated discovery client into each controller.
	if _, err := mapper.RESTMapping(operandServiceMonitorGVK.GroupKind(), operandServiceMonitorGVK.Version); err != nil {
		// NoMatch means the optional ServiceMonitor CRD is absent — expected.
		// Other errors (discovery/RBAC) still skip the watch, but log so setup
		// failures are visible rather than silent permanent omission.
		if !meta.IsNoMatchError(err) {
			logf.Log.Error(err, "failed to resolve ServiceMonitor REST mapping; skipping watch registration")
		}
		return nil, false
	}
	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(operandServiceMonitorGVK)
	return sm, true
}
