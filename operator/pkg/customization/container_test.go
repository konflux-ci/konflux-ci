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

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

func TestNewContainerOverlay(t *testing.T) {
	ctx := DeploymentContext{Replicas: 1}

	t.Run("empty options returns empty container", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx)
		g.Expect(c).NotTo(gomega.BeNil())
		g.Expect(c.Image).To(gomega.BeEmpty())
	})

	t.Run("multiple options are applied in order", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx,
			WithImage("image1"),
			WithImage("image2"),
		)
		g.Expect(c.Image).To(gomega.Equal("image2"))
	})
}

func TestFromContainerSpec(t *testing.T) {
	ctx := DeploymentContext{Replicas: 1}

	t.Run("nil spec is handled gracefully", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx, FromContainerSpec(nil))
		g.Expect(c.Resources.Limits).To(gomega.BeNil())
		g.Expect(c.Resources.Requests).To(gomega.BeNil())
	})

	t.Run("nil resources in spec is handled gracefully", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ContainerSpec{Resources: nil}
		c := NewContainerOverlay(ctx, FromContainerSpec(spec))
		g.Expect(c.Resources.Limits).To(gomega.BeNil())
		g.Expect(c.Resources.Requests).To(gomega.BeNil())
	})

	t.Run("resources are copied from spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ContainerSpec{
			Resources: &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
		}

		c := NewContainerOverlay(ctx, FromContainerSpec(spec))

		g.Expect(c.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		g.Expect(c.Resources.Limits.Memory().String()).To(gomega.Equal("256Mi"))
		g.Expect(c.Resources.Requests.Cpu().String()).To(gomega.Equal("100m"))
		g.Expect(c.Resources.Requests.Memory().String()).To(gomega.Equal("128Mi"))
	})

	t.Run("empty env in spec is handled gracefully", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ContainerSpec{Env: nil}
		c := NewContainerOverlay(ctx, FromContainerSpec(spec))
		g.Expect(c.Env).To(gomega.BeEmpty())
	})

	t.Run("env vars are copied from spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ContainerSpec{
			Env: []corev1.EnvVar{
				{Name: "VAR1", Value: "value1"},
				{Name: "VAR2", Value: "value2"},
			},
		}

		c := NewContainerOverlay(ctx, FromContainerSpec(spec))

		g.Expect(c.Env).To(gomega.HaveLen(2))
		g.Expect(c.Env[0].Name).To(gomega.Equal("VAR1"))
		g.Expect(c.Env[0].Value).To(gomega.Equal("value1"))
		g.Expect(c.Env[1].Name).To(gomega.Equal("VAR2"))
		g.Expect(c.Env[1].Value).To(gomega.Equal("value2"))
	})

	t.Run("env vars from spec are appended to existing env", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ContainerSpec{
			Env: []corev1.EnvVar{
				{Name: "FROM_SPEC", Value: "spec-value"},
			},
		}

		c := NewContainerOverlay(ctx,
			WithEnv(corev1.EnvVar{Name: "EXISTING", Value: "existing-value"}),
			FromContainerSpec(spec),
		)

		g.Expect(c.Env).To(gomega.HaveLen(2))
		g.Expect(c.Env[0].Name).To(gomega.Equal("EXISTING"))
		g.Expect(c.Env[1].Name).To(gomega.Equal("FROM_SPEC"))
	})

	t.Run("env vars with valueFrom are copied from spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ContainerSpec{
			Env: []corev1.EnvVar{
				{
					Name: "SECRET_VAR",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
							Key:                  "password",
						},
					},
				},
			},
		}

		c := NewContainerOverlay(ctx, FromContainerSpec(spec))

		g.Expect(c.Env).To(gomega.HaveLen(1))
		g.Expect(c.Env[0].Name).To(gomega.Equal("SECRET_VAR"))
		g.Expect(c.Env[0].ValueFrom).NotTo(gomega.BeNil())
		g.Expect(c.Env[0].ValueFrom.SecretKeyRef.Name).To(gomega.Equal("my-secret"))
		g.Expect(c.Env[0].ValueFrom.SecretKeyRef.Key).To(gomega.Equal("password"))
	})
}

func TestWithArgs(t *testing.T) {
	ctx := DeploymentContext{Replicas: 1}

	t.Run("appends args to empty container", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx, WithArgs("--flag1", "--flag2"))
		g.Expect(c.Args).To(gomega.HaveLen(2))
		g.Expect(c.Args).To(gomega.Equal([]string{"--flag1", "--flag2"}))
	})

	t.Run("appends args to existing args", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx,
			WithArgs("--first"),
			WithArgs("--second", "--third"),
		)
		g.Expect(c.Args).To(gomega.Equal([]string{"--first", "--second", "--third"}))
	})
}

func TestWithEnv(t *testing.T) {
	ctx := DeploymentContext{Replicas: 1}

	t.Run("adds environment variables", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx, WithEnv(
			corev1.EnvVar{Name: "VAR1", Value: "value1"},
			corev1.EnvVar{Name: "VAR2", Value: "value2"},
		))
		g.Expect(c.Env).To(gomega.HaveLen(2))
		g.Expect(c.Env[0].Name).To(gomega.Equal("VAR1"))
		g.Expect(c.Env[0].Value).To(gomega.Equal("value1"))
		g.Expect(c.Env[1].Name).To(gomega.Equal("VAR2"))
		g.Expect(c.Env[1].Value).To(gomega.Equal("value2"))
	})

	t.Run("appends to existing env", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx,
			WithEnv(corev1.EnvVar{Name: "FIRST", Value: "1"}),
			WithEnv(corev1.EnvVar{Name: "SECOND", Value: "2"}),
		)
		g.Expect(c.Env).To(gomega.HaveLen(2))
	})
}

func TestWithResources(t *testing.T) {
	ctx := DeploymentContext{Replicas: 1}

	t.Run("sets resource requirements", func(t *testing.T) {
		g := gomega.NewWithT(t)
		resources := corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
		}

		c := NewContainerOverlay(ctx, WithResources(resources))
		g.Expect(c.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
	})

	t.Run("overwrites existing resources", func(t *testing.T) {
		g := gomega.NewWithT(t)
		resources1 := corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		}
		resources2 := corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
		}

		c := NewContainerOverlay(ctx,
			WithResources(resources1),
			WithResources(resources2),
		)
		g.Expect(c.Resources.Limits.Cpu().String()).To(gomega.Equal("2"))
	})
}

func TestWithVolumeMounts(t *testing.T) {
	ctx := DeploymentContext{Replicas: 1}

	t.Run("adds volume mounts", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx, WithVolumeMounts(
			corev1.VolumeMount{Name: "vol1", MountPath: "/mnt/vol1"},
			corev1.VolumeMount{Name: "vol2", MountPath: "/mnt/vol2"},
		))
		g.Expect(c.VolumeMounts).To(gomega.HaveLen(2))
		g.Expect(c.VolumeMounts[0].Name).To(gomega.Equal("vol1"))
		g.Expect(c.VolumeMounts[0].MountPath).To(gomega.Equal("/mnt/vol1"))
	})
}

func TestWithSecurityContext(t *testing.T) {
	ctx := DeploymentContext{Replicas: 1}

	t.Run("sets security context", func(t *testing.T) {
		g := gomega.NewWithT(t)
		runAsNonRoot := true
		sc := &corev1.SecurityContext{
			RunAsNonRoot: &runAsNonRoot,
		}

		c := NewContainerOverlay(ctx, WithSecurityContext(sc))
		g.Expect(c.SecurityContext).NotTo(gomega.BeNil())
		g.Expect(*c.SecurityContext.RunAsNonRoot).To(gomega.BeTrue())
	})
}

func TestWithImage(t *testing.T) {
	ctx := DeploymentContext{Replicas: 1}

	t.Run("sets container image", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx, WithImage("nginx:latest"))
		g.Expect(c.Image).To(gomega.Equal("nginx:latest"))
	})
}

func TestWithLeaderElection(t *testing.T) {
	tests := []struct {
		name          string
		replicas      int32
		expectedArgs  int
		shouldContain string
	}{
		{
			name:         "single replica - no leader election",
			replicas:     1,
			expectedArgs: 0,
		},
		{
			name:          "multiple replicas - adds leader election",
			replicas:      2,
			expectedArgs:  1,
			shouldContain: "--leader-elect=true",
		},
		{
			name:          "many replicas - adds leader election",
			replicas:      5,
			expectedArgs:  1,
			shouldContain: "--leader-elect=true",
		},
		{
			name:         "zero replicas - no leader election",
			replicas:     0,
			expectedArgs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)
			ctx := DeploymentContext{Replicas: tt.replicas}
			c := NewContainerOverlay(ctx, WithLeaderElection())

			g.Expect(c.Args).To(gomega.HaveLen(tt.expectedArgs))

			if tt.expectedArgs > 0 {
				g.Expect(c.Args).To(gomega.ContainElement(tt.shouldContain))
			}
		})
	}
}

func TestCombinedOptions(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := DeploymentContext{Replicas: 3}

	spec := &konfluxv1alpha1.ContainerSpec{
		Resources: &corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("500m"),
			},
		},
		Env: []corev1.EnvVar{
			{Name: "FROM_SPEC", Value: "spec-value"},
		},
	}

	c := NewContainerOverlay(ctx,
		FromContainerSpec(spec),
		WithImage("my-image:v1"),
		WithArgs("--config=/etc/config"),
		WithEnv(corev1.EnvVar{Name: "LOG_LEVEL", Value: "debug"}),
		WithLeaderElection(),
	)

	// Verify all options were applied
	g.Expect(c.Image).To(gomega.Equal("my-image:v1"))
	g.Expect(c.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
	// Should have 2 args: --config and --leader-elect
	g.Expect(c.Args).To(gomega.HaveLen(2))
	g.Expect(c.Args).To(gomega.ContainElements("--config=/etc/config", "--leader-elect=true"))
	// Should have 2 env vars: FROM_SPEC from spec and LOG_LEVEL from WithEnv
	g.Expect(c.Env).To(gomega.HaveLen(2))
	g.Expect(c.Env[0].Name).To(gomega.Equal("FROM_SPEC"))
	g.Expect(c.Env[1].Name).To(gomega.Equal("LOG_LEVEL"))
}
