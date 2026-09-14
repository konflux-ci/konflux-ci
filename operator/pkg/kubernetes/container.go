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
	"slices"

	corev1 "k8s.io/api/core/v1"
)

// FindContainer returns a pointer to the container with the given name, or nil if not found.
func FindContainer(containers []corev1.Container, name string) *corev1.Container {
	i := slices.IndexFunc(containers, func(c corev1.Container) bool {
		return c.Name == name
	})
	if i == -1 {
		return nil
	}
	return &containers[i]
}
