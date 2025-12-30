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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewPodOverlay(t *testing.T) {
	t.Run("creates empty overlay", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay()
		g.Expect(p).NotTo(gomega.BeNil())
		g.Expect(p.containerOverlays).NotTo(gomega.BeNil())
	})

	t.Run("applies options", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay(
			WithServiceAccountName("test-sa"),
			WithPriorityClassName("high-priority"),
		)
		g.Expect(p.podSpec.ServiceAccountName).To(gomega.Equal("test-sa"))
		g.Expect(p.podSpec.PriorityClassName).To(gomega.Equal("high-priority"))
	})
}

func TestWithContainer(t *testing.T) {
	t.Run("adds container overlay", func(t *testing.T) {
		g := gomega.NewWithT(t)
		container := &corev1.Container{
			Image: "nginx:latest",
		}
		p := NewPodOverlay(WithContainer("nginx", container))

		g.Expect(p.containerOverlays).To(gomega.HaveKey("nginx"))
		g.Expect(p.containerOverlays["nginx"].Image).To(gomega.Equal("nginx:latest"))
	})

	t.Run("nil overlay is ignored", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay(WithContainer("nginx", nil))
		g.Expect(p.containerOverlays).NotTo(gomega.HaveKey("nginx"))
	})
}

func TestWithContainerOpts(t *testing.T) {
	ctx := DeploymentContext{Replicas: 1}

	t.Run("creates container overlay with options", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay(
			WithContainerOpts("nginx", ctx,
				WithImage("nginx:1.21"),
				WithArgs("--config=/etc/nginx.conf"),
			),
		)

		g.Expect(p.containerOverlays).To(gomega.HaveKey("nginx"))
		g.Expect(p.containerOverlays["nginx"].Image).To(gomega.Equal("nginx:1.21"))
		g.Expect(p.containerOverlays["nginx"].Args).To(gomega.HaveLen(1))
	})
}

func TestWithTolerations(t *testing.T) {
	t.Run("adds tolerations", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay(WithTolerations(
			corev1.Toleration{Key: "key1", Operator: corev1.TolerationOpExists},
			corev1.Toleration{Key: "key2", Value: "value2", Effect: corev1.TaintEffectNoSchedule},
		))

		g.Expect(p.podSpec.Tolerations).To(gomega.HaveLen(2))
		g.Expect(p.podSpec.Tolerations[0].Key).To(gomega.Equal("key1"))
	})

	t.Run("appends tolerations", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay(
			WithTolerations(corev1.Toleration{Key: "key1"}),
			WithTolerations(corev1.Toleration{Key: "key2"}),
		)
		g.Expect(p.podSpec.Tolerations).To(gomega.HaveLen(2))
	})
}

func TestWithNodeSelector(t *testing.T) {
	t.Run("sets node selector", func(t *testing.T) {
		g := gomega.NewWithT(t)
		selector := map[string]string{
			"kubernetes.io/os": "linux",
			"node-type":        "compute",
		}
		p := NewPodOverlay(WithNodeSelector(selector))

		g.Expect(p.podSpec.NodeSelector).To(gomega.HaveLen(2))
		g.Expect(p.podSpec.NodeSelector["kubernetes.io/os"]).To(gomega.Equal("linux"))
	})
}

func TestWithAffinity(t *testing.T) {
	t.Run("sets affinity", func(t *testing.T) {
		g := gomega.NewWithT(t)
		affinity := &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "topology.kubernetes.io/zone",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"us-east-1a"},
								},
							},
						},
					},
				},
			},
		}
		p := NewPodOverlay(WithAffinity(affinity))

		g.Expect(p.podSpec.Affinity).NotTo(gomega.BeNil())
		g.Expect(p.podSpec.Affinity.NodeAffinity).NotTo(gomega.BeNil())
	})
}

func TestWithVolumes(t *testing.T) {
	t.Run("adds volumes", func(t *testing.T) {
		g := gomega.NewWithT(t)
		configVol := corev1.Volume{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-config"},
				},
			},
		}
		secretVol := corev1.Volume{
			Name: "secret",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: "my-secret"},
			},
		}
		p := NewPodOverlay(WithVolumes(configVol, secretVol))

		g.Expect(p.podSpec.Volumes).To(gomega.HaveLen(2))
		g.Expect(p.podSpec.Volumes[0].Name).To(gomega.Equal("config"))
	})
}

func TestWithServiceAccountName(t *testing.T) {
	t.Run("sets service account name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay(WithServiceAccountName("my-sa"))
		g.Expect(p.podSpec.ServiceAccountName).To(gomega.Equal("my-sa"))
	})
}

func TestWithPodSecurityContext(t *testing.T) {
	t.Run("sets pod security context", func(t *testing.T) {
		g := gomega.NewWithT(t)
		runAsUser := int64(1000)
		fsGroup := int64(2000)
		sc := &corev1.PodSecurityContext{
			RunAsUser: &runAsUser,
			FSGroup:   &fsGroup,
		}
		p := NewPodOverlay(WithPodSecurityContext(sc))

		g.Expect(p.podSpec.SecurityContext).NotTo(gomega.BeNil())
		g.Expect(*p.podSpec.SecurityContext.RunAsUser).To(gomega.Equal(int64(1000)))
		g.Expect(*p.podSpec.SecurityContext.FSGroup).To(gomega.Equal(int64(2000)))
	})
}

func TestWithPriorityClassName(t *testing.T) {
	t.Run("sets priority class name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay(WithPriorityClassName("system-cluster-critical"))
		g.Expect(p.podSpec.PriorityClassName).To(gomega.Equal("system-cluster-critical"))
	})
}

func TestWithTopologySpreadConstraints(t *testing.T) {
	t.Run("adds topology spread constraints", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay(WithTopologySpreadConstraints(
			corev1.TopologySpreadConstraint{
				MaxSkew:           1,
				TopologyKey:       "topology.kubernetes.io/zone",
				WhenUnsatisfiable: corev1.DoNotSchedule,
			},
		))

		g.Expect(p.podSpec.TopologySpreadConstraints).To(gomega.HaveLen(1))
		g.Expect(p.podSpec.TopologySpreadConstraints[0].TopologyKey).To(gomega.Equal("topology.kubernetes.io/zone"))
	})
}

func TestApplyToPodTemplateSpec(t *testing.T) {
	t.Run("nil overlay is safe", func(t *testing.T) {
		g := gomega.NewWithT(t)
		var p *PodOverlay
		template := &corev1.PodTemplateSpec{}
		err := p.ApplyToPodTemplateSpec(template) // Should not panic
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})

	t.Run("nil template is safe", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay()
		err := p.ApplyToPodTemplateSpec(nil) // Should not panic
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})

	t.Run("applies pod-level customizations", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay(
			WithServiceAccountName("custom-sa"),
			WithNodeSelector(map[string]string{"node": "worker"}),
		)

		template := &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				ServiceAccountName: "default-sa",
			},
		}

		err := p.ApplyToPodTemplateSpec(template)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(template.Spec.ServiceAccountName).To(gomega.Equal("custom-sa"))
		g.Expect(template.Spec.NodeSelector["node"]).To(gomega.Equal("worker"))
	})

	t.Run("applies container customizations by name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := DeploymentContext{Replicas: 1}
		p := NewPodOverlay(
			WithContainerOpts("nginx", ctx,
				WithResources(corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
					},
				}),
			),
		)

		template := &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "nginx", Image: "nginx:1.20"},
					{Name: "sidecar", Image: "sidecar:latest"},
				},
			},
		}

		err := p.ApplyToPodTemplateSpec(template)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// nginx should have resources set
		g.Expect(template.Spec.Containers[0].Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		// sidecar should be unchanged
		g.Expect(template.Spec.Containers[1].Resources.Limits).To(gomega.BeNil())
	})

	t.Run("applies to init containers", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := DeploymentContext{Replicas: 1}
		p := NewPodOverlay(
			WithContainerOpts("init-db", ctx,
				WithImage("init:v2"),
			),
		)

		template := &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{Name: "init-db", Image: "init:v1"},
				},
				Containers: []corev1.Container{
					{Name: "app", Image: "app:v1"},
				},
			},
		}

		err := p.ApplyToPodTemplateSpec(template)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(template.Spec.InitContainers[0].Image).To(gomega.Equal("init:v2"))
	})

	t.Run("non-matching container names are ignored", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := DeploymentContext{Replicas: 1}
		p := NewPodOverlay(
			WithContainerOpts("nonexistent", ctx,
				WithImage("new:image"),
			),
		)

		template := &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "app", Image: "app:v1"},
				},
			},
		}

		err := p.ApplyToPodTemplateSpec(template)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// app should be unchanged
		g.Expect(template.Spec.Containers[0].Image).To(gomega.Equal("app:v1"))
	})
}

func TestApplyToWorkloads(t *testing.T) {
	t.Run("nil workloads are safe", func(t *testing.T) {
		g := gomega.NewWithT(t)
		p := NewPodOverlay()
		// These should not panic
		g.Expect(p.ApplyToDeployment(nil)).NotTo(gomega.HaveOccurred())
		g.Expect(p.ApplyToStatefulSet(nil)).NotTo(gomega.HaveOccurred())
		g.Expect(p.ApplyToDaemonSet(nil)).NotTo(gomega.HaveOccurred())
	})

	t.Run("applies customizations to deployment", func(t *testing.T) { //nolint:dupl
		g := gomega.NewWithT(t)
		ctx := DeploymentContext{Replicas: 1}
		p := NewPodOverlay(
			WithServiceAccountName("custom-sa"),
			WithContainerOpts("app", ctx, WithImage("app:v2")),
		)

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-deploy"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: "default",
						Containers:         []corev1.Container{{Name: "app", Image: "app:v1"}},
					},
				},
			},
		}

		err := p.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(gomega.Equal("custom-sa"))
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(gomega.Equal("app:v2"))
	})

	t.Run("applies customizations to statefulset", func(t *testing.T) { //nolint:dupl
		g := gomega.NewWithT(t)
		ctx := DeploymentContext{Replicas: 1}
		p := NewPodOverlay(
			WithServiceAccountName("custom-sa"),
			WithContainerOpts("db", ctx, WithImage("postgres:14")),
		)

		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-sts"},
			Spec: appsv1.StatefulSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: "default",
						Containers:         []corev1.Container{{Name: "db", Image: "postgres:13"}},
					},
				},
			},
		}

		err := p.ApplyToStatefulSet(sts)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(sts.Spec.Template.Spec.ServiceAccountName).To(gomega.Equal("custom-sa"))
		g.Expect(sts.Spec.Template.Spec.Containers[0].Image).To(gomega.Equal("postgres:14"))
	})

	t.Run("applies customizations to daemonset", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := DeploymentContext{Replicas: 1}
		toleration := corev1.Toleration{
			Key:    "node-role.kubernetes.io/master",
			Effect: corev1.TaintEffectNoSchedule,
		}
		p := NewPodOverlay(
			WithTolerations(toleration),
			WithContainerOpts("agent", ctx, WithImage("agent:v2")),
		)

		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-ds"},
			Spec: appsv1.DaemonSetSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "agent", Image: "agent:v1"}},
					},
				},
			},
		}

		err := p.ApplyToDaemonSet(ds)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(ds.Spec.Template.Spec.Tolerations).To(gomega.HaveLen(1))
		g.Expect(ds.Spec.Template.Spec.Containers[0].Image).To(gomega.Equal("agent:v2"))
	})
}

func TestComplexScenario(t *testing.T) {
	t.Run("realistic deployment customization", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := DeploymentContext{Replicas: 3}

		p := NewPodOverlay(
			// Pod-level customizations
			WithServiceAccountName("my-app-sa"),
			WithPriorityClassName("high-priority"),
			WithNodeSelector(map[string]string{
				"kubernetes.io/os":                 "linux",
				"node.kubernetes.io/instance-type": "m5.large",
			}),
			WithTolerations(
				corev1.Toleration{Key: "dedicated", Value: "my-app", Effect: corev1.TaintEffectNoSchedule},
			),
			WithTopologySpreadConstraints(
				corev1.TopologySpreadConstraint{
					MaxSkew:           1,
					TopologyKey:       "topology.kubernetes.io/zone",
					WhenUnsatisfiable: corev1.ScheduleAnyway,
				},
			),
			WithVolumes(
				corev1.Volume{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"},
						},
					},
				},
			),
			// Container customizations
			WithContainerOpts("app", ctx,
				WithResources(corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				}),
				WithEnv(corev1.EnvVar{Name: "LOG_LEVEL", Value: "info"}),
				WithLeaderElection(),
			),
			WithContainerOpts("sidecar", ctx,
				WithResources(corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				}),
			),
		)

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: "default",
						Containers: []corev1.Container{
							{Name: "app", Image: "my-app:v1"},
							{Name: "sidecar", Image: "envoy:v1.20"},
						},
					},
				},
			},
		}

		err := p.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		spec := deployment.Spec.Template.Spec

		// Verify pod-level customizations
		g.Expect(spec.ServiceAccountName).To(gomega.Equal("my-app-sa"))
		g.Expect(spec.PriorityClassName).To(gomega.Equal("high-priority"))
		g.Expect(spec.NodeSelector).To(gomega.HaveLen(2))
		g.Expect(spec.Tolerations).To(gomega.HaveLen(1))
		g.Expect(spec.TopologySpreadConstraints).To(gomega.HaveLen(1))
		g.Expect(spec.Volumes).To(gomega.HaveLen(1))

		// Verify app container customizations
		appContainer := spec.Containers[0]
		g.Expect(appContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("2"))
		g.Expect(appContainer.Env).To(gomega.HaveLen(1))
		g.Expect(appContainer.Env[0].Name).To(gomega.Equal("LOG_LEVEL"))
		// Should have leader election arg due to replicas > 1
		g.Expect(appContainer.Args).To(gomega.ContainElement("--leader-elect=true"))

		// Verify sidecar container customizations
		sidecarContainer := spec.Containers[1]
		g.Expect(sidecarContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("100m"))
	})
}

// TestApplyToPodTemplateSpec_ContainerDeletionBug tests for a regression where
// applying pod-level customizations (like ServiceAccountName) would accidentally
// delete all containers from the deployment.
//
// The bug was caused by the overlay PodSpec having Containers: nil, which
// serializes to "containers": null in JSON. Strategic merge interprets null
// as "delete this field", so the containers would be removed.
//
// The fix preserves containers by copying them from the template to the overlay
// before doing the strategic merge.
func TestApplyToPodTemplateSpec_ContainerDeletionBug(t *testing.T) {
	t.Run("pod-level customizations preserve containers", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create overlay with only pod-level customizations (no container overlays)
		p := NewPodOverlay(
			WithServiceAccountName("custom-sa"),
		)

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-app"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: "default",
						Containers: []corev1.Container{
							{Name: "app", Image: "app:v1"},
							{Name: "sidecar", Image: "sidecar:v1"},
						},
						InitContainers: []corev1.Container{
							{Name: "init", Image: "init:v1"},
						},
					},
				},
			},
		}

		err := p.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		spec := deployment.Spec.Template.Spec

		// Pod-level customization should be applied
		g.Expect(spec.ServiceAccountName).To(gomega.Equal("custom-sa"))

		// CRITICAL: Containers should NOT be deleted
		// This was the bug: containers would be nil after applying pod-level customizations
		g.Expect(spec.Containers).To(gomega.HaveLen(2), "containers should not be deleted")
		g.Expect(spec.Containers[0].Name).To(gomega.Equal("app"))
		g.Expect(spec.Containers[0].Image).To(gomega.Equal("app:v1"))
		g.Expect(spec.Containers[1].Name).To(gomega.Equal("sidecar"))
		g.Expect(spec.Containers[1].Image).To(gomega.Equal("sidecar:v1"))

		// InitContainers should also NOT be deleted
		g.Expect(spec.InitContainers).To(gomega.HaveLen(1), "initContainers should not be deleted")
		g.Expect(spec.InitContainers[0].Name).To(gomega.Equal("init"))
	})

	t.Run("no pod-level customizations preserve containers", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create overlay with no pod-level customizations at all
		// (only container overlays)
		ctx := DeploymentContext{Replicas: 1}
		p := NewPodOverlay(
			WithContainerOpts("app", ctx, WithImage("app:v2")),
		)

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-app"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "app", Image: "app:v1"},
						},
					},
				},
			},
		}

		err := p.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		spec := deployment.Spec.Template.Spec

		// Container should be updated, not deleted
		g.Expect(spec.Containers).To(gomega.HaveLen(1))
		g.Expect(spec.Containers[0].Name).To(gomega.Equal("app"))
		g.Expect(spec.Containers[0].Image).To(gomega.Equal("app:v2"))
	})

	t.Run("combined pod and container customizations work together", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create overlay with both pod-level and container-level customizations
		ctx := DeploymentContext{Replicas: 1}
		p := NewPodOverlay(
			WithServiceAccountName("custom-sa"),
			WithTolerations(corev1.Toleration{Key: "special", Operator: corev1.TolerationOpExists}),
			WithContainerOpts("app", ctx,
				WithImage("app:v2"),
				WithEnv(corev1.EnvVar{Name: "NEW_VAR", Value: "new-value"}),
			),
		)

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-app"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: "default",
						Containers: []corev1.Container{
							{
								Name:  "app",
								Image: "app:v1",
								Env:   []corev1.EnvVar{{Name: "EXISTING", Value: "existing"}},
							},
							{Name: "sidecar", Image: "sidecar:v1"},
						},
					},
				},
			},
		}

		err := p.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		spec := deployment.Spec.Template.Spec

		// Pod-level customizations applied
		g.Expect(spec.ServiceAccountName).To(gomega.Equal("custom-sa"))
		g.Expect(spec.Tolerations).To(gomega.HaveLen(1))

		// Containers preserved
		g.Expect(spec.Containers).To(gomega.HaveLen(2))

		// App container customizations applied
		appContainer := spec.Containers[0]
		g.Expect(appContainer.Image).To(gomega.Equal("app:v2"))
		// Should have both existing and new env vars
		g.Expect(appContainer.Env).To(gomega.HaveLen(2))

		// Sidecar unchanged
		g.Expect(spec.Containers[1].Image).To(gomega.Equal("sidecar:v1"))
	})
}
