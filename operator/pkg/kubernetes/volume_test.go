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
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

func TestFindVolume(t *testing.T) {
	volumes := []corev1.Volume{
		{Name: "config"},
		{Name: "data"},
	}

	t.Run("returns pointer to matching volume", func(t *testing.T) {
		g := gomega.NewWithT(t)
		result := FindVolume(volumes, "data")
		g.Expect(result).NotTo(gomega.BeNil())
		g.Expect(result.Name).To(gomega.Equal("data"))
		g.Expect(result).To(gomega.BeIdenticalTo(&volumes[1]))
	})

	t.Run("returns nil when not found", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(FindVolume(volumes, "does-not-exist")).To(gomega.BeNil())
	})

	t.Run("returns nil for empty slice", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(FindVolume(nil, "config")).To(gomega.BeNil())
	})
}

func TestFindVolumeMount(t *testing.T) {
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/config"},
		{Name: "data", MountPath: "/mnt/data"},
	}

	t.Run("returns pointer to matching mount", func(t *testing.T) {
		g := gomega.NewWithT(t)
		result := FindVolumeMount(mounts, "data")
		g.Expect(result).NotTo(gomega.BeNil())
		g.Expect(result.Name).To(gomega.Equal("data"))
		g.Expect(result.MountPath).To(gomega.Equal("/mnt/data"))
		g.Expect(result).To(gomega.BeIdenticalTo(&mounts[1]))
	})

	t.Run("returns nil when not found", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(FindVolumeMount(mounts, "does-not-exist")).To(gomega.BeNil())
	})

	t.Run("returns nil for empty slice", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(FindVolumeMount(nil, "data")).To(gomega.BeNil())
	})
}
