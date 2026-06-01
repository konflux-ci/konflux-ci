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

package imagecontroller

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

func getImageControllerDeployment(t *testing.T) *appsv1.Deployment {
	t.Helper()
	store := testutil.GetTestObjectStore(t)
	objects, err := store.GetForComponent(manifests.ImageController)
	if err != nil {
		t.Fatalf("failed to get ImageController manifests: %v", err)
	}

	for _, obj := range objects {
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if deployment.Name == controllerManagerDeploymentName {
				return deployment
			}
		}
	}
	t.Fatalf("deployment %q not found in ImageController manifests", controllerManagerDeploymentName)
	return nil
}

func findQuayCAEnvVar(envVars []corev1.EnvVar) *corev1.EnvVar {
	for i := range envVars {
		if envVars[i].Name == quayAdditionalCAEnvVar {
			return &envVars[i]
		}
	}
	return nil
}

func findVolume(volumes []corev1.Volume, name string) *corev1.Volume {
	for i := range volumes {
		if volumes[i].Name == name {
			return &volumes[i]
		}
	}
	return nil
}

func TestBuildImageControllerManagerOverlay(t *testing.T) {
	t.Run("empty spec returns overlay", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{}
		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(overlay).NotTo(gomega.BeNil())
	})

	t.Run("nil QuayCABundle and nil ImageControllerManager leaves deployment unchanged", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{}
		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(overlay).NotTo(gomega.BeNil())

		deployment := getImageControllerDeployment(t)
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(findQuayCAEnvVar(managerContainer.Env)).To(gomega.BeNil())
	})

	t.Run("manager resources are applied", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			ImageControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("6Gi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("250m"),
							corev1.ResourceMemory: resource.MustParse("3Gi"),
						},
					},
				},
			},
		}

		deployment := getImageControllerDeployment(t)
		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		g.Expect(managerContainer.Resources.Limits.Memory().String()).To(gomega.Equal("6Gi"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("250m"))
		g.Expect(managerContainer.Resources.Requests.Memory().String()).To(gomega.Equal("3Gi"))
	})

	t.Run("manager env vars are injected", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			ImageControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: "CUSTOM_VAR", Value: "custom-value"},
					},
				},
			},
		}

		deployment := getImageControllerDeployment(t)
		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		var found bool
		for _, env := range managerContainer.Env {
			if env.Name == "CUSTOM_VAR" {
				g.Expect(env.Value).To(gomega.Equal("custom-value"))
				found = true
			}
		}
		g.Expect(found).To(gomega.BeTrue())
	})

	t.Run("preserves existing container fields when resources are set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			ImageControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				},
			},
		}

		deployment := getImageControllerDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		originalImage := managerContainer.Image

		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Image).To(gomega.Equal(originalImage))
	})

	t.Run("single replica keeps leader-elect disabled", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			ImageControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Replicas: 1,
			},
		}

		deployment := getImageControllerDeployment(t)
		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Args).To(gomega.ContainElement("--leader-elect=false"))
	})

	t.Run("multiple replicas enables leader-elect", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			ImageControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Replicas: 3,
			},
		}

		deployment := getImageControllerDeployment(t)
		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Args).To(gomega.ContainElement("--leader-elect=true"))
	})

	t.Run("QuayCABundle and ImageControllerManager combined", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "my-custom-ca",
				Key:           "ca.crt",
			},
			ImageControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				},
			},
		}

		deployment := getImageControllerDeployment(t)
		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		envVar := findQuayCAEnvVar(managerContainer.Env)
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal("/etc/ssl/certs/quay-ca/ca.crt"))

		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))

		vol := findVolume(deployment.Spec.Template.Spec.Volumes, quayCABundleVolumeName)
		g.Expect(vol).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap.Name).To(gomega.Equal("my-custom-ca"))
	})

	t.Run("sets QUAY_ADDITIONAL_CA env var when QuayCABundle is configured", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "quay-ca.crt",
			},
		}

		deployment := getImageControllerDeployment(t)
		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		envVar := findQuayCAEnvVar(managerContainer.Env)
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal("/etc/ssl/certs/quay-ca/quay-ca.crt"))
	})

	t.Run("updates ConfigMap name when different from default", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "my-custom-ca",
				Key:           "ca.crt",
			},
		}

		deployment := getImageControllerDeployment(t)
		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		vol := findVolume(deployment.Spec.Template.Spec.Volumes, quayCABundleVolumeName)
		g.Expect(vol).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap.Name).To(gomega.Equal("my-custom-ca"))
	})

	t.Run("preserves default ConfigMap name when matching", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: defaultQuayCAConfigMapName,
				Key:           "ca.crt",
			},
		}

		deployment := getImageControllerDeployment(t)
		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		vol := findVolume(deployment.Spec.Template.Spec.Volumes, quayCABundleVolumeName)
		g.Expect(vol).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap.Name).To(gomega.Equal(defaultQuayCAConfigMapName))
	})

	t.Run("preserves existing volumes and mounts", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "quay-ca.crt",
			},
		}

		deployment := getImageControllerDeployment(t)
		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// quaytoken volume should still exist
		vol := findVolume(deployment.Spec.Template.Spec.Volumes, "quaytoken")
		g.Expect(vol).NotTo(gomega.BeNil())
		g.Expect(vol.Secret).NotTo(gomega.BeNil())
		g.Expect(vol.Secret.SecretName).To(gomega.Equal("quaytoken"))

		// quaytoken mount should still exist on the manager container
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		var quaytokenMount *corev1.VolumeMount
		for i := range managerContainer.VolumeMounts {
			if managerContainer.VolumeMounts[i].Name == "quaytoken" {
				quaytokenMount = &managerContainer.VolumeMounts[i]
				break
			}
		}
		g.Expect(quaytokenMount).NotTo(gomega.BeNil())
		g.Expect(quaytokenMount.MountPath).To(gomega.Equal("/workspace"))
	})

	t.Run("preserves existing container fields", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "quay-ca.crt",
			},
		}

		deployment := getImageControllerDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		originalImage := managerContainer.Image

		overlay, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Image).To(gomega.Equal(originalImage))
	})

	t.Run("rejects key with path traversal", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "../etc/passwd",
			},
		}

		_, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("invalid CA bundle key"))
	})

	t.Run("rejects key with absolute path", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "/etc/passwd",
			},
		}

		_, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("invalid CA bundle key"))
	})

	t.Run("rejects key with embedded path separator", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "subdir/ca.crt",
			},
		}

		_, err := buildImageControllerManagerOverlay(spec)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("invalid CA bundle key"))
	})
}

func TestApplyImageControllerDeploymentCustomizations(t *testing.T) {
	t.Run("applies customizations to controller-manager deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "quay-ca.crt",
			},
		}

		deployment := getImageControllerDeployment(t)
		err := applyImageControllerDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		envVar := findQuayCAEnvVar(managerContainer.Env)
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal("/etc/ssl/certs/quay-ca/quay-ca.crt"))
	})

	t.Run("ignores unknown deployment names", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			QuayCABundle: &konfluxv1alpha1.QuayCABundleSpec{
				ConfigMapName: "quay-ca-bundle",
				Key:           "quay-ca.crt",
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

		err := applyImageControllerDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(gomega.BeEmpty())
	})

	t.Run("handles empty spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{}

		deployment := getImageControllerDeployment(t)
		err := applyImageControllerDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(findQuayCAEnvVar(managerContainer.Env)).To(gomega.BeNil())
	})

	t.Run("applies replicas to controller-manager deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			ImageControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Replicas: 3,
			},
		}

		deployment := getImageControllerDeployment(t)
		err := applyImageControllerDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(3)))
	})

	t.Run("applies default replicas when using default value", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			ImageControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Replicas: 1,
			},
		}

		deployment := getImageControllerDeployment(t)
		err := applyImageControllerDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(1)))
	})

	t.Run("does not modify replicas when ImageControllerManager is nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			ImageControllerManager: nil,
		}

		deployment := getImageControllerDeployment(t)
		originalReplicas := deployment.Spec.Replicas
		err := applyImageControllerDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).To(gomega.Equal(originalReplicas))
	})

	t.Run("applies replicas together with container resources", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxImageControllerSpec{
			ImageControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
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

		deployment := getImageControllerDeployment(t)
		err := applyImageControllerDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(5)))

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("2"))
	})
}
