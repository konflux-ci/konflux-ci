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
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
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

		managerContainer := kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
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
		managerContainer := kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist in controller-manager deployment")
		originalImage := managerContainer.Image

		overlay := buildReleaseControllerManagerOverlay(spec)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
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

		managerContainer := kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
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
		managerContainer := kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
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
		managerContainer := kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
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
		g.Expect(rscSpec["debug"]).To(gomega.BeTrue())
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
		g.Expect(rscSpec["debug"]).To(gomega.BeFalse())
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
		overrides, ok := rscSpec["EmptyDirOverrides"].([]interface{})
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(overrides).To(gomega.HaveLen(2))

		first, _ := overrides[0].(map[string]interface{})
		g.Expect(first["url"]).To(gomega.Equal(".*"))
		g.Expect(first["revision"]).To(gomega.Equal(".*"))
		g.Expect(first["pathInRepo"]).To(gomega.Equal("pipelines/managed/fbc-release/fbc-release.yaml"))

		second, _ := overrides[1].(map[string]interface{})
		g.Expect(second["url"]).To(gomega.Equal("https://github.com/example/repo"))
		g.Expect(second["revision"]).To(gomega.Equal("main"))
		g.Expect(second["pathInRepo"]).To(gomega.Equal("pipelines/test.yaml"))
	})

	t.Run("removes EmptyDirOverrides when not provided", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		// First set some overrides
		rsc.Object["spec"] = map[string]interface{}{
			"debug": false,
			"EmptyDirOverrides": []interface{}{
				map[string]interface{}{"url": ".*", "revision": ".*", "pathInRepo": "test.yaml"},
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
		_, exists := rscSpec["EmptyDirOverrides"]
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
		g.Expect(rscSpec["debug"]).To(gomega.BeTrue())
		overrides, ok := rscSpec["EmptyDirOverrides"].([]interface{})
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
		g.Expect(rscSpec["debug"]).To(gomega.BeFalse())
		_, exists := rscSpec["EmptyDirOverrides"]
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
		g.Expect(rscSpec["debug"]).To(gomega.BeTrue())
		overrides, ok := rscSpec["EmptyDirOverrides"].([]interface{})
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(overrides).To(gomega.HaveLen(1))
	})

	t.Run("sets retryablePipelines when provided", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				RetryablePipelines: []konfluxv1alpha1.RetryablePipeline{
					{
						URL:        "https://github.com/konflux-ci/release-service-catalog.git",
						Revision:   ".*",
						PathInRepo: "pipelines/managed/fbc-release/fbc-release.yaml",
						RetryPolicy: konfluxv1alpha1.RetryPolicy{
							MaxRetries: 1,
							DisableOn: &konfluxv1alpha1.RetryDisableOn{
								Tags: []string{`.*\{\{\s*incrementer\s*\}\}.*`},
							},
							Mitigations: &konfluxv1alpha1.RetryMitigations{
								OOMKill: &konfluxv1alpha1.OOMKillMitigation{
									Multiplier: "2",
									MaxComputeResources: &konfluxv1alpha1.ComputeResources{
										Limits: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("8Gi"),
										},
										Requests: corev1.ResourceList{
											corev1.ResourceMemory: resource.MustParse("8Gi"),
										},
									},
								},
								Timeout: &konfluxv1alpha1.TimeoutMitigation{
									Pipeline: &konfluxv1alpha1.TimeoutAdjustment{
										Increment:  "30m",
										MaxTimeout: "12h",
									},
									Task: &konfluxv1alpha1.TimeoutAdjustment{
										Increment:  "15m",
										MaxTimeout: "7h",
									},
								},
							},
						},
					},
				},
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		g.Expect(rscSpec).NotTo(gomega.BeNil())
		pipelines, ok := rscSpec["retryablePipelines"].([]interface{})
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(pipelines).To(gomega.HaveLen(1))

		first, _ := pipelines[0].(map[string]interface{})
		g.Expect(first["url"]).To(gomega.Equal("https://github.com/konflux-ci/release-service-catalog.git"))
		g.Expect(first["revision"]).To(gomega.Equal(".*"))
		g.Expect(first["pathInRepo"]).To(gomega.Equal("pipelines/managed/fbc-release/fbc-release.yaml"))

		policy, _ := first["retryPolicy"].(map[string]interface{})
		g.Expect(policy["maxRetries"]).To(gomega.Equal(int64(1)))

		disableOn, _ := policy["disableOn"].(map[string]interface{})
		tags, _ := disableOn["tags"].([]interface{})
		g.Expect(tags).To(gomega.HaveLen(1))

		mitigations, _ := policy["mitigations"].(map[string]interface{})
		oomKill, _ := mitigations["oomKill"].(map[string]interface{})
		g.Expect(oomKill["multiplier"]).To(gomega.Equal("2"))

		maxResources, _ := oomKill["maxComputeResources"].(map[string]interface{})
		limits, _ := maxResources["limits"].(map[string]interface{})
		g.Expect(limits["memory"]).To(gomega.Equal("8Gi"))
		requests, _ := maxResources["requests"].(map[string]interface{})
		g.Expect(requests["memory"]).To(gomega.Equal("8Gi"))

		timeout, _ := mitigations["timeout"].(map[string]interface{})
		pipelineTimeout, _ := timeout["pipeline"].(map[string]interface{})
		g.Expect(pipelineTimeout["increment"]).To(gomega.Equal("30m"))
		g.Expect(pipelineTimeout["maxTimeout"]).To(gomega.Equal("12h"))
		taskTimeout, _ := timeout["task"].(map[string]interface{})
		g.Expect(taskTimeout["increment"]).To(gomega.Equal("15m"))
		g.Expect(taskTimeout["maxTimeout"]).To(gomega.Equal("7h"))
	})

	t.Run("omits disableOn when tags are empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				RetryablePipelines: []konfluxv1alpha1.RetryablePipeline{
					{
						URL:        "https://example.com/repo.git",
						Revision:   "main",
						PathInRepo: "pipeline.yaml",
						RetryPolicy: konfluxv1alpha1.RetryPolicy{
							MaxRetries: 2,
							DisableOn:  &konfluxv1alpha1.RetryDisableOn{Tags: nil},
						},
					},
				},
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		pipelines, _ := rscSpec["retryablePipelines"].([]interface{})
		first, _ := pipelines[0].(map[string]interface{})
		policy, _ := first["retryPolicy"].(map[string]interface{})
		g.Expect(policy).To(gomega.HaveKey("disableOn"), "disableOn should be present as empty map when non-nil but tags are empty")
	})

	t.Run("omits mitigations sub-fields when both are nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				RetryablePipelines: []konfluxv1alpha1.RetryablePipeline{
					{
						URL:        "https://example.com/repo.git",
						Revision:   "main",
						PathInRepo: "pipeline.yaml",
						RetryPolicy: konfluxv1alpha1.RetryPolicy{
							MaxRetries:  1,
							Mitigations: &konfluxv1alpha1.RetryMitigations{},
						},
					},
				},
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		pipelines, _ := rscSpec["retryablePipelines"].([]interface{})
		first, _ := pipelines[0].(map[string]interface{})
		policy, _ := first["retryPolicy"].(map[string]interface{})
		g.Expect(policy).To(gomega.HaveKey("mitigations"), "mitigations should be present as empty map when non-nil but sub-fields are nil")
	})

	t.Run("omits timeout when both pipeline and task are nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				RetryablePipelines: []konfluxv1alpha1.RetryablePipeline{
					{
						URL:        "https://example.com/repo.git",
						Revision:   "main",
						PathInRepo: "pipeline.yaml",
						RetryPolicy: konfluxv1alpha1.RetryPolicy{
							MaxRetries: 1,
							Mitigations: &konfluxv1alpha1.RetryMitigations{
								OOMKill: &konfluxv1alpha1.OOMKillMitigation{Multiplier: "2"},
								Timeout: &konfluxv1alpha1.TimeoutMitigation{},
							},
						},
					},
				},
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		pipelines, _ := rscSpec["retryablePipelines"].([]interface{})
		first, _ := pipelines[0].(map[string]interface{})
		policy, _ := first["retryPolicy"].(map[string]interface{})
		mitigations, _ := policy["mitigations"].(map[string]interface{})
		g.Expect(mitigations).To(gomega.HaveKey("oomKill"), "oomKill should still be present")
		g.Expect(mitigations).To(gomega.HaveKey("timeout"), "timeout should be present as empty map when non-nil but sub-fields are nil")
	})

	t.Run("omits maxComputeResources when limits and requests are empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				RetryablePipelines: []konfluxv1alpha1.RetryablePipeline{
					{
						URL:        "https://example.com/repo.git",
						Revision:   "main",
						PathInRepo: "pipeline.yaml",
						RetryPolicy: konfluxv1alpha1.RetryPolicy{
							MaxRetries: 1,
							Mitigations: &konfluxv1alpha1.RetryMitigations{
								OOMKill: &konfluxv1alpha1.OOMKillMitigation{
									Multiplier:          "2",
									MaxComputeResources: &konfluxv1alpha1.ComputeResources{},
								},
							},
						},
					},
				},
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		pipelines, _ := rscSpec["retryablePipelines"].([]interface{})
		first, _ := pipelines[0].(map[string]interface{})
		policy, _ := first["retryPolicy"].(map[string]interface{})
		mitigations, _ := policy["mitigations"].(map[string]interface{})
		oomKill, _ := mitigations["oomKill"].(map[string]interface{})
		g.Expect(oomKill["multiplier"]).To(gomega.Equal("2"))
		g.Expect(oomKill).To(gomega.HaveKey("maxComputeResources"), "maxComputeResources should be present as empty map when non-nil but empty")
	})

	t.Run("removes retryablePipelines when not provided", func(t *testing.T) {
		g := gomega.NewWithT(t)
		rsc := getReleaseServiceConfig(t)
		rsc.Object["spec"] = map[string]interface{}{
			"debug": false,
			"retryablePipelines": []interface{}{
				map[string]interface{}{"url": ".*", "revision": ".*", "pathInRepo": "test.yaml",
					"retryPolicy": map[string]interface{}{"maxRetries": int64(1)}},
			},
		}

		spec := konfluxv1alpha1.KonfluxReleaseServiceSpec{
			KonfluxReleaseServiceConfigSpec: konfluxv1alpha1.KonfluxReleaseServiceConfigSpec{
				RetryablePipelines: nil,
			},
		}

		err := applyReleaseServiceConfigCustomizations(rsc, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rscSpec, _ := rsc.Object["spec"].(map[string]interface{})
		g.Expect(rscSpec).NotTo(gomega.BeNil())
		_, exists := rscSpec["retryablePipelines"]
		g.Expect(exists).To(gomega.BeFalse())
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
		managerContainer := kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
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

		managerContainer = kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("50m"))
		g.Expect(managerContainer.Resources.Requests.Memory().String()).To(gomega.Equal("64Mi"))
	})

	t.Run("merges requests without affecting limits", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Get deployment and set existing limits
		deployment := getReleaseServiceDeployment(t)
		managerContainer := kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
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

		managerContainer = kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, releaseManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
		g.Expect(managerContainer.Resources.Limits.Memory().String()).To(gomega.Equal("512Mi"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("100m"))
	})
}
