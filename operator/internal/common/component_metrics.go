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
	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

// ForwardedComponentMetrics returns a deep copy of the Konflux component metrics config for sub-CR specs.
func ForwardedComponentMetrics(owner *konfluxv1alpha1.Konflux) *konfluxv1alpha1.ComponentMetricsConfig {
	if owner == nil || owner.Spec.ComponentMetrics == nil {
		return nil
	}
	return owner.Spec.ComponentMetrics.DeepCopy()
}
