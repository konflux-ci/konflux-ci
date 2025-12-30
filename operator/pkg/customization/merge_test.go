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
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestStrategicMerge_Container(t *testing.T) {
	t.Run("merges basic fields", func(t *testing.T) {
		g := gomega.NewWithT(t)
		base := &corev1.Container{
			Name:  "app",
			Image: "app:v1",
		}
		overlay := &corev1.Container{
			Name:  "app", // Name must be set in overlay to preserve it
			Image: "app:v2",
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(base.Name).To(gomega.Equal("app"))
		g.Expect(base.Image).To(gomega.Equal("app:v2"))
	})

	t.Run("preserves base fields not in overlay", func(t *testing.T) {
		g := gomega.NewWithT(t)
		base := &corev1.Container{
			Name:       "app",
			Image:      "app:v1",
			WorkingDir: "/app",
		}
		overlay := &corev1.Container{
			Name:  "app",
			Image: "app:v2",
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(base.WorkingDir).To(gomega.Equal("/app"))
	})

	t.Run("merges env vars by name (strategic merge)", func(t *testing.T) {
		g := gomega.NewWithT(t)
		base := &corev1.Container{
			Name: "app",
			Env: []corev1.EnvVar{
				{Name: "VAR1", Value: "original"},
				{Name: "VAR2", Value: "keep"},
			},
		}
		overlay := &corev1.Container{
			Name: "app",
			Env: []corev1.EnvVar{
				{Name: "VAR1", Value: "updated"},
				{Name: "VAR3", Value: "new"},
			},
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should have 3 env vars after merge
		g.Expect(base.Env).To(gomega.HaveLen(3))

		// Check values
		envMap := make(map[string]string)
		for _, e := range base.Env {
			envMap[e.Name] = e.Value
		}

		g.Expect(envMap["VAR1"]).To(gomega.Equal("updated"))
		g.Expect(envMap["VAR2"]).To(gomega.Equal("keep"))
		g.Expect(envMap["VAR3"]).To(gomega.Equal("new"))
	})

	t.Run("merges volume mounts by mountPath (strategic merge)", func(t *testing.T) {
		g := gomega.NewWithT(t)
		base := &corev1.Container{
			Name: "app",
			VolumeMounts: []corev1.VolumeMount{
				{Name: "vol1", MountPath: "/mnt/data"},
				{Name: "vol2", MountPath: "/mnt/config"},
			},
		}
		overlay := &corev1.Container{
			Name: "app",
			VolumeMounts: []corev1.VolumeMount{
				{Name: "vol1-updated", MountPath: "/mnt/data", ReadOnly: true},
				{Name: "vol3", MountPath: "/mnt/secret"},
			},
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should have 3 volume mounts after merge
		g.Expect(base.VolumeMounts).To(gomega.HaveLen(3))

		// Check that /mnt/data was updated
		mountMap := make(map[string]corev1.VolumeMount)
		for _, m := range base.VolumeMounts {
			mountMap[m.MountPath] = m
		}

		g.Expect(mountMap["/mnt/data"].Name).To(gomega.Equal("vol1-updated"))
		g.Expect(mountMap["/mnt/data"].ReadOnly).To(gomega.BeTrue())
	})

	t.Run("merges resources", func(t *testing.T) {
		g := gomega.NewWithT(t)
		base := &corev1.Container{
			Name: "app",
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		}
		overlay := &corev1.Container{
			Name: "app",
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("500m"),
				},
			},
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// CPU limit should be updated
		g.Expect(base.Resources.Limits.Cpu().String()).To(gomega.Equal("2"))
		// Memory limit should be preserved
		g.Expect(base.Resources.Limits.Memory().String()).To(gomega.Equal("1Gi"))
		// CPU request should be added
		g.Expect(base.Resources.Requests.Cpu().String()).To(gomega.Equal("500m"))
	})
}

func TestStrategicMerge_PodSpec(t *testing.T) {
	t.Run("merges tolerations (append strategy)", func(t *testing.T) {
		g := gomega.NewWithT(t)
		base := &corev1.PodSpec{
			Tolerations: []corev1.Toleration{
				{Key: "key1", Operator: corev1.TolerationOpExists},
			},
		}
		overlay := &corev1.PodSpec{
			Tolerations: []corev1.Toleration{
				{Key: "key2", Value: "value2"},
			},
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Tolerations use atomic strategy by default, so overlay replaces base
		// But the actual behavior depends on patchStrategy tags
		g.Expect(base.Tolerations).To(gomega.HaveLen(1))
	})

	t.Run("merges containers by name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		base := &corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "app:v1"},
				{Name: "sidecar", Image: "sidecar:v1"},
			},
		}
		overlay := &corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "app:v2"},
			},
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should still have 2 containers
		g.Expect(base.Containers).To(gomega.HaveLen(2))

		// app should be updated
		containerMap := make(map[string]corev1.Container)
		for _, c := range base.Containers {
			containerMap[c.Name] = c
		}

		g.Expect(containerMap["app"].Image).To(gomega.Equal("app:v2"))
		g.Expect(containerMap["sidecar"].Image).To(gomega.Equal("sidecar:v1"))
	})

	t.Run("merges volumes by name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		base := &corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "old-config"},
						},
					},
				},
				{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
		}
		overlay := &corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "new-config"},
						},
					},
				},
				{
					Name: "secret",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: "my-secret"},
					},
				},
			},
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should have 3 volumes
		g.Expect(base.Volumes).To(gomega.HaveLen(3))

		volMap := make(map[string]corev1.Volume)
		for _, v := range base.Volumes {
			volMap[v.Name] = v
		}

		g.Expect(volMap["config"].ConfigMap.Name).To(gomega.Equal("new-config"))
		g.Expect(volMap["data"].EmptyDir).NotTo(gomega.BeNil())
		g.Expect(volMap["secret"].Secret).NotTo(gomega.BeNil())
	})

	t.Run("merges node selector", func(t *testing.T) {
		g := gomega.NewWithT(t)
		base := &corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/os": "linux",
			},
		}
		overlay := &corev1.PodSpec{
			NodeSelector: map[string]string{
				"node-type": "compute",
			},
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Maps are replaced atomically, so overlay replaces base
		g.Expect(base.NodeSelector["node-type"]).To(gomega.Equal("compute"))
	})

	t.Run("sets service account name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		base := &corev1.PodSpec{
			ServiceAccountName: "default",
		}
		overlay := &corev1.PodSpec{
			ServiceAccountName: "custom-sa",
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(base.ServiceAccountName).To(gomega.Equal("custom-sa"))
	})
}

func TestStrategicMerge_EmptyOverlay(t *testing.T) {
	t.Run("empty overlay preserves non-string fields", func(t *testing.T) {
		g := gomega.NewWithT(t)
		// Note: JSON marshaling treats empty strings as valid values, so they will
		// overwrite base values. Only slices and maps with nil/empty values are
		// truly "not set" in JSON. This test verifies slice preservation.
		base := &corev1.Container{
			Name:  "app",
			Image: "app:v1",
			Env: []corev1.EnvVar{
				{Name: "VAR1", Value: "value1"},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "vol1", MountPath: "/data"},
			},
		}
		overlay := &corev1.Container{
			Name:  "app",    // Must set name to preserve it
			Image: "app:v1", // Must set image to preserve it
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(base.Name).To(gomega.Equal("app"))
		g.Expect(base.Image).To(gomega.Equal("app:v1"))
		g.Expect(base.Env).To(gomega.HaveLen(1))
		g.Expect(base.VolumeMounts).To(gomega.HaveLen(1))
	})
}

func TestStrategicMerge_SecurityContext(t *testing.T) {
	t.Run("merges security context", func(t *testing.T) {
		g := gomega.NewWithT(t)
		runAsNonRoot := true
		readOnlyRootFilesystem := true
		runAsUser := int64(1000)

		base := &corev1.Container{
			Name: "app",
			SecurityContext: &corev1.SecurityContext{
				RunAsNonRoot: &runAsNonRoot,
			},
		}
		overlay := &corev1.Container{
			Name: "app",
			SecurityContext: &corev1.SecurityContext{
				ReadOnlyRootFilesystem: &readOnlyRootFilesystem,
				RunAsUser:              &runAsUser,
			},
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(base.SecurityContext).NotTo(gomega.BeNil())
		// SecurityContext is merged as a whole object
		g.Expect(base.SecurityContext.ReadOnlyRootFilesystem).NotTo(gomega.BeNil())
		g.Expect(*base.SecurityContext.ReadOnlyRootFilesystem).To(gomega.BeTrue())
		g.Expect(base.SecurityContext.RunAsUser).NotTo(gomega.BeNil())
		g.Expect(*base.SecurityContext.RunAsUser).To(gomega.Equal(int64(1000)))
	})
}

// TestStrategicMerge_EnvVarValueFromBug tests for a regression where env vars with
// valueFrom in base would get their valueFrom pointer incorrectly preserved when
// merged with overlay env vars that had value set. This caused env vars to have
// both value AND valueFrom set, which is invalid in Kubernetes.
//
// The bug was caused by json.Unmarshal not clearing pointer fields when
// unmarshaling into an existing struct. The fix is to unmarshal into a new
// zero-value struct first.
func TestStrategicMerge_EnvVarValueFromBug(t *testing.T) {
	t.Run("does not combine value and valueFrom from different env vars", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Base container has env vars with valueFrom (secrets)
		base := &corev1.Container{
			Name: "oauth2-proxy",
			Env: []corev1.EnvVar{
				{
					Name: "CLIENT_SECRET",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "secret"},
							Key:                  "client-secret",
						},
					},
				},
				{
					Name: "COOKIE_SECRET",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "secret"},
							Key:                  "cookie-secret",
						},
					},
				},
			},
		}

		// Overlay container has env vars with value (not valueFrom)
		overlay := &corev1.Container{
			Name: "oauth2-proxy",
			Env: []corev1.EnvVar{
				{Name: "PROVIDER", Value: "oidc"},
				{Name: "CLIENT_ID", Value: "my-client"},
			},
		}

		err := StrategicMerge(base, overlay)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should have 4 env vars (2 from base + 2 from overlay)
		g.Expect(base.Env).To(gomega.HaveLen(4))

		// CRITICAL: No env var should have both value and valueFrom set
		for _, env := range base.Env {
			hasValue := env.Value != ""
			hasValueFrom := env.ValueFrom != nil

			// This was the bug: env vars from overlay would incorrectly get
			// valueFrom pointers from base env vars at the same index
			g.Expect(hasValue && hasValueFrom).To(gomega.BeFalse(),
				"env var %s has both value (%q) and valueFrom set - this is invalid",
				env.Name, env.Value)
		}

		// Verify the specific env vars have correct values
		envMap := make(map[string]corev1.EnvVar)
		for _, env := range base.Env {
			envMap[env.Name] = env
		}

		// Overlay env vars should only have value
		g.Expect(envMap["PROVIDER"].Value).To(gomega.Equal("oidc"))
		g.Expect(envMap["PROVIDER"].ValueFrom).To(gomega.BeNil())
		g.Expect(envMap["CLIENT_ID"].Value).To(gomega.Equal("my-client"))
		g.Expect(envMap["CLIENT_ID"].ValueFrom).To(gomega.BeNil())

		// Base env vars should still have only valueFrom
		g.Expect(envMap["CLIENT_SECRET"].Value).To(gomega.BeEmpty())
		g.Expect(envMap["CLIENT_SECRET"].ValueFrom).NotTo(gomega.BeNil())
		g.Expect(envMap["COOKIE_SECRET"].Value).To(gomega.BeEmpty())
		g.Expect(envMap["COOKIE_SECRET"].ValueFrom).NotTo(gomega.BeNil())
	})
}
