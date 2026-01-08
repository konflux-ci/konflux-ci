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
	"sync"
	"testing"

	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

var (
	testObjectStore     *manifests.ObjectStore
	testObjectStoreOnce sync.Once
	testObjectStoreErr  error
)

// getTestObjectStore returns a shared ObjectStore for tests.
// It is initialized once and reused across all tests.
func getTestObjectStore(t *testing.T) *manifests.ObjectStore {
	t.Helper()
	testObjectStoreOnce.Do(func() {
		// Add our types to the scheme
		err := konfluxv1alpha1.AddToScheme(scheme.Scheme)
		if err != nil {
			testObjectStoreErr = err
			return
		}
		testObjectStore, testObjectStoreErr = manifests.NewObjectStore(scheme.Scheme)
	})
	if testObjectStoreErr != nil {
		t.Fatalf("failed to create ObjectStore: %v", testObjectStoreErr)
	}
	return testObjectStore
}

// getDeployment returns a deep copy of the BuildService controller-manager deployment from the manifests.
func getDeployment(t *testing.T) *appsv1.Deployment {
	t.Helper()
	store := getTestObjectStore(t)
	objects, err := store.GetForComponent(manifests.BuildService)
	if err != nil {
		t.Fatalf("failed to get BuildService manifests: %v", err)
	}

	for _, obj := range objects {
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if deployment.Name == controllerManagerDeploymentName {
				return deployment
			}
		}
	}
	t.Fatalf("deployment %q not found in BuildService manifests", controllerManagerDeploymentName)
	return nil
}

// findContainer finds a container by name in a slice of containers.
func findContainer(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

func TestBuildControllerManagerOverlay(t *testing.T) {
	t.Run("nil spec returns empty overlay", func(t *testing.T) {
		g := gomega.NewWithT(t)
		overlay := buildControllerManagerOverlay(nil)
		g.Expect(overlay).NotTo(gomega.BeNil())
	})

	t.Run("empty spec returns overlay without customizations", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{}
		overlay := buildControllerManagerOverlay(spec)
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

		deployment := getDeployment(t)
		overlay := buildControllerManagerOverlay(spec)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := findContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
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

		deployment := getDeployment(t)
		managerContainer := findContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist in controller-manager deployment")
		originalImage := managerContainer.Image

		overlay := buildControllerManagerOverlay(spec)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = findContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Image).To(gomega.Equal(originalImage))
	})
}

func TestApplyDeploymentCustomizations(t *testing.T) {
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

		deployment := getDeployment(t)
		err := applyDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := findContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
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

		err := applyDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic and container should be unchanged
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(gomega.BeNil())
	})

	t.Run("handles nil controller-manager spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: nil,
		}

		deployment := getDeployment(t)
		err := applyDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		managerContainer := findContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
	})

	t.Run("handles empty spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{}

		deployment := getDeployment(t)
		err := applyDeploymentCustomizations(deployment, spec)
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

		deployment := getDeployment(t)
		err := applyDeploymentCustomizations(deployment, spec)
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

		deployment := getDeployment(t)
		err := applyDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(1)))
	})

	t.Run("does not modify replicas when controller-manager spec is nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: nil,
		}

		deployment := getDeployment(t)
		originalReplicas := deployment.Spec.Replicas
		err := applyDeploymentCustomizations(deployment, spec)
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

		deployment := getDeployment(t)
		err := applyDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Check replicas
		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(5)))

		// Check container resources
		managerContainer := findContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("2"))
	})
}

func TestApplyDeploymentCustomizations_ResourceMerging(t *testing.T) {
	t.Run("merges limits without affecting requests", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Get deployment and set existing requests
		deployment := getDeployment(t)
		managerContainer := findContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
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

		err := applyDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = findContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("50m"))
		g.Expect(managerContainer.Resources.Requests.Memory().String()).To(gomega.Equal("64Mi"))
	})

	t.Run("merges requests without affecting limits", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Get deployment and set existing limits
		deployment := getDeployment(t)
		managerContainer := findContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
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

		err := applyDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = findContainer(deployment.Spec.Template.Spec.Containers, managerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
		g.Expect(managerContainer.Resources.Limits.Memory().String()).To(gomega.Equal("512Mi"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("100m"))
	})
}

