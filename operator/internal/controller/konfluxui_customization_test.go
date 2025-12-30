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

package controller

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
	"github.com/konflux-ci/konflux-ci/operator/pkg/customization"
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

// getUIDeployment returns a deep copy of a UI deployment from the manifests.
func getUIDeployment(t *testing.T, name string) *appsv1.Deployment {
	t.Helper()
	store := getTestObjectStore(t)
	objects, err := store.GetForComponent(manifests.UI)
	if err != nil {
		t.Fatalf("failed to get UI manifests: %v", err)
	}

	for _, obj := range objects {
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if deployment.Name == name {
				return deployment
			}
		}
	}
	t.Fatalf("deployment %q not found in UI manifests", name)
	return nil
}

// requiredOAuth2ProxyEnvVars are the environment variables that must be set for oauth2-proxy.
var requiredOAuth2ProxyEnvVars = []string{
	"OAUTH2_PROXY_PROVIDER",
	"OAUTH2_PROXY_REDIRECT_URL",
	"OAUTH2_PROXY_OIDC_ISSUER_URL",
	"OAUTH2_PROXY_LOGIN_URL",
	"OAUTH2_PROXY_SKIP_OIDC_DISCOVERY",
	"OAUTH2_PROXY_REDEEM_URL",
	"OAUTH2_PROXY_OIDC_JWKS_URL",
	"OAUTH2_PROXY_COOKIE_SECURE",
	"OAUTH2_PROXY_COOKIE_NAME",
	"OAUTH2_PROXY_EMAIL_DOMAINS",
	"OAUTH2_PROXY_SET_XAUTHREQUEST",
	"OAUTH2_PROXY_SKIP_JWT_BEARER_TOKENS",
	"OAUTH2_PROXY_SSL_INSECURE_SKIP_VERIFY",
	"OAUTH2_PROXY_WHITELIST_DOMAINS",
}

// assertOAuth2ProxyEnvVarsSet verifies that all required oauth2-proxy env vars are present.
func assertOAuth2ProxyEnvVarsSet(g *gomega.WithT, container *corev1.Container) {
	envMap := make(map[string]string)
	for _, env := range container.Env {
		envMap[env.Name] = env.Value
	}
	for _, key := range requiredOAuth2ProxyEnvVars {
		g.Expect(envMap).To(gomega.HaveKey(key), "missing required env var: %s", key)
	}
}

// assertNoConflictingEnvVars verifies no env var has both value and valueFrom set.
func assertNoConflictingEnvVars(g *gomega.WithT, container *corev1.Container) {
	for _, env := range container.Env {
		hasValue := env.Value != ""
		hasValueFrom := env.ValueFrom != nil
		g.Expect(hasValue && hasValueFrom).To(gomega.BeFalse(),
			"env var %s has both value (%q) and valueFrom set - this is invalid", env.Name, env.Value)
	}
}

func TestBuildProxyOverlay(t *testing.T) {
	t.Run("nil spec returns overlay with oauth2-proxy config", func(t *testing.T) {
		g := gomega.NewWithT(t)
		overlay := buildProxyOverlay(nil, buildOAuth2ProxyOptions("localhost", "9443")...)
		g.Expect(overlay).NotTo(gomega.BeNil())

		deployment := getUIDeployment(t, proxyDeploymentName)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := findContainer(deployment.Spec.Template.Spec.Containers, oauth2ProxyContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		assertNoConflictingEnvVars(g, container)
		assertOAuth2ProxyEnvVarsSet(g, container)
	})

	t.Run("empty spec returns overlay with oauth2-proxy config", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ProxyDeploymentSpec{}
		overlay := buildProxyOverlay(spec, buildOAuth2ProxyOptions("localhost", "9443")...)
		g.Expect(overlay).NotTo(gomega.BeNil())

		deployment := getUIDeployment(t, proxyDeploymentName)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := findContainer(deployment.Spec.Template.Spec.Containers, oauth2ProxyContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		assertNoConflictingEnvVars(g, container)
		assertOAuth2ProxyEnvVarsSet(g, container)
	})

	t.Run("nginx resources are applied", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ProxyDeploymentSpec{
			Nginx: &konfluxv1alpha1.ContainerSpec{
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

		deployment := getUIDeployment(t, proxyDeploymentName)
		overlay := buildProxyOverlay(spec, buildOAuth2ProxyOptions("localhost", "9443")...)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		nginxContainer := findContainer(deployment.Spec.Template.Spec.Containers, nginxContainerName)
		g.Expect(nginxContainer).NotTo(gomega.BeNil())
		g.Expect(nginxContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		g.Expect(nginxContainer.Resources.Limits.Memory().String()).To(gomega.Equal("256Mi"))
		g.Expect(nginxContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("100m"))
		g.Expect(nginxContainer.Resources.Requests.Memory().String()).To(gomega.Equal("128Mi"))
	})

	t.Run("oauth2-proxy resources are applied", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ProxyDeploymentSpec{
			OAuth2Proxy: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			},
		}

		deployment := getUIDeployment(t, proxyDeploymentName)
		overlay := buildProxyOverlay(spec, buildOAuth2ProxyOptions("localhost", "9443")...)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := findContainer(deployment.Spec.Template.Spec.Containers, oauth2ProxyContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		g.Expect(container.Resources.Limits.Cpu().String()).To(gomega.Equal("200m"))
		g.Expect(container.Resources.Limits.Memory().String()).To(gomega.Equal("128Mi"))

		// Verify required env vars are set
		assertOAuth2ProxyEnvVarsSet(g, container)
	})

	t.Run("both containers can be customized", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ProxyDeploymentSpec{
			Nginx: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("1"),
					},
				},
			},
			OAuth2Proxy: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
					},
				},
			},
		}

		deployment := getUIDeployment(t, proxyDeploymentName)
		overlay := buildProxyOverlay(spec, buildOAuth2ProxyOptions("localhost", "9443")...)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		nginxContainer := findContainer(deployment.Spec.Template.Spec.Containers, nginxContainerName)
		g.Expect(nginxContainer).NotTo(gomega.BeNil())
		g.Expect(nginxContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))

		oauth2Container := findContainer(deployment.Spec.Template.Spec.Containers, oauth2ProxyContainerName)
		g.Expect(oauth2Container).NotTo(gomega.BeNil())
		g.Expect(oauth2Container.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))

		// Verify required env vars are set
		assertOAuth2ProxyEnvVarsSet(g, oauth2Container)
	})

	t.Run("preserves existing container fields", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.ProxyDeploymentSpec{
			Nginx: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("500m"),
					},
				},
			},
		}

		deployment := getUIDeployment(t, proxyDeploymentName)
		nginxContainer := findContainer(deployment.Spec.Template.Spec.Containers, nginxContainerName)
		g.Expect(nginxContainer).NotTo(gomega.BeNil(), "nginx container must exist in proxy deployment")
		originalImage := nginxContainer.Image

		overlay := buildProxyOverlay(spec, buildOAuth2ProxyOptions("localhost", "9443")...)
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		nginxContainer = findContainer(deployment.Spec.Template.Spec.Containers, nginxContainerName)
		g.Expect(nginxContainer).NotTo(gomega.BeNil())
		g.Expect(nginxContainer.Image).To(gomega.Equal(originalImage))
	})
}

func TestBuildOAuth2ProxyOptions(t *testing.T) {
	t.Run("returns default options for empty spec", func(t *testing.T) {
		g := gomega.NewWithT(t)

		opts := buildOAuth2ProxyOptions("localhost", "9443")

		// Apply options to a container to verify
		c := &corev1.Container{}
		for _, opt := range opts {
			opt(c, customization.DeploymentContext{})
		}

		envMap := make(map[string]string)
		for _, env := range c.Env {
			envMap[env.Name] = env.Value
		}

		// Verify default hostname/port are used
		g.Expect(envMap["OAUTH2_PROXY_REDIRECT_URL"]).To(gomega.Equal("https://localhost:9443/oauth2/callback"))
		g.Expect(envMap["OAUTH2_PROXY_WHITELIST_DOMAINS"]).To(gomega.Equal("localhost:9443"))
	})

	t.Run("returns all required options", func(t *testing.T) {
		g := gomega.NewWithT(t)

		opts := buildOAuth2ProxyOptions("localhost", "9443")

		// Apply options to a container
		c := &corev1.Container{}
		for _, opt := range opts {
			opt(c, customization.DeploymentContext{})
		}

		envMap := make(map[string]string)
		for _, env := range c.Env {
			envMap[env.Name] = env.Value
		}

		// Verify all required env vars are present
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_PROVIDER"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_REDIRECT_URL"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_OIDC_ISSUER_URL"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_LOGIN_URL"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_SKIP_OIDC_DISCOVERY"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_REDEEM_URL"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_OIDC_JWKS_URL"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_COOKIE_SECURE"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_COOKIE_NAME"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_EMAIL_DOMAINS"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_SET_XAUTHREQUEST"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_SKIP_JWT_BEARER_TOKENS"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_SSL_INSECURE_SKIP_VERIFY"))
		g.Expect(envMap).To(gomega.HaveKey("OAUTH2_PROXY_WHITELIST_DOMAINS"))
	})
}

func TestBuildDexOverlay(t *testing.T) {
	t.Run("nil spec returns overlay with configmap update", func(t *testing.T) {
		g := gomega.NewWithT(t)
		overlay := buildDexOverlay(nil, "dex-config-abc123")
		g.Expect(overlay).NotTo(gomega.BeNil())
	})

	t.Run("empty spec returns overlay with configmap update", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.DexDeploymentSpec{}
		overlay := buildDexOverlay(spec, "dex-config-abc123")
		g.Expect(overlay).NotTo(gomega.BeNil())
	})

	t.Run("dex resources are applied", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.DexDeploymentSpec{
			Dex: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("300m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
				},
			},
		}

		deployment := getUIDeployment(t, dexDeploymentName)
		overlay := buildDexOverlay(spec, "dex-config-abc123")
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		dexContainer := findContainer(deployment.Spec.Template.Spec.Containers, dexContainerName)
		g.Expect(dexContainer).NotTo(gomega.BeNil())
		g.Expect(dexContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("300m"))
		g.Expect(dexContainer.Resources.Limits.Memory().String()).To(gomega.Equal("512Mi"))
		g.Expect(dexContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("50m"))
		g.Expect(dexContainer.Resources.Requests.Memory().String()).To(gomega.Equal("64Mi"))
	})

	t.Run("preserves existing container fields", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.DexDeploymentSpec{
			Dex: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
				},
			},
		}

		deployment := getUIDeployment(t, dexDeploymentName)
		dexContainer := findContainer(deployment.Spec.Template.Spec.Containers, dexContainerName)
		g.Expect(dexContainer).NotTo(gomega.BeNil(), "dex container must exist in dex deployment")
		originalImage := dexContainer.Image
		originalArgs := dexContainer.Args

		overlay := buildDexOverlay(spec, "dex-config-abc123")
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		dexContainer = findContainer(deployment.Spec.Template.Spec.Containers, dexContainerName)
		g.Expect(dexContainer).NotTo(gomega.BeNil())
		g.Expect(dexContainer.Image).To(gomega.Equal(originalImage))
		g.Expect(dexContainer.Args).To(gomega.Equal(originalArgs))
	})

	t.Run("updates configmap volume reference", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getUIDeployment(t, dexDeploymentName)

		overlay := buildDexOverlay(nil, "dex-newconfig-xyz789")
		err := overlay.ApplyToDeployment(deployment)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Find the dex volume and verify ConfigMap name was updated
		var dexVolume *corev1.Volume
		for i := range deployment.Spec.Template.Spec.Volumes {
			if deployment.Spec.Template.Spec.Volumes[i].Name == dexConfigMapVolumeName {
				dexVolume = &deployment.Spec.Template.Spec.Volumes[i]
				break
			}
		}
		g.Expect(dexVolume).NotTo(gomega.BeNil(), "dex volume must exist")
		g.Expect(dexVolume.ConfigMap).NotTo(gomega.BeNil())
		g.Expect(dexVolume.ConfigMap.Name).To(gomega.Equal("dex-newconfig-xyz789"))
	})
}

func TestApplyUIDeploymentCustomizations(t *testing.T) {
	const testConfigMapName = "dex-config-test123"

	t.Run("applies customizations to proxy deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{
			Proxy: &konfluxv1alpha1.ProxyDeploymentSpec{
				Nginx: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
				},
			},
		}

		deployment := getUIDeployment(t, proxyDeploymentName)
		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		nginxContainer := findContainer(deployment.Spec.Template.Spec.Containers, nginxContainerName)
		g.Expect(nginxContainer).NotTo(gomega.BeNil())
		g.Expect(nginxContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
	})

	t.Run("applies customizations to dex deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{
			Dex: &konfluxv1alpha1.DexDeploymentSpec{
				Dex: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
		}

		deployment := getUIDeployment(t, dexDeploymentName)
		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		dexContainer := findContainer(deployment.Spec.Template.Spec.Containers, dexContainerName)
		g.Expect(dexContainer).NotTo(gomega.BeNil())
		g.Expect(dexContainer.Resources.Limits.Memory().String()).To(gomega.Equal("512Mi"))
	})

	t.Run("ignores unknown deployment names", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{
			Proxy: &konfluxv1alpha1.ProxyDeploymentSpec{
				Nginx: &konfluxv1alpha1.ContainerSpec{
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

		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic and container should be unchanged
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Resources.Limits).To(gomega.BeNil())
	})

	t.Run("handles nil proxy spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{
			Proxy: nil,
		}

		deployment := getUIDeployment(t, proxyDeploymentName)
		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		nginxContainer := findContainer(deployment.Spec.Template.Spec.Containers, nginxContainerName)
		g.Expect(nginxContainer).NotTo(gomega.BeNil())
	})

	t.Run("handles nil dex spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{
			Dex: nil,
		}

		deployment := getUIDeployment(t, dexDeploymentName)
		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		dexContainer := findContainer(deployment.Spec.Template.Spec.Containers, dexContainerName)
		g.Expect(dexContainer).NotTo(gomega.BeNil())
	})

	t.Run("handles empty spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{}

		deployment := getUIDeployment(t, proxyDeploymentName)
		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Should not panic
		g.Expect(deployment.Spec.Template.Spec.Containers).NotTo(gomega.BeEmpty())
	})

	t.Run("applies replicas to proxy deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{
			Proxy: &konfluxv1alpha1.ProxyDeploymentSpec{
				Replicas: 3,
			},
		}

		deployment := getUIDeployment(t, proxyDeploymentName)
		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(3)))
	})

	t.Run("applies replicas to dex deployment", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{
			Dex: &konfluxv1alpha1.DexDeploymentSpec{
				Replicas: 2,
			},
		}

		deployment := getUIDeployment(t, dexDeploymentName)
		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(2)))
	})

	t.Run("applies default replicas when using default value", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{
			Proxy: &konfluxv1alpha1.ProxyDeploymentSpec{
				Replicas: 1, // default value
			},
		}

		deployment := getUIDeployment(t, proxyDeploymentName)
		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(1)))
	})

	t.Run("does not modify replicas when proxy spec is nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{
			Proxy: nil,
		}

		deployment := getUIDeployment(t, proxyDeploymentName)
		originalReplicas := deployment.Spec.Replicas
		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		g.Expect(deployment.Spec.Replicas).To(gomega.Equal(originalReplicas))
	})

	t.Run("applies replicas together with container resources", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{
			Proxy: &konfluxv1alpha1.ProxyDeploymentSpec{
				Replicas: 5,
				Nginx: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("2"),
						},
					},
				},
			},
		}

		deployment := getUIDeployment(t, proxyDeploymentName)
		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Check replicas
		g.Expect(deployment.Spec.Replicas).NotTo(gomega.BeNil())
		g.Expect(*deployment.Spec.Replicas).To(gomega.Equal(int32(5)))

		// Check container resources
		nginxContainer := findContainer(deployment.Spec.Template.Spec.Containers, nginxContainerName)
		g.Expect(nginxContainer).NotTo(gomega.BeNil())
		g.Expect(nginxContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("2"))
	})

	t.Run("updates dex configmap volume reference", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxUISpec{}

		deployment := getUIDeployment(t, dexDeploymentName)
		err := applyUIDeploymentCustomizations(deployment, spec, "dex-custom-config-abc", "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Find the dex volume and verify ConfigMap name was updated
		var dexVolume *corev1.Volume
		for i := range deployment.Spec.Template.Spec.Volumes {
			if deployment.Spec.Template.Spec.Volumes[i].Name == dexConfigMapVolumeName {
				dexVolume = &deployment.Spec.Template.Spec.Volumes[i]
				break
			}
		}
		g.Expect(dexVolume).NotTo(gomega.BeNil(), "dex volume must exist")
		g.Expect(dexVolume.ConfigMap).NotTo(gomega.BeNil())
		g.Expect(dexVolume.ConfigMap.Name).To(gomega.Equal("dex-custom-config-abc"))
	})
}

func TestApplyUIDeploymentCustomizations_ResourceMerging(t *testing.T) {
	const testConfigMapName = "dex-config-test123"

	t.Run("merges limits without affecting requests", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Get deployment and set existing requests
		deployment := getUIDeployment(t, proxyDeploymentName)
		nginxContainer := findContainer(deployment.Spec.Template.Spec.Containers, nginxContainerName)
		g.Expect(nginxContainer).NotTo(gomega.BeNil(), "nginx container must exist")
		nginxContainer.Resources.Requests = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		}

		spec := konfluxv1alpha1.KonfluxUISpec{
			Proxy: &konfluxv1alpha1.ProxyDeploymentSpec{
				Nginx: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				},
			},
		}

		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		nginxContainer = findContainer(deployment.Spec.Template.Spec.Containers, nginxContainerName)
		g.Expect(nginxContainer).NotTo(gomega.BeNil())
		g.Expect(nginxContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		g.Expect(nginxContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("50m"))
		g.Expect(nginxContainer.Resources.Requests.Memory().String()).To(gomega.Equal("64Mi"))
	})

	t.Run("merges requests without affecting limits", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Get deployment and set existing limits
		deployment := getUIDeployment(t, proxyDeploymentName)
		nginxContainer := findContainer(deployment.Spec.Template.Spec.Containers, nginxContainerName)
		g.Expect(nginxContainer).NotTo(gomega.BeNil(), "nginx container must exist")
		nginxContainer.Resources.Limits = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}

		spec := konfluxv1alpha1.KonfluxUISpec{
			Proxy: &konfluxv1alpha1.ProxyDeploymentSpec{
				Nginx: &konfluxv1alpha1.ContainerSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
					},
				},
			},
		}

		err := applyUIDeploymentCustomizations(deployment, spec, testConfigMapName, "localhost", "9443")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		nginxContainer = findContainer(deployment.Spec.Template.Spec.Containers, nginxContainerName)
		g.Expect(nginxContainer).NotTo(gomega.BeNil())
		g.Expect(nginxContainer.Resources.Limits.Cpu().String()).To(gomega.Equal("1"))
		g.Expect(nginxContainer.Resources.Limits.Memory().String()).To(gomega.Equal("512Mi"))
		g.Expect(nginxContainer.Resources.Requests.Cpu().String()).To(gomega.Equal("100m"))
	})
}

// Helper functions

func findContainer(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}
