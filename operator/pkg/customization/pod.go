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
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
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
	// secretVolumeUpdates holds updates to existing Secret volume references
	secretVolumeUpdates map[string]string
	// argReplacements holds args that replace (not append) base args with the same
	// flag key. Key is the container name, value is the list of replacement args.
	argReplacements map[string][]string
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

// WithSecretVolumeUpdate updates an existing Secret volume's Secret name.
// This modifies the volume in-place during ApplyToPodTemplateSpec, preserving other fields
// like items, defaultMode, and optional.
func WithSecretVolumeUpdate(volumeName, secretName string) PodOverlayOption {
	return func(p *PodOverlay) {
		if p.secretVolumeUpdates == nil {
			p.secretVolumeUpdates = make(map[string]string)
		}
		p.secretVolumeUpdates[volumeName] = secretName
	}
}

// WithArgReplace replaces a base arg that has the same flag key, or appends if no match.
// The flag key is everything up to and including "=" (e.g. "--zap-encoder=" in
// "--zap-encoder=console"). For standalone flags (no "="), the full arg is the key.
//
// This is a PodOverlayOption (not a ContainerOption) because replacements run after
// the regular overlay merge — they can replace both original base args and args
// appended by WithArgs in the same overlay.
// Use this instead of WithArgs when an upstream manifest might already contain the flag.
func WithArgReplace(containerName string, args ...string) PodOverlayOption {
	return func(p *PodOverlay) {
		if p.argReplacements == nil {
			p.argReplacements = make(map[string][]string)
		}
		p.argReplacements[containerName] = append(p.argReplacements[containerName], args...)
	}
}

// WithLeaderElection adds --leader-elect=true if replicas > 1.
// It uses WithArgReplace so that any existing --leader-elect flag in the base
// manifest is replaced rather than duplicated.
func WithLeaderElection(containerName string, replicas int32) PodOverlayOption {
	if replicas <= 1 {
		return func(_ *PodOverlay) {}
	}
	return WithArgReplace(containerName, "--leader-elect=true")
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

	// Apply pod-level customizations using strategic merge.
	// We copy containers from the template to the overlay to prevent them from
	// being deleted by strategic merge (nil serializes as "null" in JSON, which
	// strategic merge interprets as "delete this field"). Container merging is
	// handled separately below with mergeContainerList.
	overlay := p.podSpec
	overlay.Containers = template.Spec.Containers
	overlay.InitContainers = template.Spec.InitContainers
	if err := StrategicMerge(&template.Spec, &overlay); err != nil {
		return err
	}

	// Apply per-container customizations
	if p.containerOverlays != nil || len(p.argReplacements) > 0 {
		if err := mergeContainerList(template.Spec.Containers, p.containerOverlays, p.argReplacements); err != nil {
			return err
		}
		if err := mergeContainerList(template.Spec.InitContainers, p.containerOverlays, p.argReplacements); err != nil {
			return err
		}
	}

	// Apply ConfigMap volume updates
	applyConfigMapVolumeUpdates(template.Spec.Volumes, p.configMapVolumeUpdates)

	// Apply Secret volume updates
	applySecretVolumeUpdates(template.Spec.Volumes, p.secretVolumeUpdates)

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

// ApplyToCronJob applies customizations to a CronJob.
func (p *PodOverlay) ApplyToCronJob(cronJob *batchv1.CronJob) error {
	if cronJob == nil {
		return nil
	}
	return p.ApplyToPodTemplateSpec(&cronJob.Spec.JobTemplate.Spec.Template)
}

// ValidateCronJobContainer checks that a container with the given name exists
// in the CronJob's pod template. Returns an error if the container is not found.
func ValidateCronJobContainer(cj *batchv1.CronJob, containerName string) error {
	for _, c := range cj.Spec.JobTemplate.Spec.Template.Spec.Containers {
		if c.Name == containerName {
			return nil
		}
	}
	return fmt.Errorf(
		"container %q not found in CronJob %s: customizations cannot be applied",
		containerName, cj.Name)
}

// ApplyCronJobContainerSpec applies a ContainerSpec to a CronJob container.
// It validates that the expected container exists, builds an overlay from the
// ContainerSpec, and applies it to the CronJob's pod template.
// Returns nil immediately if spec is nil (no customizations requested).
func ApplyCronJobContainerSpec(
	cj *batchv1.CronJob,
	containerName string,
	spec *konfluxv1alpha1.ContainerSpec,
) error {
	if spec == nil {
		return nil
	}

	if err := ValidateCronJobContainer(cj, containerName); err != nil {
		return err
	}

	overlay := BuildPodOverlay(
		DeploymentContext{},
		WithContainerBuilder(containerName, FromContainerSpec(spec)),
	)
	return overlay.ApplyToCronJob(cj)
}

// mergeContainerList merges container overlay configurations into a list of containers.
// Container.Args is tagged +listType=atomic in the Kubernetes API, so strategic merge
// replaces the entire args list. We handle args separately: save them before merge,
// then append after merge so overlay args are added to (not replace) the base args.
//
// argReplacements are applied after the regular overlay merge. Each replacement arg
// replaces a base arg with the same flag key, or is appended if no match exists.
func mergeContainerList(
	containers []corev1.Container,
	overlays map[string]*corev1.Container,
	argReplacements map[string][]string,
) error {
	for i := range containers {
		base := &containers[i]
		if overlay := overlays[base.Name]; overlay != nil {
			overlay.Name = base.Name

			extraArgs := overlay.Args
			overlay.Args = nil

			// Extract env vars before strategic merge. Strategic merge merges
			// env vars by name and preserves omitted fields from the base,
			// which means overriding a valueFrom env var with a value (or vice
			// versa) produces an invalid EnvVar with both fields set. By
			// removing env from the overlay before merge and applying it
			// manually afterward, we ensure the entire EnvVar struct is
			// replaced for matching names.
			extraEnv := overlay.Env
			overlay.Env = nil

			if err := StrategicMerge(base, overlay); err != nil {
				overlay.Args = extraArgs
				overlay.Env = extraEnv
				return err
			}

			overlay.Args = extraArgs
			overlay.Env = extraEnv
			base.Args = append(base.Args, extraArgs...)
			mergeEnvByName(base, extraEnv)
		}

		for _, replacement := range argReplacements[base.Name] {
			key := argKey(replacement)
			replaced := false
			for j, baseArg := range base.Args {
				if argKey(baseArg) == key {
					base.Args[j] = replacement
					replaced = true
					break
				}
			}
			if !replaced {
				base.Args = append(base.Args, replacement)
			}
		}
	}
	return nil
}

// mergeEnvByName replaces or appends overlay env vars into a container by name.
// When an overlay env var has the same name as a base env var, the entire EnvVar
// struct is replaced. This prevents invalid combinations of value and valueFrom
// that strategic merge can produce.
func mergeEnvByName(base *corev1.Container, overlay []corev1.EnvVar) {
	for _, ov := range overlay {
		found := false
		for j, baseEnv := range base.Env {
			if baseEnv.Name == ov.Name {
				base.Env[j] = ov
				found = true
				break
			}
		}
		if !found {
			base.Env = append(base.Env, ov)
		}
	}
}

// argKey returns the flag key for deduplication.
// For "--key=value" it returns "--key"; for standalone flags it returns the full arg.
// This ensures "--leader-elect" and "--leader-elect=true" are treated as the same key.
// Space-separated args (e.g. "--flag" "value" as two elements) are not handled;
// controller-runtime uses --flag=value syntax exclusively.
func argKey(arg string) string {
	if idx := strings.Index(arg, "="); idx >= 0 {
		return arg[:idx]
	}
	return arg
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

// applySecretVolumeUpdates updates Secret volume references in-place.
func applySecretVolumeUpdates(volumes []corev1.Volume, updates map[string]string) {
	if updates == nil {
		return
	}
	for i := range volumes {
		vol := &volumes[i]
		if secretName, ok := updates[vol.Name]; ok && vol.Secret != nil {
			vol.Secret.SecretName = secretName
		}
	}
}
