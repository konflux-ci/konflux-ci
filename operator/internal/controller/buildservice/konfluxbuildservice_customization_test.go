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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/hashedconfigmap"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const (
	testWebhookConfigMapName = "webhook-config-abc1234567"
	testCustomPaCURL         = "http://custom-pac.example.com:9999"
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

// findPaCWebhookURLEnvValue returns the last PAC_WEBHOOK_URL env var value.
// Returns the value and whether the env var was found. Checks the last match
// to match Kubernetes behavior where later env entries override earlier ones.
func findPaCWebhookURLEnvValue(envs []corev1.EnvVar) (string, bool) {
	var value string
	var found bool
	for _, env := range envs {
		if env.Name == pacWebhookURLEnvName {
			value = env.Value
			found = true
		}
	}
	return value, found
}

func TestBuildBuildControllerManagerOverlay(t *testing.T) {
	t.Run("empty spec returns overlay", func(t *testing.T) {
		g := gomega.NewWithT(t)
		overlay := buildBuildControllerManagerOverlay(konfluxv1alpha1.KonfluxBuildServiceSpec{}, nil, "")
		g.Expect(overlay).NotTo(gomega.BeNil())
	})

	t.Run("manager resources are applied", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
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
			},
		}

		deployment := getBuildServiceDeployment(t)
		overlay := buildBuildControllerManagerOverlay(spec, nil, "")
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

		deployment := getBuildServiceDeployment(t)
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil(), "manager container must exist in controller-manager deployment")
		originalImage := managerContainer.Image

		overlay := buildBuildControllerManagerOverlay(spec, nil, "")
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
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
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

		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
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
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
	})

	t.Run("handles empty spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
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
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
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
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
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
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
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
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
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

func newOpenShiftClusterInfo(t *testing.T) *clusterinfo.Info {
	t.Helper()
	info, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			"config.openshift.io/v1": {APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}}},
		},
	})
	if err != nil {
		t.Fatalf("failed to create OpenShift cluster info: %v", err)
	}
	return info
}

func newDefaultClusterInfo(t *testing.T) *clusterinfo.Info {
	t.Helper()
	info, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{},
	})
	if err != nil {
		t.Fatalf("failed to create default cluster info: %v", err)
	}
	return info
}

type mockDiscoveryClient struct {
	resources map[string]*metav1.APIResourceList
}

func (m *mockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if rl, ok := m.resources[groupVersion]; ok {
		return rl, nil
	}
	return nil, errors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *mockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return &version.Info{}, nil
}

func TestApplyBuildServiceDeploymentCustomizations_PaCWebhookURL(t *testing.T) {
	t.Run("sets PAC_WEBHOOK_URL on non-OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, newDefaultClusterInfo(t), "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		pacURL, found := findPaCWebhookURLEnvValue(managerContainer.Env)
		g.Expect(found).To(gomega.BeTrue(), "PAC_WEBHOOK_URL should be set on non-OpenShift")
		g.Expect(pacURL).To(gomega.Equal(pacWebhookURLNonOpenShift))
	})

	t.Run("does not set PAC_WEBHOOK_URL on OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, newOpenShiftClusterInfo(t), "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		_, found := findPaCWebhookURLEnvValue(managerContainer.Env)
		g.Expect(found).To(gomega.BeFalse(), "PAC_WEBHOOK_URL should NOT be set on OpenShift (auto-discover Route)")
	})

	t.Run("CR env override takes precedence on non-OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)
		customURL := testCustomPaCURL
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: pacWebhookURLEnvName, Value: customURL},
					},
				},
			},
		}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, newDefaultClusterInfo(t), "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		pacURL, found := findPaCWebhookURLEnvValue(managerContainer.Env)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(pacURL).To(gomega.Equal(customURL))
	})

	t.Run("CR env override works on OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)
		customURL := testCustomPaCURL
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: pacWebhookURLEnvName, Value: customURL},
					},
				},
			},
		}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, newOpenShiftClusterInfo(t), "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		pacURL, found := findPaCWebhookURLEnvValue(managerContainer.Env)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(pacURL).To(gomega.Equal(customURL))
	})

	t.Run("nil ClusterInfo sets non-OpenShift default", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		pacURL, found := findPaCWebhookURLEnvValue(managerContainer.Env)
		g.Expect(found).To(gomega.BeTrue(), "nil ClusterInfo should be treated as non-OpenShift")
		g.Expect(pacURL).To(gomega.Equal(pacWebhookURLNonOpenShift))
	})
}

func TestApplyBuildServiceDeploymentCustomizations_WebhookConfig(t *testing.T) {
	t.Run("updates volume reference to hashed ConfigMap", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{}
		configMapName := testWebhookConfigMapName

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, configMapName)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		var volFound bool
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.Name == webhookConfigVolName && vol.ConfigMap != nil && vol.ConfigMap.Name == configMapName {
				volFound = true
				break
			}
		}
		g.Expect(volFound).To(gomega.BeTrue(), "webhook-config volume should reference the hashed ConfigMap")
	})

	t.Run("keeps placeholder name when webhook config is empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{}

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Volume should exist from the manifest with the placeholder name
		var volFound bool
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.Name == webhookConfigVolName && vol.ConfigMap != nil {
				g.Expect(vol.ConfigMap.Name).To(gomega.Equal(webhookConfigBaseName),
					"volume should keep the placeholder ConfigMap name")
				g.Expect(*vol.ConfigMap.Optional).To(gomega.BeTrue(),
					"volume should be optional so the pod starts without a ConfigMap")
				volFound = true
				break
			}
		}
		g.Expect(volFound).To(gomega.BeTrue(), "webhook-config volume should be present from manifest")
	})

	t.Run("different hashed names produce different volume references", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{}

		content1 := `{"https://github.com":"https://smee.example.com/hook1"}`
		content2 := `{"https://github.com":"https://smee.example.com/hook2"}`

		name1 := hashedconfigmap.BuildConfigMapName(webhookConfigBaseName, content1)
		name2 := hashedconfigmap.BuildConfigMapName(webhookConfigBaseName, content2)
		g.Expect(name1).NotTo(gomega.Equal(name2))

		deployment1 := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment1, spec, nil, name1)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		deployment2 := getBuildServiceDeployment(t)
		err = applyBuildServiceDeploymentCustomizations(deployment2, spec, nil, name2)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		var vol1Name, vol2Name string
		for _, vol := range deployment1.Spec.Template.Spec.Volumes {
			if vol.Name == webhookConfigVolName {
				vol1Name = vol.ConfigMap.Name
			}
		}
		for _, vol := range deployment2.Spec.Template.Spec.Volumes {
			if vol.Name == webhookConfigVolName {
				vol2Name = vol.ConfigMap.Name
			}
		}
		g.Expect(vol1Name).To(gomega.Equal(name1))
		g.Expect(vol2Name).To(gomega.Equal(name2))
	})

	t.Run("re-applying with new hashed name does not duplicate volumes", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{}

		content1 := `{"https://github.com":"https://smee.example.com/hook1"}`
		content2 := `{"https://github.com":"https://smee.example.com/hook2"}`

		name1 := hashedconfigmap.BuildConfigMapName(webhookConfigBaseName, content1)
		name2 := hashedconfigmap.BuildConfigMapName(webhookConfigBaseName, content2)
		g.Expect(name1).NotTo(gomega.Equal(name2))

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, name1)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		err = applyBuildServiceDeploymentCustomizations(deployment, spec, nil, name2)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		var webhookVolCount int
		var webhookVolCMName string
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.Name == webhookConfigVolName {
				webhookVolCount++
				g.Expect(vol.ConfigMap).NotTo(gomega.BeNil())
				webhookVolCMName = vol.ConfigMap.Name
			}
		}
		g.Expect(webhookVolCount).To(gomega.Equal(1), "should not create duplicate webhook-config volumes")
		g.Expect(webhookVolCMName).To(gomega.Equal(name2), "should reference the latest hashed ConfigMap name")
	})

	t.Run("webhook config works with nil spec on OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{}
		configMapName := testWebhookConfigMapName

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, newOpenShiftClusterInfo(t), configMapName)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Volume should reference the hashed ConfigMap
		var volFound bool
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.Name == webhookConfigVolName && vol.ConfigMap != nil && vol.ConfigMap.Name == configMapName {
				volFound = true
				break
			}
		}
		g.Expect(volFound).To(gomega.BeTrue(), "webhook-config volume should reference the hashed ConfigMap")

		// PAC_WEBHOOK_URL should NOT be set on OpenShift
		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		_, found := findPaCWebhookURLEnvValue(managerContainer.Env)
		g.Expect(found).To(gomega.BeFalse())
	})

	t.Run("does not set PAC_WEBHOOK_URL when webhookURLs configured on non-OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			WebhookURLs: map[string]string{
				"https://gitlab.com": "https://smee.example.com/hook",
			},
		}
		configMapName := testWebhookConfigMapName

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, newDefaultClusterInfo(t), configMapName)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		_, found := findPaCWebhookURLEnvValue(managerContainer.Env)
		g.Expect(found).To(gomega.BeFalse(), "PAC_WEBHOOK_URL should NOT be set when webhookURLs are configured")
	})

	t.Run("user PAC_WEBHOOK_URL override with webhookURLs on non-OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)
		customURL := testCustomPaCURL
		spec := konfluxv1alpha1.KonfluxBuildServiceSpec{
			BuildControllerManager: &konfluxv1alpha1.ControllerManagerDeploymentSpec{
				Manager: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: pacWebhookURLEnvName, Value: customURL},
					},
				},
			},
			WebhookURLs: map[string]string{
				"https://gitlab.com": "https://smee.example.com/hook",
			},
		}
		configMapName := testWebhookConfigMapName

		deployment := getBuildServiceDeployment(t)
		err := applyBuildServiceDeploymentCustomizations(deployment, spec, newDefaultClusterInfo(t), configMapName)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())

		// User-specified PAC_WEBHOOK_URL is set even when webhookURLs are configured
		pacURL, found := findPaCWebhookURLEnvValue(managerContainer.Env)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(pacURL).To(gomega.Equal(customURL))

		// Webhook config volume should reference the hashed ConfigMap
		var volFound bool
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.Name == webhookConfigVolName && vol.ConfigMap != nil && vol.ConfigMap.Name == configMapName {
				volFound = true
				break
			}
		}
		g.Expect(volFound).To(gomega.BeTrue())
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

		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
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

		err := applyBuildServiceDeploymentCustomizations(deployment, spec, nil, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		managerContainer = testutil.FindContainer(deployment.Spec.Template.Spec.Containers, buildManagerContainerName)
		g.Expect(managerContainer).NotTo(gomega.BeNil())
		g.Expect(managerContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
		g.Expect(managerContainer.Resources.Limits.Memory().String()).To(gomega.Equal("512Mi"))
		g.Expect(managerContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("100m"))
	})
}
