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
	consolev1 "github.com/openshift/api/console/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var consoleLinkGroupKind = consolev1.GroupVersion.WithKind("ConsoleLink").GroupKind()

// ConsoleLinkCRDInstalled reports whether the ConsoleLink CRD is discoverable in
// mapper. The CRD ships with the OpenShift web console, which is an optional
// cluster capability, so it can be absent while IsOpenShift() is still true.
// Controllers gate Owns(&consolev1.ConsoleLink{}) on this: controller-runtime
// waits for every source to sync before starting workers, so watching a kind the
// API server does not serve stops the controller from reconciling at all.
// Pass mgr.GetRESTMapper() from SetupWithManager.
func ConsoleLinkCRDInstalled(mapper meta.RESTMapper) bool {
	if mapper == nil {
		return false
	}
	// RESTMapper discovery is backed by the API server discovery endpoints.
	// Use RESTMapping here to gate watch registration on runtime CRD availability
	// without wiring a dedicated discovery client into each controller.
	if _, err := mapper.RESTMapping(consoleLinkGroupKind, consolev1.GroupVersion.Version); err != nil {
		// NoMatch means the console capability is disabled (or this is not
		// OpenShift) — expected. Other errors (discovery/RBAC) still skip the
		// watch, but log so setup failures are visible rather than silently
		// permanent.
		if !meta.IsNoMatchError(err) {
			logf.Log.Error(err, "failed to resolve ConsoleLink REST mapping; skipping watch registration")
		}
		return false
	}
	return true
}
