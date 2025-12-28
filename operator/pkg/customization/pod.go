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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// PodOverlay holds all customizations for a pod template.
// It can be applied to any workload type (Deployment, StatefulSet, DaemonSet, Job, etc.).
type PodOverlay struct {
	// podSpec holds pod-level customizations (tolerations, nodeSelector, volumes, etc.)
	// Use pod-level options to configure these fields.
	podSpec corev1.PodSpec
	// containerOverlays holds per-container customizations by name
	containerOverlays map[string]*corev1.Container
	// configMapVolumeUpdates holds updates to existing ConfigMap volume references
	configMapVolumeUpdates map[string]string
}

// PodOverlayOption is a functional option for configuring a PodOverlay.
type PodOverlayOption func(*PodOverlay)

// NewPodOverlay creates a PodOverlay with the given options.
func NewPodOverlay(opts ...PodOverlayOption) *PodOverlay {
	p := &PodOverlay{
		containerOverlays: make(map[string]*corev1.Container),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// --- Container options ---

// WithContainer adds a container overlay.
func WithContainer(name string, overlay *corev1.Container) PodOverlayOption {
	return func(p *PodOverlay) {
		if overlay != nil {
			p.containerOverlays[name] = overlay
		}
	}
}

// WithContainerOpts creates a container overlay with the given options and adds it.
func WithContainerOpts(name string, ctx DeploymentContext, opts ...ContainerOption) PodOverlayOption {
	return func(p *PodOverlay) {
		p.containerOverlays[name] = NewContainerOverlay(ctx, opts...)
	}
}

// ContainerBuilder is a function that creates a PodOverlayOption given a deployment context.
type ContainerBuilder func(ctx DeploymentContext) PodOverlayOption

// WithContainerBuilder creates a ContainerBuilder for the given container name and options.
func WithContainerBuilder(name string, opts ...ContainerOption) ContainerBuilder {
	return func(ctx DeploymentContext) PodOverlayOption {
		return WithContainerOpts(name, ctx, opts...)
	}
}

// BuildPodOverlay builds a PodOverlay with the given context and container builders.
func BuildPodOverlay(ctx DeploymentContext, builders ...ContainerBuilder) *PodOverlay {
	opts := make([]PodOverlayOption, len(builders))
	for i, build := range builders {
		opts[i] = build(ctx)
	}

	return NewPodOverlay(opts...)
}

// --- Pod-level options ---

// WithTolerations adds tolerations to the pod.
func WithTolerations(tolerations ...corev1.Toleration) PodOverlayOption {
	return func(p *PodOverlay) {
		p.podSpec.Tolerations = append(p.podSpec.Tolerations, tolerations...)
	}
}

// WithNodeSelector sets the node selector for the pod.
func WithNodeSelector(selector map[string]string) PodOverlayOption {
	return func(p *PodOverlay) {
		p.podSpec.NodeSelector = selector
	}
}

// WithAffinity sets the affinity for the pod.
func WithAffinity(affinity *corev1.Affinity) PodOverlayOption {
	return func(p *PodOverlay) {
		p.podSpec.Affinity = affinity
	}
}

// WithVolumes adds volumes to the pod.
func WithVolumes(volumes ...corev1.Volume) PodOverlayOption {
	return func(p *PodOverlay) {
		p.podSpec.Volumes = append(p.podSpec.Volumes, volumes...)
	}
}

// WithConfigMapVolumeUpdate updates an existing ConfigMap volume's ConfigMap name.
// This modifies the volume in-place during ApplyToPodTemplateSpec, preserving other fields
// like items and defaultMode.
func WithConfigMapVolumeUpdate(volumeName, configMapName string) PodOverlayOption {
	return func(p *PodOverlay) {
		if p.configMapVolumeUpdates == nil {
			p.configMapVolumeUpdates = make(map[string]string)
		}
		p.configMapVolumeUpdates[volumeName] = configMapName
	}
}

// WithServiceAccountName sets the service account name for the pod.
func WithServiceAccountName(name string) PodOverlayOption {
	return func(p *PodOverlay) {
		p.podSpec.ServiceAccountName = name
	}
}

// WithPodSecurityContext sets the pod-level security context.
func WithPodSecurityContext(sc *corev1.PodSecurityContext) PodOverlayOption {
	return func(p *PodOverlay) {
		p.podSpec.SecurityContext = sc
	}
}

// WithPriorityClassName sets the priority class name for the pod.
func WithPriorityClassName(name string) PodOverlayOption {
	return func(p *PodOverlay) {
		p.podSpec.PriorityClassName = name
	}
}

// WithTopologySpreadConstraints sets the topology spread constraints for the pod.
func WithTopologySpreadConstraints(constraints ...corev1.TopologySpreadConstraint) PodOverlayOption {
	return func(p *PodOverlay) {
		p.podSpec.TopologySpreadConstraints = append(p.podSpec.TopologySpreadConstraints, constraints...)
	}
}

// ApplyToPodTemplateSpec applies customizations to a PodTemplateSpec.
// This is the core method that all workload-specific methods use.
func (p *PodOverlay) ApplyToPodTemplateSpec(template *corev1.PodTemplateSpec) error {
	if p == nil || template == nil {
		return nil
	}

	// Apply pod-level customizations using strategic merge
	if err := StrategicMerge(&template.Spec, &p.podSpec); err != nil {
		return err
	}

	// Apply per-container customizations
	if p.containerOverlays != nil {
		if err := mergeContainerList(template.Spec.Containers, p.containerOverlays); err != nil {
			return err
		}
		if err := mergeContainerList(template.Spec.InitContainers, p.containerOverlays); err != nil {
			return err
		}
	}

	// Apply ConfigMap volume updates
	applyConfigMapVolumeUpdates(template.Spec.Volumes, p.configMapVolumeUpdates)

	return nil
}

// ApplyToDeployment applies customizations to a Deployment.
func (p *PodOverlay) ApplyToDeployment(deployment *appsv1.Deployment) error {
	if deployment == nil {
		return nil
	}
	return p.ApplyToPodTemplateSpec(&deployment.Spec.Template)
}

// ApplyToStatefulSet applies customizations to a StatefulSet.
func (p *PodOverlay) ApplyToStatefulSet(statefulSet *appsv1.StatefulSet) error {
	if statefulSet == nil {
		return nil
	}
	return p.ApplyToPodTemplateSpec(&statefulSet.Spec.Template)
}

// ApplyToDaemonSet applies customizations to a DaemonSet.
func (p *PodOverlay) ApplyToDaemonSet(daemonSet *appsv1.DaemonSet) error {
	if daemonSet == nil {
		return nil
	}
	return p.ApplyToPodTemplateSpec(&daemonSet.Spec.Template)
}

// mergeContainerList merges container overlay configurations into a list of containers.
func mergeContainerList(containers []corev1.Container, overlays map[string]*corev1.Container) error {
	for i := range containers {
		base := &containers[i]
		if overlay := overlays[base.Name]; overlay != nil {
			// Set the overlay's name to match the base to prevent strategic merge
			// from overwriting the name with an empty string
			overlay.Name = base.Name
			if err := StrategicMerge(base, overlay); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyConfigMapVolumeUpdates updates ConfigMap volume references in-place.
func applyConfigMapVolumeUpdates(volumes []corev1.Volume, updates map[string]string) {
	if updates == nil {
		return
	}
	for i := range volumes {
		vol := &volumes[i]
		if configMapName, ok := updates[vol.Name]; ok && vol.ConfigMap != nil {
			vol.ConfigMap.Name = configMapName
		}
	}
}
