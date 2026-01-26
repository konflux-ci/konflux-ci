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

package integrationservice

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

const (
	testConsoleURL         = "https://konflux.example.com"
	testConsoleURLTemplate = "https://konflux.example.com/ns/{{ .Namespace }}/pipelinerun/{{ .PipelineRunName }}"
)

// getIntegrationServiceDeployment returns a deep copy of the IntegrationService controller-manager deployment from the manifests.
func getIntegrationServiceDeployment(t *testing.T) *appsv1.Deployment {
	t.Helper()
	store := testutil.GetTestObjectStore(t)
	objects, err := store.GetForComponent(manifests.Integration)
	if err != nil {
		t.Fatalf("failed to get IntegrationService manifests: %v", err)
	}

	for _, obj := range objects {
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if deployment.Name == controllerManagerDeploymentName {
				return deployment
			}
		}
	}
	t.Fatalf("deployment %q not found in IntegrationService manifests", controllerManagerDeploymentName)
	return nil
}

func TestBuildControllerManagerOverlay(t *testing.T) {
	t.Run("nil spec returns empty overlay", func(t *testing.T) {
		g := gomega.NewWithT(t)
		overlay := buildControllerManagerOverlay(nil, "")
		g.Expect(overlay).NotTo(gomega.BeNil())
	})

	t.Run("empty spec returns overlay without customizations", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{}
		overlay := buildControllerManagerOverlay(spec, "")
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

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, "")
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
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

		deployment := getIntegrationServiceDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist in controller-manager deployment")
		originalImage := managerContainer.Image

		overlay := buildControllerManagerOverlay(spec, "")
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Image).To(gomega.Equal(originalImage))
	})
}

func TestApplyIntegrationServiceDeploymentCustomizations(t *testing.T) {
	t.Run("applies customizations to controller-manager deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceSpec{
			IntegrationControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
				},
			},
		}

		deployment := getIntegrationServiceDeployment(t)
		err := applyIntegrationServiceDeploymentCustomizations(deployment, spec, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
	})

	t.Run("ignores unknown deployment names", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceSpec{
			IntegrationControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
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

		err := applyIntegrationServiceDeploymentCustomizations(deployment, spec, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic and container should be unchanged
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(gomega.BeNil())
	})

	t.Run("handles nil controller-manager spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceSpec{
			IntegrationControllerManager: nil,
		}

		deployment := getIntegrationServiceDeployment(t)
		err := applyIntegrationServiceDeploymentCustomizations(deployment, spec, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
	})

	t.Run("handles empty spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceSpec{}

		deployment := getIntegrationServiceDeployment(t)
		err := applyIntegrationServiceDeploymentCustomizations(deployment, spec, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		g.Expect(deployment.Spec.Template.Spec.Containers).NotTo(gomega.BeEmpty())
	})

	t.Run("applies replicas to controller-manager deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceSpec{
			IntegrationControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Replicas: 3,
			},
		}

		deployment := getIntegrationServiceDeployment(t)
		err := applyIntegrationServiceDeploymentCustomizations(deployment, spec, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(3)))
	})

	t.Run("applies default replicas when using default value", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceSpec{
			IntegrationControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Replicas: 1, // default value
			},
		}

		deployment := getIntegrationServiceDeployment(t)
		err := applyIntegrationServiceDeploymentCustomizations(deployment, spec, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(1)))
	})

	t.Run("does not modify replicas when controller-manager spec is nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceSpec{
			IntegrationControllerManager: nil,
		}

		deployment := getIntegrationServiceDeployment(t)
		originalReplicas := deployment.Spec.Replicas
		err := applyIntegrationServiceDeploymentCustomizations(deployment, spec, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).To(gomega.Equal(originalReplicas))
	})

	t.Run("applies replicas together with container resources", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceSpec{
			IntegrationControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
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

		deployment := getIntegrationServiceDeployment(t)
		err := applyIntegrationServiceDeploymentCustomizations(deployment, spec, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Check replicas
		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(5)))

		// Check container resources
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("2"))
	})
}

func TestApplyIntegrationServiceDeploymentCustomizations_ResourceMerging(t *testing.T) {
	t.Run("merges limits without affecting requests", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Get deployment and set existing requests
		deployment := getIntegrationServiceDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist")
		managerContainer.Resources.Requests = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		}

		spec := konfluxv1alpha1.KonfluxIntegrationServiceSpec{
			IntegrationControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				},
			},
		}

		err := applyIntegrationServiceDeploymentCustomizations(deployment, spec, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("50m"))
		g.Expect(managerContainer.Resources.Requests.Memory().String()).To(gomega.Equal("64Mi"))
	})

	t.Run("merges requests without affecting limits", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Get deployment and set existing limits
		deployment := getIntegrationServiceDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist")
		managerContainer.Resources.Limits = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}

		spec := konfluxv1alpha1.KonfluxIntegrationServiceSpec{
			IntegrationControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
					},
				},
			},
		}

		err := applyIntegrationServiceDeploymentCustomizations(deployment, spec, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
		g.Expect(managerContainer.Resources.Limits.Memory().String()).To(gomega.Equal("512Mi"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("100m"))
	})
}

// findEnvVar finds an environment variable by name in a container's env array.
func findEnvVar(env []corev1.EnvVar, name string) *corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			return &env[i]
		}
	}
	return nil
}

func TestBuildControllerManagerOverlay_ConsoleURL(t *testing.T) {
	t.Run("injects CONSOLE_URL when provided", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{}

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, testConsoleURL)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		envVar := findEnvVar(managerContainer.Env, "CONSOLE_URL")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(testConsoleURLTemplate))
	})

	t.Run("injects CONSOLE_URL when provided with nil spec", func(t *testing.T) {
		g := gomega.NewWithT(t)

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(nil, testConsoleURL)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		envVar := findEnvVar(managerContainer.Env, "CONSOLE_URL")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(testConsoleURLTemplate))
	})

	t.Run("injects CONSOLE_URL with empty value when URL not available", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{}

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, "")
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		// CONSOLE_URL should be present with empty value
		envVar := findEnvVar(managerContainer.Env, "CONSOLE_URL")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(""))
	})

	t.Run("system-provided CONSOLE_URL overrides user-provided CONSOLE_URL", func(t *testing.T) {
		g := gomega.NewWithT(t)
		userConsoleURL := "https://user-override.example.com"
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{
			Manager: &konfluxv1alpha1.ContainerSpec{
				Env: []corev1.EnvVar{
					{Name: "CONSOLE_URL", Value: userConsoleURL},
					{Name: "OTHER_VAR", Value: "other-value"},
				},
			},
		}

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, testConsoleURL)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		// System-provided CONSOLE_URL should override user-provided one
		envVar := findEnvVar(managerContainer.Env, "CONSOLE_URL")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(testConsoleURLTemplate))

		// Other user-provided env vars should still be present
		otherVar := findEnvVar(managerContainer.Env, "OTHER_VAR")
		g.Expect(otherVar).NotTo(gomega.BeNil())
		g.Expect(otherVar.Value).To(gomega.Equal("other-value"))
	})

	t.Run("CONSOLE_URL works together with other environment variables", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{
			Manager: &konfluxv1alpha1.ContainerSpec{
				Env: []corev1.EnvVar{
					{Name: "CUSTOM_VAR", Value: "custom-value"},
				},
			},
		}

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, testConsoleURL)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		// Both CONSOLE_URL and user-provided env vars should be present
		consoleURLVar := findEnvVar(managerContainer.Env, "CONSOLE_URL")
		g.Expect(consoleURLVar).NotTo(gomega.BeNil())
		g.Expect(consoleURLVar.Value).To(gomega.Equal(testConsoleURLTemplate))

		customVar := findEnvVar(managerContainer.Env, "CUSTOM_VAR")
		g.Expect(customVar).NotTo(gomega.BeNil())
		g.Expect(customVar.Value).To(gomega.Equal("custom-value"))
	})

	t.Run("preserves Resources when Env is nil with CONSOLE_URL", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{
			Manager: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
					},
				},
				// Env is nil (zero value) - this tests the defensive copy fix
			},
		}

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, testConsoleURL)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		// Resources should be preserved
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))

		// CONSOLE_URL should be injected
		consoleURLVar := findEnvVar(managerContainer.Env, "CONSOLE_URL")
		g.Expect(consoleURLVar).NotTo(gomega.BeNil())
		g.Expect(consoleURLVar.Value).To(gomega.Equal(testConsoleURLTemplate))
	})

	t.Run("preserves Resources when Env is empty slice with CONSOLE_URL", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{
			Manager: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
				},
				Env: []corev1.EnvVar{}, // Empty slice, not nil
			},
		}

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, testConsoleURL)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		// Resources should be preserved
		g.Expect(managerContainer.Resources.Requests.Memory().String()).To(gomega.Equal("256Mi"))

		// CONSOLE_URL should be injected
		consoleURLVar := findEnvVar(managerContainer.Env, "CONSOLE_URL")
		g.Expect(consoleURLVar).NotTo(gomega.BeNil())
		g.Expect(consoleURLVar.Value).To(gomega.Equal(testConsoleURLTemplate))
	})

	t.Run("updates CONSOLE_URL when console URL changes", func(t *testing.T) {
		g := gomega.NewWithT(t)
		oldConsoleURL := "https://old.example.com"
		oldConsoleURLTemplate := "https://old.example.com/ns/{{ .Namespace }}/pipelinerun/{{ .PipelineRunName }}"

		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{}

		// First, apply with old console URL
		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, oldConsoleURL)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		// Verify old URL template is set
		envVar := findEnvVar(managerContainer.Env, "CONSOLE_URL")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(oldConsoleURLTemplate))

		// Now apply with new console URL (simulating KonfluxUI ingress URL change)
		overlay = buildControllerManagerOverlay(spec, testConsoleURL)
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		// Verify URL was updated to new value
		envVar = findEnvVar(managerContainer.Env, "CONSOLE_URL")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(testConsoleURLTemplate))
	})
}
