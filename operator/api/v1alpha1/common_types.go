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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// ContainerSpec defines customizations for a specific container.
// This type is reused across all deployment specs.
type ContainerSpec struct {
	// Resources specifies the resource requirements for the container.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Env specifies environment variables for the container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
}
