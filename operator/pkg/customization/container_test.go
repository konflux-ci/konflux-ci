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
	"k8s.io/utils/ptr"
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

func TestWithEnvOverride(t *testing.T) {
	ctx := DeploymentContext{Replicas: 1}

	t.Run("adds environment variable when none exists", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx, WithEnvOverride("VAR1", "value1"))
		g.Expect(c.Env).To(gomega.HaveLen(1))
		g.Expect(c.Env[0].Name).To(gomega.Equal("VAR1"))
		g.Expect(c.Env[0].Value).To(gomega.Equal("value1"))
	})

	t.Run("overrides existing environment variable", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx,
			WithEnv(corev1.EnvVar{Name: "VAR1", Value: "original"}),
			WithEnvOverride("VAR1", "override"),
		)
		g.Expect(c.Env).To(gomega.HaveLen(1))
		g.Expect(c.Env[0].Name).To(gomega.Equal("VAR1"))
		g.Expect(c.Env[0].Value).To(gomega.Equal("override"))
	})

	t.Run("preserves other environment variables when overriding", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx,
			WithEnv(
				corev1.EnvVar{Name: "VAR1", Value: "value1"},
				corev1.EnvVar{Name: "VAR2", Value: "value2"},
				corev1.EnvVar{Name: "VAR3", Value: "value3"},
			),
			WithEnvOverride("VAR2", "override"),
		)
		g.Expect(c.Env).To(gomega.HaveLen(3))
		// VAR1 and VAR3 should be preserved, VAR2 should be at the end with new value
		var var1, var2, var3 *corev1.EnvVar
		for i := range c.Env {
			switch c.Env[i].Name {
			case "VAR1":
				var1 = &c.Env[i]
			case "VAR2":
				var2 = &c.Env[i]
			case "VAR3":
				var3 = &c.Env[i]
			}
		}
		g.Expect(var1).NotTo(gomega.BeNil())
		g.Expect(var1.Value).To(gomega.Equal("value1"))
		g.Expect(var2).NotTo(gomega.BeNil())
		g.Expect(var2.Value).To(gomega.Equal("override"))
		g.Expect(var3).NotTo(gomega.BeNil())
		g.Expect(var3.Value).To(gomega.Equal("value3"))
	})

	t.Run("overrides with empty value", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx,
			WithEnv(corev1.EnvVar{Name: "VAR1", Value: "original"}),
			WithEnvOverride("VAR1", ""),
		)
		// Original should be overridden with empty value
		g.Expect(c.Env).To(gomega.HaveLen(1))
		g.Expect(c.Env[0].Name).To(gomega.Equal("VAR1"))
		g.Expect(c.Env[0].Value).To(gomega.Equal(""))
	})

	t.Run("adds variable with empty value when none exists", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := NewContainerOverlay(ctx, WithEnvOverride("VAR1", ""))
		g.Expect(c.Env).To(gomega.HaveLen(1))
		g.Expect(c.Env[0].Name).To(gomega.Equal("VAR1"))
		g.Expect(c.Env[0].Value).To(gomega.Equal(""))
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

func TestComputeLeaderElect(t *testing.T) {
	t.Run("nil spec returns nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(ComputeLeaderElect(nil)).To(gomega.BeNil())
	})

	t.Run("explicit LeaderElect=true wins regardless of replicas", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{Replicas: 1, LeaderElect: ptr.To(true)}
		g.Expect(ComputeLeaderElect(spec)).To(gomega.Equal(ptr.To(true)))
	})

	t.Run("explicit LeaderElect=false wins even when replicas>1", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{Replicas: 3, LeaderElect: ptr.To(false)}
		g.Expect(ComputeLeaderElect(spec)).To(gomega.Equal(ptr.To(false)))
	})

	t.Run("replicas>1 with no override auto-enables", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{Replicas: 3}
		g.Expect(ComputeLeaderElect(spec)).To(gomega.Equal(ptr.To(true)))
	})

	t.Run("replicas=1 with no override returns nil (embedded manifest default wins)", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{Replicas: 1}
		g.Expect(ComputeLeaderElect(spec)).To(gomega.BeNil())
	})
}

func TestWithLeaderElectionControl(t *testing.T) {
	t.Run("nil enabled leaves args unchanged", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := DeploymentContext{Replicas: 1}
		c := NewContainerOverlay(ctx, WithArgs("--metrics-bind-address=:8080", "--leader-elect"))
		WithLeaderElectionControl(nil)(c, ctx)
		g.Expect(c.Args).To(gomega.Equal([]string{"--metrics-bind-address=:8080", "--leader-elect"}))
	})

	t.Run("true replaces bare --leader-elect with --leader-elect=true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := DeploymentContext{Replicas: 1}
		c := &corev1.Container{Args: []string{"--metrics-bind-address=:8080", "--leader-elect", "--lease-duration=30s"}}
		WithLeaderElectionControl(ptr.To(true))(c, ctx)
		g.Expect(c.Args).To(gomega.ContainElements(
			"--metrics-bind-address=:8080", "--leader-elect=true", "--lease-duration=30s",
		))
		g.Expect(c.Args).NotTo(gomega.ContainElement("--leader-elect"))
	})

	t.Run("false sets --leader-elect=false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := DeploymentContext{Replicas: 1}
		c := &corev1.Container{Args: []string{"--leader-elect"}}
		WithLeaderElectionControl(ptr.To(false))(c, ctx)
		g.Expect(c.Args).To(gomega.Equal([]string{"--leader-elect=false"}))
	})

	t.Run("replaces existing --leader-elect=value", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := DeploymentContext{Replicas: 1}
		c := &corev1.Container{Args: []string{"--leader-elect=false", "--other"}}
		WithLeaderElectionControl(ptr.To(true))(c, ctx)
		g.Expect(c.Args).To(gomega.Equal([]string{"--other", "--leader-elect=true"}))
	})
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
		WithLeaderElectionControl(ptr.To(true)),
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
