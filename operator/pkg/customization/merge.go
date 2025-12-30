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

package customization

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

// StrategicMerge merges an overlay into a base object using Kubernetes strategic merge patch.
// This uses the official K8s strategic merge logic that respects patchStrategy and patchMergeKey tags.
// It works with any Kubernetes API type (Container, PodSpec, Deployment, etc.).
func StrategicMerge[T any](base, overlay *T) error {
	baseJSON, err := json.Marshal(base)
	if err != nil {
		return err
	}

	overlayJSON, err := json.Marshal(overlay)
	if err != nil {
		return err
	}

	// Use K8s strategic merge patch - respects struct tags like patchMergeKey, patchStrategy
	merged, err := strategicpatch.StrategicMergePatch(baseJSON, overlayJSON, *base)
	if err != nil {
		return err
	}

	// Unmarshal into a new zero-value struct to prevent stale data from the original base.
	// Without this, existing pointer fields (like EnvVar.ValueFrom) would be preserved
	// even when the merged JSON doesn't include them, causing invalid combinations
	// (e.g., EnvVar with both Value and ValueFrom set).
	var result T
	if err := json.Unmarshal(merged, &result); err != nil {
		return err
	}
	*base = result
	return nil
}
