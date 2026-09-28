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

// FindVolume returns a pointer to the volume with the given name, or nil if not found.
func FindVolume(volumes []corev1.Volume, name string) *corev1.Volume {
	i := slices.IndexFunc(volumes, func(v corev1.Volume) bool {
		return v.Name == name
	})
	if i == -1 {
		return nil
	}
	return &volumes[i]
}

// FindVolumeMount returns a pointer to the volume mount with the given name, or nil if not found.
func FindVolumeMount(mounts []corev1.VolumeMount, name string) *corev1.VolumeMount {
	i := slices.IndexFunc(mounts, func(m corev1.VolumeMount) bool {
		return m.Name == name
	})
	if i == -1 {
		return nil
	}
	return &mounts[i]
}
