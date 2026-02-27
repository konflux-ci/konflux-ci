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

package buildservice

import (
	"testing"

	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

// getBuildServiceDeployment returns a deep copy of the BuildService controller-manager deployment from the manifests.
func getBuildServiceDeployment(t *testing.T) *appsv1.Deployment {
	t.Helper()
	store := testutil.GetTestObjectStore(t)
	objects, err := store.GetForComponent(manifests.BuildService)
	if err != nil {
		t.Fatalf("failed to get BuildService manifests: %v", err)
	}

	for _, obj := range objects {
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if deployment.Name == buildControllerManagerDeploymentName {
				return deployment
			}
		}
	}
	t.Fatalf("deployment %q not found in BuildService manifests", buildControllerManagerDeploymentName)
	return nil
}

func TestBuildBuildControllerManagerOverlay(t *testing.T) {
	t.Run("nil spec returns empty overlay", func(t *testing.T) {
		g := gomega.NewWithT(t)
		overlay := buildBuildControllerManagerOverlay(nil)
		g.Expect(overlay).NotTo(gomega.BeNil())
	})

	t.Run("empty spec returns overlay without customizations", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{}
		overlay := buildBuildControllerManagerOverlay(spec)
		g.Expect(overlay).NotTo(gomega.BeNil())
	})

	t.Run("manager resources are applied", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{
			Manager: &konfluxv1alpha1.ContainerSpec{
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
			},
		}

		deployment := getBuildServiceDeployment(t)
		overlay := buildBuildControllerManagerOverlay(spec)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		g.Expect(managerContainer.Resources.Limits.Memory().String()).To(gomega.Equal("256Mi"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("100m"))
		g.Expect(managerContainer.Resources.Requests.Memory().String()).To(gomega.Equal("128Mi"))
	})

	t.Run("preserves existing container fields", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{
			Manager: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
					},
				},
			},
		}

		deployment := getBuildServiceDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist in controller-manager deployment")
		originalImage := managerContainer.Image

		overlay := buildBuildControllerManagerOverlay(spec)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Image).To(gomega.Equal(originalImage))
	})
}

func TestApplyBuildServiceDeploymentCustomizations(t *testing.T) {
	t.Run("applies customizations to controller-manager deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
				},
			},
		}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
	})

	t.Run("ignores unknown deployment names", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
				},
			},
		}

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "unknown-deployment"},
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

		err := applyBuildServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic and container should be unchanged
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(gomega.BeNil())
	})

	t.Run("handles nil controller-manager spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: nil,
		}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
	})

	t.Run("handles empty spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		g.Expect(deployment.Spec.Template.Spec.Containers).NotTo(gomega.BeEmpty())
	})

	t.Run("applies replicas to controller-manager deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Replicas: 3,
			},
		}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(3)))
	})

	t.Run("applies default replicas when using default value", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Replicas: 1, // default value
			},
		}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(1)))
	})

	t.Run("does not modify replicas when controller-manager spec is nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: nil,
		}

		deployment := getBuildServiceDeployment(t)
		originalReplicas := deployment.Spec.Replicas
		err := applyBuildServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).To(gomega.Equal(originalReplicas))
	})

	t.Run("applies replicas together with container resources", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Replicas: 5,
				Manager: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("2"),
						},
					},
				},
			},
		}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Check replicas
		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(5)))

		// Check container resources
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("2"))
	})
}

func TestApplyBuildServiceDeploymentCustomizations_ResourceMerging(t *testing.T) {
	t.Run("merges limits without affecting requests", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Get deployment and set existing requests
		deployment := getBuildServiceDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist")
		managerContainer.Resources.Requests = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		}

		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				},
			},
		}

		err := applyBuildServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("50m"))
		g.Expect(managerContainer.Resources.Requests.Memory().String()).To(gomega.Equal("64Mi"))
	})

	t.Run("merges requests without affecting limits", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Get deployment and set existing limits
		deployment := getBuildServiceDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist")
		managerContainer.Resources.Limits = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}

		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
					},
				},
			},
		}

		err := applyBuildServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
		g.Expect(managerContainer.Resources.Limits.Memory().String()).To(gomega.Equal("512Mi"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("100m"))
	})
}

func makeConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		Data: map[string]string{
			"config.yaml": defaultConfigYAML,
		},
	}
}

const defaultConfigYAML = `default-pipeline-name: docker-build-oci-ta
pipelines:
- name: fbc-builder
  bundle: quay.io/konflux-ci/tekton-catalog/pipeline-fbc-builder@sha256:abc123
- name: docker-build-oci-ta
  bundle: quay.io/konflux-ci/tekton-catalog/pipeline-docker-build-oci-ta@sha256:def456
`

func TestApplyPipelineConfigMerge(t *testing.T) {
	t.Run("nil pipelineConfig leaves defaults unchanged", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.Equal(defaultConfigYAML))
	})

	t.Run("empty pipelineConfig leaves defaults unchanged", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("docker-build-oci-ta"))
	})

	t.Run("removeDefaults with custom pipelines only", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			RemoveDefaults: true,
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-custom", Bundle: "quay.io/myorg/pipeline:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("- name: fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("- name: docker-build-oci-ta"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("my-custom"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("quay.io/myorg/pipeline:latest"))
	})

	t.Run("removeDefaults with no pipelines yields empty list", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			RemoveDefaults: true,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("pipelines: []"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name"))
	})

	t.Run("individual pipeline removal via removed: true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "fbc-builder", Removed: true},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("docker-build-oci-ta"))
	})

	t.Run("removing nonexistent pipeline is a no-op", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "nonexistent", Removed: true},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("docker-build-oci-ta"))
	})

	t.Run("pipeline override replaces bundle", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "fbc-builder", Bundle: "quay.io/myorg/fbc:v2"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("quay.io/myorg/fbc:v2"))
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("sha256:abc123"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("docker-build-oci-ta"))
	})

	t.Run("adding new pipelines alongside defaults", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-custom", Bundle: "quay.io/myorg/pipeline:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("docker-build-oci-ta"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("my-custom"))
	})

	t.Run("preserves default-pipeline-name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "fbc-builder", Removed: true},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: docker-build-oci-ta"))
	})

	t.Run("missing config.yaml key returns error", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{Data: map[string]string{}}
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{})
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("missing config.yaml"))
	})

	t.Run("defaultPipelineName override to existing default pipeline", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			DefaultPipelineName: "fbc-builder",
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("- name: fbc-builder"))
	})

	t.Run("defaultPipelineName override to custom pipeline", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			DefaultPipelineName: "my-custom",
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-custom", Bundle: "quay.io/myorg/custom:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: my-custom"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("- name: my-custom"))
	})

	t.Run("defaultPipelineName with removeDefaults: true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			RemoveDefaults:      true,
			DefaultPipelineName: "my-only-pipeline",
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-only-pipeline", Bundle: "quay.io/myorg/custom:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: my-only-pipeline"))
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("docker-build-oci-ta"))
	})

	t.Run("error when defaultPipelineName doesn't exist", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			DefaultPipelineName: "nonexistent-pipeline",
		})
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("nonexistent-pipeline"))
		g.Expect(err.Error()).To(gomega.ContainSubstring("not found"))
	})

	t.Run("error when defaultPipelineName references a removed pipeline", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			DefaultPipelineName: "fbc-builder",
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "fbc-builder", Removed: true},
			},
		})
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("fbc-builder"))
		g.Expect(err.Error()).To(gomega.ContainSubstring("not found"))
	})

	t.Run("preserves existing default when defaultPipelineName field not set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-custom", Bundle: "quay.io/myorg/custom:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: docker-build-oci-ta"))
	})

	t.Run("treats empty string defaultPipelineName as not set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(cm, &konfluxv1alpha1.PipelineConfigSpec{
			DefaultPipelineName: "",
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-custom", Bundle: "quay.io/myorg/custom:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: docker-build-oci-ta"))
	})
}

func TestMergePipelines(t *testing.T) {
	defaults := []pipelineEntryYAML{
		{Name: "pipeline-a", Bundle: "bundle-a"},
		{Name: "pipeline-b", Bundle: "bundle-b"},
		{Name: "pipeline-c", Bundle: "bundle-c"},
	}

	t.Run("does not modify defaults slice", func(t *testing.T) {
		g := gomega.NewWithT(t)
		originalLen := len(defaults)
		_ = mergePipelines(defaults, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "pipeline-a", Removed: true},
			},
		})
		g.Expect(defaults).To(gomega.HaveLen(originalLen))
	})

	t.Run("removeDefaults ignores defaults", func(t *testing.T) {
		g := gomega.NewWithT(t)
		result := mergePipelines(defaults, &konfluxv1alpha1.PipelineConfigSpec{
			RemoveDefaults: true,
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "custom", Bundle: "custom-bundle"},
			},
		})
		g.Expect(result).To(gomega.HaveLen(1))
		g.Expect(result[0].Name).To(gomega.Equal("custom"))
	})

	t.Run("upsert replaces by name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		result := mergePipelines(defaults, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "pipeline-b", Bundle: "new-bundle-b"},
			},
		})
		g.Expect(result).To(gomega.HaveLen(3))
		for _, p := range result {
			if p.Name == "pipeline-b" {
				g.Expect(p.Bundle).To(gomega.Equal("new-bundle-b"))
			}
		}
	})

	t.Run("remove then add same name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		result := mergePipelines(defaults, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "pipeline-a", Removed: true},
				{Name: "pipeline-a", Bundle: "replacement"},
			},
		})
		g.Expect(result).To(gomega.HaveLen(3))
		found := false
		for _, p := range result {
			if p.Name == "pipeline-a" {
				g.Expect(p.Bundle).To(gomega.Equal("replacement"))
				found = true
			}
		}
		g.Expect(found).To(gomega.BeTrue())
	})
}

func TestValidateDefaultPipeline(t *testing.T) {
	t.Run("returns nil when default pipeline name is empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cfg := &pipelineConfigYAML{
			DefaultPipelineName: "",
			Pipelines: []pipelineEntryYAML{
				{Name: "pipeline-a", Bundle: "bundle-a"},
			},
		}
		err := validateDefaultPipeline(cfg)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})

	t.Run("returns nil when default pipeline exists in pipelines list", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cfg := &pipelineConfigYAML{
			DefaultPipelineName: "pipeline-b",
			Pipelines: []pipelineEntryYAML{
				{Name: "pipeline-a", Bundle: "bundle-a"},
				{Name: "pipeline-b", Bundle: "bundle-b"},
				{Name: "pipeline-c", Bundle: "bundle-c"},
			},
		}
		err := validateDefaultPipeline(cfg)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})

	t.Run("returns error when default pipeline not found in pipelines list", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cfg := &pipelineConfigYAML{
			DefaultPipelineName: "nonexistent-pipeline",
			Pipelines: []pipelineEntryYAML{
				{Name: "pipeline-a", Bundle: "bundle-a"},
				{Name: "pipeline-b", Bundle: "bundle-b"},
			},
		}
		err := validateDefaultPipeline(cfg)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("nonexistent-pipeline"))
		g.Expect(err.Error()).To(gomega.ContainSubstring("pipeline-a"))
		g.Expect(err.Error()).To(gomega.ContainSubstring("pipeline-b"))
	})

	t.Run("returns error when default pipeline set but no pipelines available", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cfg := &pipelineConfigYAML{
			DefaultPipelineName: "some-pipeline",
			Pipelines:           []pipelineEntryYAML{},
		}
		err := validateDefaultPipeline(cfg)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("some-pipeline"))
		g.Expect(err.Error()).To(gomega.ContainSubstring("no pipelines"))
	})

	t.Run("returns nil when default pipeline is first in list", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cfg := &pipelineConfigYAML{
			DefaultPipelineName: "pipeline-a",
			Pipelines: []pipelineEntryYAML{
				{Name: "pipeline-a", Bundle: "bundle-a"},
				{Name: "pipeline-b", Bundle: "bundle-b"},
			},
		}
		err := validateDefaultPipeline(cfg)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})

	t.Run("returns nil when default pipeline is last in list", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cfg := &pipelineConfigYAML{
			DefaultPipelineName: "pipeline-c",
			Pipelines: []pipelineEntryYAML{
				{Name: "pipeline-a", Bundle: "bundle-a"},
				{Name: "pipeline-b", Bundle: "bundle-b"},
				{Name: "pipeline-c", Bundle: "bundle-c"},
			},
		}
		err := validateDefaultPipeline(cfg)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})
}
