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

package releaseservice

import (
	"testing"

	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

// getReleaseServiceDeployment returns a deep copy of the ReleaseService controller-manager deployment from the manifests.
func getReleaseServiceDeployment(t *testing.T) *appsv1.Deployment {
	t.Helper()
	store := testutil.GetTestObjectStore(t)
	objects, err := store.GetForComponent(manifests.Release)
	if err != nil {
		t.Fatalf("failed to get ReleaseService manifests: %v", err)
	}

	for _, obj := range objects {
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if deployment.Name == releaseControllerManagerDeploymentName {
				return deployment
			}
		}
	}
	t.Fatalf("deployment %q not found in ReleaseService manifests", releaseControllerManagerDeploymentName)
	return nil
}

func TestBuildReleaseControllerManagerOverlay(t *testing.T) {
	t.Run("nil spec returns empty overlay", func(t *testing.T) {
		g := gomega.NewWithT(t)
		overlay := buildReleaseControllerManagerOverlay(nil)
		g.Expect(overlay).NotTo(gomega.BeNil())
	})

	t.Run("empty spec returns overlay without customizations", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ControllerManagerDeploymentSpec{}
		overlay := buildReleaseControllerManagerOverlay(spec)
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

		deployment := getReleaseServiceDeployment(t)
		overlay := buildReleaseControllerManagerOverlay(spec)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
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

		deployment := getReleaseServiceDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist in controller-manager deployment")
		originalImage := managerContainer.Image

		overlay := buildReleaseControllerManagerOverlay(spec)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Image).To(gomega.Equal(originalImage))
	})
}

func TestApplyReleaseServiceDeploymentCustomizations(t *testing.T) {
	t.Run("applies customizations to controller-manager deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				ReleaseControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
					Manager: &konfluxv1alpha1.ContainerSpec{
						Resources: &corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("1"),
							},
						},
					},
				},
			},
		}

		deployment := getReleaseServiceDeployment(t)
		err := applyReleaseServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
	})

	t.Run("ignores unknown deployment names", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				ReleaseControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
					Manager: &konfluxv1alpha1.ContainerSpec{
						Resources: &corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("1"),
							},
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

		err := applyReleaseServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic and container should be unchanged
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(gomega.BeNil())
	})

	t.Run("handles nil controller-manager spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				ReleaseControllerManager: nil,
			},
		}

		deployment := getReleaseServiceDeployment(t)
		err := applyReleaseServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
	})

	t.Run("handles empty spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{}

		deployment := getReleaseServiceDeployment(t)
		err := applyReleaseServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		g.Expect(deployment.Spec.Template.Spec.Containers).NotTo(gomega.BeEmpty())
	})

	t.Run("applies replicas to controller-manager deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				ReleaseControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
					Replicas: 3,
				},
			},
		}

		deployment := getReleaseServiceDeployment(t)
		err := applyReleaseServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(3)))
	})

	t.Run("applies default replicas when using default value", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				ReleaseControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
					Replicas: 1, // default value
				},
			},
		}

		deployment := getReleaseServiceDeployment(t)
		err := applyReleaseServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(1)))
	})

	t.Run("does not modify replicas when controller-manager spec is nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				ReleaseControllerManager: nil,
			},
		}

		deployment := getReleaseServiceDeployment(t)
		originalReplicas := deployment.Spec.Replicas
		err := applyReleaseServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).To(gomega.Equal(originalReplicas))
	})

	t.Run("applies replicas together with container resources", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				ReleaseControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
					Replicas: 5,
					Manager: &konfluxv1alpha1.ContainerSpec{
						Resources: &corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("2"),
							},
						},
					},
				},
			},
		}

		deployment := getReleaseServiceDeployment(t)
		err := applyReleaseServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Check replicas
		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(5)))

		// Check container resources
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("2"))
	})
}

// getReleaseServiceConfig returns a deep copy of the ReleaseServiceConfig from the manifests.
func getReleaseServiceConfig(t *testing.T) *unstructured.Unstructured {
	t.Helper()
	store := testutil.GetTestObjectStore(t)
	objects, err := store.GetForComponent(manifests.Release)
	if err != nil {
		t.Fatalf("failed to get ReleaseService manifests: %v", err)
	}

	for _, obj := range objects {
		if isReleaseServiceConfig(obj) {
			u, ok := obj.(*unstructured.Unstructured)
			if !ok {
				t.Fatalf("ReleaseServiceConfig is not *unstructured.Unstructured")
			}
			return u
		}
	}
	t.Fatalf("ReleaseServiceConfig not found in ReleaseService manifests")
	return nil
}

func TestIsReleaseServiceConfig(t *testing.T) {
	t.Run("returns true for ReleaseServiceConfig", func(t *testing.T) {
		g := gomega.NewWithT(t)
		obj := &unstructured.Unstructured{}
		obj.Object = map[string]interface{}{
			"apiVersion": "appstudio.redhat.com/v1alpha1",
			"kind":       "ReleaseServiceConfig",
			"metadata": map[string]interface{}{
				"name":      "release-service-config",
				"namespace": "release-service",
			},
			"spec": map[string]interface{}{
				"debug": false,
			},
		}
		g.Expect(isReleaseServiceConfig(obj)).To(gomega.BeTrue())
	})

	t.Run("returns false for Deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}
		g.Expect(isReleaseServiceConfig(deployment)).To(gomega.BeFalse())
	})

	t.Run("returns true for embedded ReleaseServiceConfig", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		g.Expect(isReleaseServiceConfig(rsc)).To(gomega.BeTrue())
	})
}

func TestApplyReleaseServiceConfigCustomizations(t *testing.T) {
	t.Run("sets debug to true when spec.Debug is true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				Debug: true,
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		g.Expect(rscSpec).NotTo(gomega.BeNil())
		g.Expect(rscSpec[rscFieldDebug]).To(gomega.BeTrue())
	})

	t.Run("sets debug to false when spec.Debug is false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				Debug: false,
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		g.Expect(rscSpec).NotTo(gomega.BeNil())
		g.Expect(rscSpec[rscFieldDebug]).To(gomega.BeFalse())
	})

	t.Run("sets EmptyDirOverrides when provided", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				EmptyDirOverrides: []konfluxv1alpha1.EmptyDirOverride{
					{
						URL:        ".*",
						Revision:   ".*",
						PathInRepo: "pipelines/managed/fbc-release/fbc-release.yaml",
					},
					{
						URL:        "https://github.com/example/repo",
						Revision:   "main",
						PathInRepo: "pipelines/test.yaml",
					},
				},
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		g.Expect(rscSpec).NotTo(gomega.BeNil())
		overrides, ok := rscSpec[rscFieldEmptyDirOverrides].([]interface{})
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(overrides).To(gomega.HaveLen(2))

		first, _ := overrides[0].(map[string]interface{})
		g.Expect(first[rscFieldURL]).To(gomega.Equal(".*"))
		g.Expect(first[rscFieldRevision]).To(gomega.Equal(".*"))
		g.Expect(first[rscFieldPathInRepo]).To(gomega.Equal("pipelines/managed/fbc-release/fbc-release.yaml"))

		second, _ := overrides[1].(map[string]interface{})
		g.Expect(second[rscFieldURL]).To(gomega.Equal("https://github.com/example/repo"))
		g.Expect(second[rscFieldRevision]).To(gomega.Equal("main"))
		g.Expect(second[rscFieldPathInRepo]).To(gomega.Equal("pipelines/test.yaml"))
	})

	t.Run("removes EmptyDirOverrides when not provided", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		// First set some overrides
		rsc.Object["spec"] = map[string]interface{}{
			rscFieldDebug: false,
			rscFieldEmptyDirOverrides: []interface{}{
				map[string]interface{}{rscFieldURL: ".*", rscFieldRevision: ".*", rscFieldPathInRepo: "test.yaml"},
			},
		}

		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				EmptyDirOverrides: nil,
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		g.Expect(rscSpec).NotTo(gomega.BeNil())
		_, exists := rscSpec[rscFieldEmptyDirOverrides]
		g.Expect(exists).To(gomega.BeFalse())
	})

	t.Run("sets both debug and EmptyDirOverrides together", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				Debug: true,
				EmptyDirOverrides: []konfluxv1alpha1.EmptyDirOverride{
					{
						URL:        ".*",
						Revision:   ".*",
						PathInRepo: "pipelines/managed/rh-advisories/rh-advisories.yaml",
					},
				},
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		g.Expect(rscSpec).NotTo(gomega.BeNil())
		g.Expect(rscSpec[rscFieldDebug]).To(gomega.BeTrue())
		overrides, ok := rscSpec[rscFieldEmptyDirOverrides].([]interface{})
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(overrides).To(gomega.HaveLen(1))
	})

	t.Run("empty spec preserves default behavior", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		g.Expect(rscSpec).NotTo(gomega.BeNil())
		g.Expect(rscSpec[rscFieldDebug]).To(gomega.BeFalse())
		_, exists := rscSpec[rscFieldEmptyDirOverrides]
		g.Expect(exists).To(gomega.BeFalse())
	})

	t.Run("handles nil spec map in ReleaseServiceConfig", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := &unstructured.Unstructured{}
		rsc.Object = map[string]interface{}{
			"apiVersion": "appstudio.redhat.com/v1alpha1",
			"kind":       "ReleaseServiceConfig",
			"metadata": map[string]interface{}{
				"name":      "release-service-config",
				"namespace": "release-service",
			},
		}
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				Debug: true,
				EmptyDirOverrides: []konfluxv1alpha1.EmptyDirOverride{
					{URL: ".*", Revision: ".*", PathInRepo: "test.yaml"},
				},
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		g.Expect(rscSpec).NotTo(gomega.BeNil())
		g.Expect(rscSpec[rscFieldDebug]).To(gomega.BeTrue())
		overrides, ok := rscSpec[rscFieldEmptyDirOverrides].([]interface{})
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(overrides).To(gomega.HaveLen(1))
	})

	t.Run("handles non-unstructured object gracefully", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				Debug: true,
			},
		}

		// Should not panic
		err := applyReleaseServiceConfigCustomizations(deployment, spec)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(deployment.Name).To(gomega.Equal("test"))
	})
}

func TestApplyReleaseServiceDeploymentCustomizations_ResourceMerging(t *testing.T) {
	t.Run("merges limits without affecting requests", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Get deployment and set existing requests
		deployment := getReleaseServiceDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist")
		managerContainer.Resources.Requests = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		}

		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				ReleaseControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
					Manager: &konfluxv1alpha1.ContainerSpec{
						Resources: &corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("500m"),
							},
						},
					},
				},
			},
		}

		err := applyReleaseServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("50m"))
		g.Expect(managerContainer.Resources.Requests.Memory().String()).To(gomega.Equal("64Mi"))
	})

	t.Run("merges requests without affecting limits", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Get deployment and set existing limits
		deployment := getReleaseServiceDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist")
		managerContainer.Resources.Limits = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}

		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				ReleaseControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
					Manager: &konfluxv1alpha1.ContainerSpec{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("100m"),
							},
						},
					},
				},
			},
		}

		err := applyReleaseServiceDeploymentCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
		g.Expect(managerContainer.Resources.Limits.Memory().String()).To(gomega.Equal("512Mi"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("100m"))
	})
}
