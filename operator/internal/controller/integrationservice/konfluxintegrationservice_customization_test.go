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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const (
	testConsoleURL                = "https://konflux.example.com"
	testConsoleURLTemplate        = testConsoleURL + "/ns/{{ .Namespace }}/pipelinerun/{{ .PipelineRunName }}"
	testConsoleURLTasklogTemplate = testConsoleURLTemplate + "/logs/{{ .TaskName }}"
)

// getIntegrationSnapshotGCCronJob returns a deep copy of the snapshot GC CronJob from the IntegrationService manifests.
func getIntegrationSnapshotGCCronJob(t *testing.T) *batchv1.CronJob {
	t.Helper()
	store := testutil.GetTestObjectStore(t)
	objects, err := store.GetForComponent(manifests.Integration)
	if err != nil {
		t.Fatalf("failed to get IntegrationService manifests: %v", err)
	}

	for _, obj := range objects {
		if cj, ok := obj.(*batchv1.CronJob); ok && cj.Name == snapshotGCCronJobName {
			return cj.DeepCopy()
		}
	}
	t.Fatalf("CronJob %q not found in IntegrationService manifests", snapshotGCCronJobName)
	return nil
}

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
		overlay := buildControllerManagerOverlay(nil, "", konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
		g.Expect(overlay).NotTo(gomega.BeNil())
	})

	t.Run("empty spec returns overlay without customizations", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{}
		overlay := buildControllerManagerOverlay(spec, "", konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
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
		overlay := buildControllerManagerOverlay(spec, "", konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
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

		overlay := buildControllerManagerOverlay(spec, "", konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
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
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
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
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
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
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
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
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{}

		deployment := getIntegrationServiceDeployment(t)
		err := applyIntegrationServiceDeploymentCustomizations(deployment, spec, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		g.Expect(deployment.Spec.Template.Spec.Containers).NotTo(gomega.BeEmpty())
	})

	t.Run("applies replicas to controller-manager deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
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
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
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
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
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
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
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

		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
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

		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
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
		overlay := buildControllerManagerOverlay(spec, testConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
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
		overlay := buildControllerManagerOverlay(nil, testConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
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
		overlay := buildControllerManagerOverlay(spec, "", konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
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
		overlay := buildControllerManagerOverlay(spec, testConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
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
		overlay := buildControllerManagerOverlay(spec, testConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
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
		overlay := buildControllerManagerOverlay(spec, testConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
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
		overlay := buildControllerManagerOverlay(spec, testConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
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
		overlay := buildControllerManagerOverlay(spec, oldConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		// Verify old URL template is set
		envVar := findEnvVar(managerContainer.Env, "CONSOLE_URL")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(oldConsoleURLTemplate))

		// Now apply with new console URL (simulating KonfluxUI ingress URL change)
		overlay = buildControllerManagerOverlay(spec, testConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
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

func TestBuildControllerManagerOverlay_ConsoleURLTasklog(t *testing.T) {
	t.Run("injects CONSOLE_URL_TASKLOG when provided", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{}

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, testConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		envVar := findEnvVar(managerContainer.Env, "CONSOLE_URL_TASKLOG")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(testConsoleURLTasklogTemplate))
	})

	t.Run("injects CONSOLE_URL_TASKLOG when provided with nil spec", func(t *testing.T) {
		g := gomega.NewWithT(t)

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(nil, testConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		envVar := findEnvVar(managerContainer.Env, "CONSOLE_URL_TASKLOG")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(testConsoleURLTasklogTemplate))
	})

	t.Run("injects CONSOLE_URL_TASKLOG with empty value when URL not available", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{}

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, "", konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		envVar := findEnvVar(managerContainer.Env, "CONSOLE_URL_TASKLOG")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(""))
	})

	t.Run("system-provided CONSOLE_URL_TASKLOG overrides user-provided value", func(t *testing.T) {
		g := gomega.NewWithT(t)
		userTasklogURL := "https://user-override.example.com/logs"
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{
			Manager: &konfluxv1alpha1.ContainerSpec{
				Env: []corev1.EnvVar{
					{Name: "CONSOLE_URL_TASKLOG", Value: userTasklogURL},
				},
			},
		}

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, testConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		envVar := findEnvVar(managerContainer.Env, "CONSOLE_URL_TASKLOG")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(testConsoleURLTasklogTemplate))
	})

	t.Run("updates CONSOLE_URL_TASKLOG when console URL changes", func(t *testing.T) {
		g := gomega.NewWithT(t)
		oldConsoleURL := "https://old.example.com"
		oldTasklogTemplate := oldConsoleURL + "/ns/{{ .Namespace }}/pipelinerun/{{ .PipelineRunName }}/logs/{{ .TaskName }}"

		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{}

		deployment := getIntegrationServiceDeployment(t)
		overlay := buildControllerManagerOverlay(spec, oldConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		envVar := findEnvVar(managerContainer.Env, "CONSOLE_URL_TASKLOG")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(oldTasklogTemplate))

		overlay = buildControllerManagerOverlay(spec, testConsoleURL, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})
		err = overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		envVar = findEnvVar(managerContainer.Env, "CONSOLE_URL_TASKLOG")
		g.Expect(envVar).NotTo(gomega.BeNil())
		g.Expect(envVar.Value).To(gomega.Equal(testConsoleURLTasklogTemplate))
	})
}

func TestBuildControllerManagerOverlay_PipelineTimeouts(t *testing.T) {
	t.Run("typed timeout fields are injected as env vars", func(t *testing.T) {
		g := gomega.NewWithT(t)
		integrationSpec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			PipelineTimeout: "6h",
			TasksTimeout:    "4h",
			FinallyTimeout:  "2h",
		}
		deployment := getIntegrationServiceDeployment(t)
		g.Expect(applyIntegrationServiceDeploymentCustomizations(deployment, integrationSpec, "")).To(gomega.Succeed())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		pipelineTimeoutVar := findEnvVar(managerContainer.Env, envPipelineTimeout)
		g.Expect(pipelineTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(pipelineTimeoutVar.Value).To(gomega.Equal("6h"))
		tasksTimeoutVar := findEnvVar(managerContainer.Env, envTasksTimeout)
		g.Expect(tasksTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(tasksTimeoutVar.Value).To(gomega.Equal("4h"))
		finallyTimeoutVar := findEnvVar(managerContainer.Env, envFinallyTimeout)
		g.Expect(finallyTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(finallyTimeoutVar.Value).To(gomega.Equal("2h"))
	})

	t.Run("typed CRD field overrides explicit manager.env entry", func(t *testing.T) {
		g := gomega.NewWithT(t)
		integrationSpec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			PipelineTimeout: "6h",
			TasksTimeout:    "4h",
			FinallyTimeout:  "2h",
			IntegrationControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: envPipelineTimeout, Value: "12h"},
						{Name: envTasksTimeout, Value: "10h"},
						{Name: envFinallyTimeout, Value: "8h"},
					},
				},
			},
		}
		deployment := getIntegrationServiceDeployment(t)
		g.Expect(applyIntegrationServiceDeploymentCustomizations(deployment, integrationSpec, "")).To(gomega.Succeed())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		pipelineTimeoutVar := findEnvVar(managerContainer.Env, envPipelineTimeout)
		g.Expect(pipelineTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(pipelineTimeoutVar.Value).To(gomega.Equal("6h"))
		tasksTimeoutVar := findEnvVar(managerContainer.Env, envTasksTimeout)
		g.Expect(tasksTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(tasksTimeoutVar.Value).To(gomega.Equal("4h"))
		finallyTimeoutVar := findEnvVar(managerContainer.Env, envFinallyTimeout)
		g.Expect(finallyTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(finallyTimeoutVar.Value).To(gomega.Equal("2h"))
	})

	t.Run("empty timeout fields do not inject env vars", func(t *testing.T) {
		g := gomega.NewWithT(t)
		integrationSpec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{}
		deployment := getIntegrationServiceDeployment(t)
		g.Expect(applyIntegrationServiceDeploymentCustomizations(deployment, integrationSpec, "")).To(gomega.Succeed())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(findEnvVar(managerContainer.Env, envPipelineTimeout)).To(gomega.BeNil())
		g.Expect(findEnvVar(managerContainer.Env, envTasksTimeout)).To(gomega.BeNil())
		g.Expect(findEnvVar(managerContainer.Env, envFinallyTimeout)).To(gomega.BeNil())
	})

	t.Run("manager.env values pass through when CRD timeout fields are not set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		integrationSpec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			// PipelineTimeout, TasksTimeout, FinallyTimeout intentionally omitted
			IntegrationControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: envPipelineTimeout, Value: "8h"},
						{Name: envTasksTimeout, Value: "5h"},
						{Name: envFinallyTimeout, Value: "3h"},
					},
				},
			},
		}
		deployment := getIntegrationServiceDeployment(t)
		g.Expect(applyIntegrationServiceDeploymentCustomizations(deployment, integrationSpec, "")).To(gomega.Succeed())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		pipelineTimeoutVar := findEnvVar(managerContainer.Env, envPipelineTimeout)
		g.Expect(pipelineTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(pipelineTimeoutVar.Value).To(gomega.Equal("8h"))
		tasksTimeoutVar := findEnvVar(managerContainer.Env, envTasksTimeout)
		g.Expect(tasksTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(tasksTimeoutVar.Value).To(gomega.Equal("5h"))
		finallyTimeoutVar := findEnvVar(managerContainer.Env, envFinallyTimeout)
		g.Expect(finallyTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(finallyTimeoutVar.Value).To(gomega.Equal("3h"))
	})

	t.Run("typed field wins for its var, manager.env pass-through for unset ones", func(t *testing.T) {
		g := gomega.NewWithT(t)
		integrationSpec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			PipelineTimeout: "6h", // typed field — should take precedence
			// TasksTimeout and FinallyTimeout intentionally omitted — manager.env should pass through
			IntegrationControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: envPipelineTimeout, Value: "12h"}, // should be overridden by typed field
						{Name: envTasksTimeout, Value: "5h"},     // should pass through unchanged
						{Name: envFinallyTimeout, Value: "3h"},   // should pass through unchanged
					},
				},
			},
		}
		deployment := getIntegrationServiceDeployment(t)
		g.Expect(applyIntegrationServiceDeploymentCustomizations(deployment, integrationSpec, "")).To(gomega.Succeed())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		pipelineTimeoutVar := findEnvVar(managerContainer.Env, envPipelineTimeout)
		g.Expect(pipelineTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(pipelineTimeoutVar.Value).To(gomega.Equal("6h"))

		tasksTimeoutVar := findEnvVar(managerContainer.Env, envTasksTimeout)
		g.Expect(tasksTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(tasksTimeoutVar.Value).To(gomega.Equal("5h"))

		finallyTimeoutVar := findEnvVar(managerContainer.Env, envFinallyTimeout)
		g.Expect(finallyTimeoutVar).NotTo(gomega.BeNil())
		g.Expect(finallyTimeoutVar.Value).To(gomega.Equal("3h"))
	})
}

func TestApplySnapshotGCCustomizations_ContainerNotFound(t *testing.T) {
	makeEmptyCronJob := func(name string) *batchv1.CronJob {
		return &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: batchv1.CronJobSpec{
				JobTemplate: batchv1.JobTemplateSpec{
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "other-container"},
								},
							},
						},
					},
				},
			},
		}
	}

	t.Run("returns error when ContainerSpec is set but container is not found", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := makeEmptyCronJob(snapshotGCCronJobName)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			SnapshotGarbageCollector: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
				},
			},
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.MatchError(gomega.ContainSubstring(snapshotGCContainerName)))
	})

	t.Run("returns error when typed fields are set but container is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := makeEmptyCronJob(snapshotGCCronJobName)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			PRSnapshotsToKeep: "10",
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.MatchError(gomega.ContainSubstring(snapshotGCContainerName)))
	})

	t.Run("returns error when nonPRSnapshotsToKeep is set but container is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := makeEmptyCronJob(snapshotGCCronJobName)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			NonPRSnapshotsToKeep: "100",
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.MatchError(gomega.ContainSubstring(snapshotGCContainerName)))
	})

	t.Run("returns error when minSnapshotsToKeepPerComponent is set but container is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := makeEmptyCronJob(snapshotGCCronJobName)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			MinSnapshotsToKeepPerComponent: "4",
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.MatchError(gomega.ContainSubstring(snapshotGCContainerName)))
	})

	t.Run("returns error when ContainerSpec env-only is set but container is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := makeEmptyCronJob(snapshotGCCronJobName)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			SnapshotGarbageCollector: &konfluxv1alpha1.ContainerSpec{
				Env: []corev1.EnvVar{{Name: "CUSTOM", Value: "val"}},
			},
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.MatchError(gomega.ContainSubstring(snapshotGCContainerName)))
	})

	t.Run("returns error when both ContainerSpec and typed fields are set but container is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := makeEmptyCronJob(snapshotGCCronJobName)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			PRSnapshotsToKeep: "10",
			SnapshotGarbageCollector: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("4Gi")},
				},
			},
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.MatchError(gomega.ContainSubstring(snapshotGCContainerName)))
	})

	t.Run("returns nil when no customizations are set and container is not found", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := makeEmptyCronJob(snapshotGCCronJobName)
		g.Expect(applySnapshotGCCustomizations(cj, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})).To(gomega.Succeed())
	})
}

func TestApplySnapshotGCCustomizations(t *testing.T) {
	t.Run("injects env vars from ContainerSpec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			SnapshotGarbageCollector: &konfluxv1alpha1.ContainerSpec{
				Env: []corev1.EnvVar{
					{Name: envPRSnapshotsToKeep, Value: "10"},
					{Name: envNonPRSnapshotsToKeep, Value: "20"},
					{Name: envMinSnapshotsToKeepPerComponent, Value: "5"},
				},
			},
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.Succeed())
		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		prVar := findEnvVar(gc.Env, envPRSnapshotsToKeep)
		g.Expect(prVar).NotTo(gomega.BeNil())
		g.Expect(prVar.Value).To(gomega.Equal("10"))
		nonPRVar := findEnvVar(gc.Env, envNonPRSnapshotsToKeep)
		g.Expect(nonPRVar).NotTo(gomega.BeNil())
		g.Expect(nonPRVar.Value).To(gomega.Equal("20"))
		minVar := findEnvVar(gc.Env, envMinSnapshotsToKeepPerComponent)
		g.Expect(minVar).NotTo(gomega.BeNil())
		g.Expect(minVar.Value).To(gomega.Equal("5"))
	})

	t.Run("nil ContainerSpec leaves GC container unchanged", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		g.Expect(applySnapshotGCCustomizations(cj, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})).To(gomega.Succeed())
		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		g.Expect(findEnvVar(gc.Env, envPRSnapshotsToKeep)).To(gomega.BeNil())
	})

	t.Run("resources from ContainerSpec are applied to GC container", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			SnapshotGarbageCollector: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"),
						corev1.ResourceMemory: resource.MustParse("2000Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1000m"),
						corev1.ResourceMemory: resource.MustParse("2000Mi"),
					},
				},
			},
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.Succeed())
		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		g.Expect(gc.Resources.Requests.Cpu().String()).To(gomega.Equal("1"))
		g.Expect(gc.Resources.Requests.Memory().String()).To(gomega.Equal("2000Mi"))
		g.Expect(gc.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
		g.Expect(gc.Resources.Limits.Memory().String()).To(gomega.Equal("2000Mi"))
	})

	t.Run("requests-only ContainerSpec preserves embedded limits", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		gcBefore := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gcBefore).NotTo(gomega.BeNil())
		origLimitsCPU := gcBefore.Resources.Limits.Cpu().String()
		origLimitsMem := gcBefore.Resources.Limits.Memory().String()

		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			SnapshotGarbageCollector: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
				},
			},
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.Succeed())

		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		g.Expect(gc.Resources.Requests.Cpu().String()).To(gomega.Equal("50m"))
		g.Expect(gc.Resources.Requests.Memory().String()).To(gomega.Equal("64Mi"))
		g.Expect(gc.Resources.Limits.Cpu().String()).To(gomega.Equal(origLimitsCPU))
		g.Expect(gc.Resources.Limits.Memory().String()).To(gomega.Equal(origLimitsMem))
	})

	t.Run("typed fields only apply env vars without ContainerSpec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		gcBefore := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gcBefore).NotTo(gomega.BeNil())
		origLimitsCPU := gcBefore.Resources.Limits.Cpu().String()

		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			PRSnapshotsToKeep: "25",
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.Succeed())

		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		prVar := findEnvVar(gc.Env, envPRSnapshotsToKeep)
		g.Expect(prVar).NotTo(gomega.BeNil())
		g.Expect(prVar.Value).To(gomega.Equal("25"))
		g.Expect(gc.Resources.Limits.Cpu().String()).To(gomega.Equal(origLimitsCPU))
	})

	t.Run("resources and typed fields applied together", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			PRSnapshotsToKeep:    "30",
			NonPRSnapshotsToKeep: "200",
			SnapshotGarbageCollector: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.Succeed())

		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		g.Expect(gc.Resources.Limits.Cpu().String()).To(gomega.Equal("4"))
		g.Expect(gc.Resources.Limits.Memory().String()).To(gomega.Equal("8Gi"))
		prVar := findEnvVar(gc.Env, envPRSnapshotsToKeep)
		g.Expect(prVar).NotTo(gomega.BeNil())
		g.Expect(prVar.Value).To(gomega.Equal("30"))
		nonPRVar := findEnvVar(gc.Env, envNonPRSnapshotsToKeep)
		g.Expect(nonPRVar).NotTo(gomega.BeNil())
		g.Expect(nonPRVar.Value).To(gomega.Equal("200"))
	})

	t.Run("ContainerSpec env-only applies without resources", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		gcBefore := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gcBefore).NotTo(gomega.BeNil())
		origLimitsCPU := gcBefore.Resources.Limits.Cpu().String()

		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			SnapshotGarbageCollector: &konfluxv1alpha1.ContainerSpec{
				Env: []corev1.EnvVar{
					{Name: "CUSTOM_FLAG", Value: "enabled"},
				},
			},
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.Succeed())

		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		customVar := findEnvVar(gc.Env, "CUSTOM_FLAG")
		g.Expect(customVar).NotTo(gomega.BeNil())
		g.Expect(customVar.Value).To(gomega.Equal("enabled"))
		g.Expect(gc.Resources.Limits.Cpu().String()).To(gomega.Equal(origLimitsCPU))
	})
}

func TestApplySnapshotGCCustomizations_TypedFields(t *testing.T) {
	t.Run("typed retention fields are injected as env vars", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		prToKeep := "10"
		nonPRToKeep := "20"
		minToKeep := "5"
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			PRSnapshotsToKeep:              prToKeep,
			NonPRSnapshotsToKeep:           nonPRToKeep,
			MinSnapshotsToKeepPerComponent: minToKeep,
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.Succeed())

		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		prVar := findEnvVar(gc.Env, envPRSnapshotsToKeep)
		g.Expect(prVar).NotTo(gomega.BeNil())
		g.Expect(prVar.Value).To(gomega.Equal(prToKeep))
		nonPRVar := findEnvVar(gc.Env, envNonPRSnapshotsToKeep)
		g.Expect(nonPRVar).NotTo(gomega.BeNil())
		g.Expect(nonPRVar.Value).To(gomega.Equal(nonPRToKeep))
		minVar := findEnvVar(gc.Env, envMinSnapshotsToKeepPerComponent)
		g.Expect(minVar).NotTo(gomega.BeNil())
		g.Expect(minVar.Value).To(gomega.Equal(minToKeep))
	})

	t.Run("typed CRD fields override explicit snapshotGarbageCollector.env entries", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		prToKeep := "10"
		nonPRToKeep := "20"
		minToKeep := "5"
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			PRSnapshotsToKeep:              prToKeep,
			NonPRSnapshotsToKeep:           nonPRToKeep,
			MinSnapshotsToKeepPerComponent: minToKeep,
			SnapshotGarbageCollector: &konfluxv1alpha1.ContainerSpec{
				Env: []corev1.EnvVar{
					{Name: envPRSnapshotsToKeep, Value: "99"},
					{Name: envNonPRSnapshotsToKeep, Value: "99"},
					{Name: envMinSnapshotsToKeepPerComponent, Value: "99"},
				},
			},
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.Succeed())

		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		prVar := findEnvVar(gc.Env, envPRSnapshotsToKeep)
		g.Expect(prVar).NotTo(gomega.BeNil())
		g.Expect(prVar.Value).To(gomega.Equal(prToKeep))
		nonPRVar := findEnvVar(gc.Env, envNonPRSnapshotsToKeep)
		g.Expect(nonPRVar).NotTo(gomega.BeNil())
		g.Expect(nonPRVar.Value).To(gomega.Equal(nonPRToKeep))
		minVar := findEnvVar(gc.Env, envMinSnapshotsToKeepPerComponent)
		g.Expect(minVar).NotTo(gomega.BeNil())
		g.Expect(minVar.Value).To(gomega.Equal(minToKeep))
	})

	t.Run("nil typed fields do not inject env vars", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		g.Expect(applySnapshotGCCustomizations(cj, konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{})).To(gomega.Succeed())

		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		g.Expect(findEnvVar(gc.Env, envPRSnapshotsToKeep)).To(gomega.BeNil())
		g.Expect(findEnvVar(gc.Env, envNonPRSnapshotsToKeep)).To(gomega.BeNil())
		g.Expect(findEnvVar(gc.Env, envMinSnapshotsToKeepPerComponent)).To(gomega.BeNil())
	})

	t.Run("zero-value typed fields inject '0', not treated as empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		prToKeep := "0"
		nonPRToKeep := "0"
		minToKeep := "0"
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			PRSnapshotsToKeep:              prToKeep,
			NonPRSnapshotsToKeep:           nonPRToKeep,
			MinSnapshotsToKeepPerComponent: minToKeep,
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.Succeed())

		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		prVar := findEnvVar(gc.Env, envPRSnapshotsToKeep)
		g.Expect(prVar).NotTo(gomega.BeNil())
		g.Expect(prVar.Value).To(gomega.Equal(prToKeep))
		nonPRVar := findEnvVar(gc.Env, envNonPRSnapshotsToKeep)
		g.Expect(nonPRVar).NotTo(gomega.BeNil())
		g.Expect(nonPRVar.Value).To(gomega.Equal(nonPRToKeep))
		minVar := findEnvVar(gc.Env, envMinSnapshotsToKeepPerComponent)
		g.Expect(minVar).NotTo(gomega.BeNil())
		g.Expect(minVar.Value).To(gomega.Equal(minToKeep))
	})

	t.Run("ContainerSpec resources and typed fields are applied together", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			PRSnapshotsToKeep: "15",
			SnapshotGarbageCollector: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.Succeed())

		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		g.Expect(gc.Resources.Limits.Cpu().String()).To(gomega.Equal("2"))
		g.Expect(gc.Resources.Limits.Memory().String()).To(gomega.Equal("4Gi"))
		prVar := findEnvVar(gc.Env, envPRSnapshotsToKeep)
		g.Expect(prVar).NotTo(gomega.BeNil())
		g.Expect(prVar.Value).To(gomega.Equal("15"))
	})

	t.Run("snapshotGarbageCollector.env values pass through when typed fields are not set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cj := getIntegrationSnapshotGCCronJob(t)
		// PRSnapshotsToKeep, NonPRSnapshotsToKeep, MinSnapshotsToKeepPerComponent intentionally omitted
		prToKeep := "70"
		nonPRToKeep := "640"
		minToKeep := "1"
		spec := konfluxv1alpha1.KonfluxIntegrationServiceConfigSpec{
			SnapshotGarbageCollector: &konfluxv1alpha1.ContainerSpec{
				Env: []corev1.EnvVar{
					{Name: envPRSnapshotsToKeep, Value: prToKeep},
					{Name: envNonPRSnapshotsToKeep, Value: nonPRToKeep},
					{Name: envMinSnapshotsToKeepPerComponent, Value: minToKeep},
				},
			},
		}
		g.Expect(applySnapshotGCCustomizations(cj, spec)).To(gomega.Succeed())

		gc := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, snapshotGCContainerName)
		g.Expect(gc).NotTo(gomega.BeNil())
		prVar := findEnvVar(gc.Env, envPRSnapshotsToKeep)
		g.Expect(prVar).NotTo(gomega.BeNil())
		g.Expect(prVar.Value).To(gomega.Equal(prToKeep))
		nonPRVar := findEnvVar(gc.Env, envNonPRSnapshotsToKeep)
		g.Expect(nonPRVar).NotTo(gomega.BeNil())
		g.Expect(nonPRVar.Value).To(gomega.Equal(nonPRToKeep))
		minVar := findEnvVar(gc.Env, envMinSnapshotsToKeepPerComponent)
		g.Expect(minVar).NotTo(gomega.BeNil())
		g.Expect(minVar.Value).To(gomega.Equal(minToKeep))
	})
}
