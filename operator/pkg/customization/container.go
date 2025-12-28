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
	corev1 "k8s.io/api/core/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

// DeploymentContext holds deployment-level settings that can affect container options.
type DeploymentContext struct {
	// Replicas is the number of replicas for the deployment.
	Replicas int32
}

// ContainerOption is a functional option for configuring a container overlay.
// It receives the DeploymentContext to allow deployment-level settings to affect container config.
type ContainerOption func(*corev1.Container, DeploymentContext)

// NewContainerOverlay creates a container overlay with the given options and context.
func NewContainerOverlay(ctx DeploymentContext, opts ...ContainerOption) *corev1.Container {
	c := &corev1.Container{}
	for _, opt := range opts {
		opt(c, ctx)
	}
	return c
}

// FromContainerSpec creates options from a user-facing ContainerSpec.
func FromContainerSpec(spec *konfluxv1alpha1.ContainerSpec) ContainerOption {
	return func(c *corev1.Container, _ DeploymentContext) {
		if spec != nil && spec.Resources != nil {
			c.Resources = *spec.Resources
		}
	}
}

// WithArgs appends arguments to the container's command.
func WithArgs(args ...string) ContainerOption {
	return func(c *corev1.Container, _ DeploymentContext) {
		c.Args = append(c.Args, args...)
	}
}

// WithEnv adds environment variables to the container.
func WithEnv(env ...corev1.EnvVar) ContainerOption {
	return func(c *corev1.Container, _ DeploymentContext) {
		c.Env = append(c.Env, env...)
	}
}

// WithResources sets resource requirements for the container.
func WithResources(resources corev1.ResourceRequirements) ContainerOption {
	return func(c *corev1.Container, _ DeploymentContext) {
		c.Resources = resources
	}
}

// WithVolumeMounts adds volume mounts to the container.
func WithVolumeMounts(mounts ...corev1.VolumeMount) ContainerOption {
	return func(c *corev1.Container, _ DeploymentContext) {
		c.VolumeMounts = append(c.VolumeMounts, mounts...)
	}
}

// WithSecurityContext sets the security context for the container.
func WithSecurityContext(sc *corev1.SecurityContext) ContainerOption {
	return func(c *corev1.Container, _ DeploymentContext) {
		c.SecurityContext = sc
	}
}

// WithImage sets the container image.
func WithImage(image string) ContainerOption {
	return func(c *corev1.Container, _ DeploymentContext) {
		c.Image = image
	}
}

// --- Context-aware options ---

// WithLeaderElection adds --leader-elect=true if replicas > 1.
func WithLeaderElection() ContainerOption {
	return func(c *corev1.Container, ctx DeploymentContext) {
		if ctx.Replicas > 1 {
			c.Args = append(c.Args, "--leader-elect=true")
		}
	}
}
