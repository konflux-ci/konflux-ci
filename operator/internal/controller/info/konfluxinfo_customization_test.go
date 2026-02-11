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

package info

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/pkg/version"
)

func TestKonfluxInfoReconciliation(t *testing.T) {
	// Skip if k8sClient is not initialized (tests need to run as part of Ginkgo suite)
	if k8sClient == nil || objectStore == nil {
		t.Skip("Skipping test: k8sClient or objectStore not initialized. Run tests via Ginkgo suite.")
	}

	ctx := context.Background()
	typeNamespacedName := types.NamespacedName{
		Name: CRName,
	}

	t.Run("should create the konflux-info namespace", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo resource
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify namespace was created
		namespace := &corev1.Namespace{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: infoNamespace}, namespace)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(namespace.Name).To(gomega.Equal(infoNamespace))
	})

	t.Run("should create default ConfigMaps with correct content", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo resource
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify info.json ConfigMap
		infoConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      infoConfigMapName,
			Namespace: infoNamespace,
		}, infoConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(infoConfigMap.Data).To(gomega.HaveKey("info.json"))

		// Verify info.json contains default values
		var infoJSON map[string]interface{}
		err = json.Unmarshal([]byte(infoConfigMap.Data["info.json"]), &infoJSON)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(infoJSON["environment"]).To(gomega.Equal("development"))
		g.Expect(infoJSON["visibility"]).To(gomega.Equal("public"))
		g.Expect(infoJSON).To(gomega.HaveKey("konfluxVersion"))
		g.Expect(infoJSON["konfluxVersion"]).To(
			gomega.Equal(version.Version), "ConfigMap konfluxVersion must match version package")
		g.Expect(infoJSON).To(gomega.HaveKey("rbac"))

		// Verify banner ConfigMap
		bannerConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      bannerConfigMapName,
			Namespace: infoNamespace,
		}, bannerConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(bannerConfigMap.Data).To(gomega.HaveKey("banner-content.yaml"))
		g.Expect(bannerConfigMap.Data["banner-content.yaml"]).NotTo(gomega.BeEmpty())
	})

	t.Run("should create Role and RoleBinding", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo resource
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify Role
		role := &rbacv1.Role{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      "konflux-public-info-view-role",
			Namespace: infoNamespace,
		}, role)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(role.Rules).ToNot(gomega.BeEmpty())

		// Verify RoleBinding
		roleBinding := &rbacv1.RoleBinding{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      "konflux-public-info-view-rb",
			Namespace: infoNamespace,
		}, roleBinding)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(roleBinding.RoleRef.Name).To(gomega.Equal("konflux-public-info-view-role"))
	})

	t.Run("should set Ready condition after successful reconciliation", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo resource
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify Ready condition
		updatedInfo := &konfluxv1alpha1.KonfluxInfo{}
		err = k8sClient.Get(ctx, typeNamespacedName, updatedInfo)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		conditions := updatedInfo.GetConditions()
		readyCondition := findCondition(conditions, condition.TypeReady)
		g.Expect(readyCondition).NotTo(gomega.BeNil())
		g.Expect(readyCondition.Status).To(gomega.Equal(metav1.ConditionTrue))
	})
}

func TestKonfluxInfoPublicInfoCustomization(t *testing.T) {
	// Skip if k8sClient is not initialized (tests need to run as part of Ginkgo suite)
	if k8sClient == nil || objectStore == nil {
		t.Skip("Skipping test: k8sClient or objectStore not initialized. Run tests via Ginkgo suite.")
	}

	ctx := context.Background()
	typeNamespacedName := types.NamespacedName{
		Name: CRName,
	}

	t.Run("should create ConfigMap with custom environment and visibility", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo with custom PublicInfo
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				PublicInfo: &konfluxv1alpha1.PublicInfo{
					Environment:   "production",
					Visibility:    "private",
					StatusPageUrl: "https://status.example.com",
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify info.json contains custom values
		infoConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      infoConfigMapName,
			Namespace: infoNamespace,
		}, infoConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		var infoJSON map[string]interface{}
		err = json.Unmarshal([]byte(infoConfigMap.Data["info.json"]), &infoJSON)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(infoJSON["environment"]).To(gomega.Equal("production"))
		g.Expect(infoJSON["visibility"]).To(gomega.Equal("private"))
		g.Expect(infoJSON["statusPageUrl"]).To(gomega.Equal("https://status.example.com"))
	})

	t.Run("should create ConfigMap with custom RBAC roles", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo with custom RBAC
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				PublicInfo: &konfluxv1alpha1.PublicInfo{
					Environment: "development",
					Visibility:  "public",
					RBAC: []konfluxv1alpha1.RBACRole{
						{
							Name:        "konflux-admin-user-actions",
							Description: "Full access to Konflux resources",
							DisplayName: "admin",
						},
						{
							Name:        "konflux-custom-role",
							Description: "Custom role description",
						},
					},
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify info.json contains custom RBAC roles
		infoConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      infoConfigMapName,
			Namespace: infoNamespace,
		}, infoConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		var infoJSON map[string]interface{}
		err = json.Unmarshal([]byte(infoConfigMap.Data["info.json"]), &infoJSON)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		rbacArray, ok := infoJSON["rbac"].([]interface{})
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(rbacArray).To(gomega.HaveLen(2))

		firstRole := rbacArray[0].(map[string]interface{})
		g.Expect(firstRole["displayName"]).To(gomega.Equal("admin"))
		g.Expect(firstRole["description"]).To(gomega.Equal("Full access to Konflux resources"))
		roleRef := firstRole["roleRef"].(map[string]interface{})
		g.Expect(roleRef["name"]).To(gomega.Equal("konflux-admin-user-actions"))
		g.Expect(roleRef["apiGroup"]).To(gomega.Equal("rbac.authorization.k8s.io"))
		g.Expect(roleRef["kind"]).To(gomega.Equal("ClusterRole"))

		secondRole := rbacArray[1].(map[string]interface{})
		g.Expect(secondRole["displayName"]).To(gomega.Equal("konflux-custom-role")) // Defaults to name
		g.Expect(secondRole["description"]).To(gomega.Equal("Custom role description"))
	})

	t.Run("should create ConfigMap with integrations configuration", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo with integrations
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				PublicInfo: &konfluxv1alpha1.PublicInfo{
					Environment: "development",
					Visibility:  "public",
					Integrations: &konfluxv1alpha1.IntegrationsConfig{
						GitHub: &konfluxv1alpha1.GitHubIntegration{
							ApplicationURL: "https://github.com/apps/my-konflux-app",
						},
						SBOMServer: &konfluxv1alpha1.SBOMServerConfig{
							URL:     "https://sbom.example.com/content",
							SBOMSha: "https://sbom.example.com/sha",
						},
						ImageController: &konfluxv1alpha1.InfoImageControllerConfig{
							Enabled: true,
							Notifications: []konfluxv1alpha1.InfoNotificationConfig{
								{
									Title:  "Build Notification",
									Event:  "build_complete",
									Method: "webhook",
									Config: apiextensionsv1.JSON{
										Raw: []byte(`{"url":"https://webhook.example.com/build"}`),
									},
								},
							},
						},
					},
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify info.json contains integrations
		infoConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      infoConfigMapName,
			Namespace: infoNamespace,
		}, infoConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		var infoJSON map[string]interface{}
		err = json.Unmarshal([]byte(infoConfigMap.Data["info.json"]), &infoJSON)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		integrations, ok := infoJSON["integrations"].(map[string]interface{})
		g.Expect(ok).To(gomega.BeTrue())

		github, ok := integrations["github"].(map[string]interface{})
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(github["application_url"]).To(gomega.Equal("https://github.com/apps/my-konflux-app"))

		sbomServer, ok := integrations["sbom_server"].(map[string]interface{})
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(sbomServer["url"]).To(gomega.Equal("https://sbom.example.com/content"))
		g.Expect(sbomServer["sbom_sha"]).To(gomega.Equal("https://sbom.example.com/sha"))

		imageController, ok := integrations["image_controller"].(map[string]interface{})
		g.Expect(ok).To(gomega.BeTrue())
		g.Expect(imageController["enabled"]).To(gomega.BeTrue())
	})
}

func TestKonfluxInfoBannerCustomization(t *testing.T) {
	// Skip if k8sClient is not initialized (tests need to run as part of Ginkgo suite)
	if k8sClient == nil || objectStore == nil {
		t.Skip("Skipping test: k8sClient or objectStore not initialized. Run tests via Ginkgo suite.")
	}

	ctx := context.Background()
	typeNamespacedName := types.NamespacedName{
		Name: CRName,
	}

	t.Run("should create ConfigMap with custom banner", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo with custom Banner
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				Banner: &konfluxv1alpha1.Banner{
					Items: &[]konfluxv1alpha1.BannerItem{
						{
							Summary: "This is a production environment. Please be careful.",
							Type:    "warning",
						},
					},
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify banner ConfigMap contains custom banner
		bannerConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      bannerConfigMapName,
			Namespace: infoNamespace,
		}, bannerConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		bannerContent := bannerConfigMap.Data["banner-content.yaml"]
		g.Expect(bannerContent).To(gomega.ContainSubstring("This is a production environment"))
		g.Expect(bannerContent).To(gomega.ContainSubstring("warning"))
	})

	t.Run("should create ConfigMap with multiple banners", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo with multiple banners
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				Banner: &konfluxv1alpha1.Banner{
					Items: &[]konfluxv1alpha1.BannerItem{
						{
							Summary: "Production environment - handle with care",
							Type:    "warning",
						},
						{
							Summary:   "Scheduled maintenance on Monday",
							Type:      "info",
							StartTime: "09:00",
							EndTime:   "17:00",
							TimeZone:  "America/New_York",
							DayOfWeek: func() *int { d := 1; return &d }(),
						},
					},
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify banner ConfigMap contains both banners
		bannerConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      bannerConfigMapName,
			Namespace: infoNamespace,
		}, bannerConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		bannerContent := bannerConfigMap.Data["banner-content.yaml"]
		g.Expect(bannerContent).To(gomega.ContainSubstring("Production environment"))
		g.Expect(bannerContent).To(gomega.ContainSubstring("Scheduled maintenance"))
		g.Expect(bannerContent).To(gomega.ContainSubstring("09:00"))
		g.Expect(bannerContent).To(gomega.ContainSubstring("America/New_York"))
	})

	t.Run("should create ConfigMap with empty banners array when Items is explicitly empty", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo with empty banners
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				Banner: &konfluxv1alpha1.Banner{
					Items: &[]konfluxv1alpha1.BannerItem{},
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify banner ConfigMap
		bannerConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      bannerConfigMapName,
			Namespace: infoNamespace,
		}, bannerConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		bannerContent := bannerConfigMap.Data["banner-content.yaml"]
		// Empty slice should produce an empty array in YAML
		g.Expect(bannerContent).To(gomega.ContainSubstring("[]"))
	})

	t.Run("should create ConfigMap with empty banners array when banner is not specified", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo without banner (nil)
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				// Banner is nil/not specified
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify banner ConfigMap exists with empty array
		bannerConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      bannerConfigMapName,
			Namespace: infoNamespace,
		}, bannerConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		bannerContent := bannerConfigMap.Data["banner-content.yaml"]
		// No banner specified should produce an empty array in YAML
		g.Expect(bannerContent).To(gomega.ContainSubstring("[]"))
	})
}

func TestKonfluxInfoEdgeCases(t *testing.T) {
	// Skip if k8sClient is not initialized (tests need to run as part of Ginkgo suite)
	if k8sClient == nil || objectStore == nil {
		t.Skip("Skipping test: k8sClient or objectStore not initialized. Run tests via Ginkgo suite.")
	}

	ctx := context.Background()

	t.Run("should not return error for non-existent resource", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Reconcile a non-existent resource
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: "non-existent-resource",
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})

	t.Run("should update ConfigMap when PublicInfo is changed", func(t *testing.T) {
		g := gomega.NewWithT(t)
		typeNamespacedName := types.NamespacedName{
			Name: CRName,
		}

		// Create KonfluxInfo with initial configuration
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				PublicInfo: &konfluxv1alpha1.PublicInfo{
					Environment: "development",
					Visibility:  "public",
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile to create initial resources
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Update KonfluxInfo with new environment
		info := &konfluxv1alpha1.KonfluxInfo{}
		err = k8sClient.Get(ctx, typeNamespacedName, info)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		info.Spec.PublicInfo.Environment = "production"
		err = k8sClient.Update(ctx, info)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Reconcile the updated resource
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify ConfigMap was updated
		infoConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      infoConfigMapName,
			Namespace: infoNamespace,
		}, infoConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		var infoJSON map[string]interface{}
		err = json.Unmarshal([]byte(infoConfigMap.Data["info.json"]), &infoJSON)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(infoJSON["environment"]).To(gomega.Equal("production"))
	})
}

func TestKonfluxInfoClusterConfig(t *testing.T) {
	// Skip if k8sClient is not initialized (tests need to run as part of Ginkgo suite)
	if k8sClient == nil || objectStore == nil {
		t.Skip("Skipping test: k8sClient or objectStore not initialized. Run tests via Ginkgo suite.")
	}

	ctx := context.Background()
	typeNamespacedName := types.NamespacedName{
		Name: CRName,
	}

	t.Run("should create cluster-config ConfigMap with user-provided values", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo with ClusterConfig
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				ClusterConfig: &konfluxv1alpha1.ClusterConfig{
					Data: &konfluxv1alpha1.ClusterConfigData{
						DefaultOIDCIssuer: "https://oidc.example.com",
						FulcioInternalUrl: "https://fulcio-internal.example.com",
						FulcioExternalUrl: "https://fulcio-external.example.com",
						RekorInternalUrl:  "https://rekor-internal.example.com",
						RekorExternalUrl:  "https://rekor-external.example.com",
						TufInternalUrl:    "https://tuf-internal.example.com",
						TufExternalUrl:    "https://tuf-external.example.com",
					},
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify cluster-config ConfigMap was created
		clusterConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      clusterConfigMapName,
			Namespace: infoNamespace,
		}, clusterConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(clusterConfigMap.Data).To(gomega.HaveKey("defaultOIDCIssuer"))
		g.Expect(clusterConfigMap.Data["defaultOIDCIssuer"]).To(gomega.Equal("https://oidc.example.com"))
		g.Expect(clusterConfigMap.Data).To(gomega.HaveKey("fulcioInternalUrl"))
		g.Expect(clusterConfigMap.Data["fulcioInternalUrl"]).To(gomega.Equal("https://fulcio-internal.example.com"))
		g.Expect(clusterConfigMap.Data).To(gomega.HaveKey("fulcioExternalUrl"))
		g.Expect(clusterConfigMap.Data["fulcioExternalUrl"]).To(gomega.Equal("https://fulcio-external.example.com"))
		g.Expect(clusterConfigMap.Data).To(gomega.HaveKey("rekorInternalUrl"))
		g.Expect(clusterConfigMap.Data["rekorInternalUrl"]).To(gomega.Equal("https://rekor-internal.example.com"))
		g.Expect(clusterConfigMap.Data).To(gomega.HaveKey("rekorExternalUrl"))
		g.Expect(clusterConfigMap.Data["rekorExternalUrl"]).To(gomega.Equal("https://rekor-external.example.com"))
		g.Expect(clusterConfigMap.Data).To(gomega.HaveKey("tufInternalUrl"))
		g.Expect(clusterConfigMap.Data["tufInternalUrl"]).To(gomega.Equal("https://tuf-internal.example.com"))
		g.Expect(clusterConfigMap.Data).To(gomega.HaveKey("tufExternalUrl"))
		g.Expect(clusterConfigMap.Data["tufExternalUrl"]).To(gomega.Equal("https://tuf-external.example.com"))
	})

	t.Run("should create empty cluster-config ConfigMap when ClusterConfig is not specified", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo without ClusterConfig
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify cluster-config ConfigMap exists with empty data
		clusterConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      clusterConfigMapName,
			Namespace: infoNamespace,
		}, clusterConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(clusterConfigMap.Data).NotTo(gomega.BeNil())
		g.Expect(clusterConfigMap.Data).To(gomega.BeEmpty())
	})

	t.Run("should create cluster-config ConfigMap with empty Data struct", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo with ClusterConfig but empty Data
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				ClusterConfig: &konfluxv1alpha1.ClusterConfig{
					Data: &konfluxv1alpha1.ClusterConfigData{},
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify cluster-config ConfigMap exists with empty data
		clusterConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      clusterConfigMapName,
			Namespace: infoNamespace,
		}, clusterConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(clusterConfigMap.Data).NotTo(gomega.BeNil())
		g.Expect(clusterConfigMap.Data).To(gomega.BeEmpty())
	})

	t.Run("should create cluster-config ConfigMap with partial values", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo with ClusterConfig containing only some fields
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				ClusterConfig: &konfluxv1alpha1.ClusterConfig{
					Data: &konfluxv1alpha1.ClusterConfigData{
						DefaultOIDCIssuer: "https://oidc.example.com",
						RekorExternalUrl:  "https://rekor-external.example.com",
						// Other fields are empty and should not be added to ConfigMap
					},
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify cluster-config ConfigMap contains only non-empty values
		clusterConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      clusterConfigMapName,
			Namespace: infoNamespace,
		}, clusterConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(clusterConfigMap.Data).To(gomega.HaveKey("defaultOIDCIssuer"))
		g.Expect(clusterConfigMap.Data["defaultOIDCIssuer"]).To(gomega.Equal("https://oidc.example.com"))
		g.Expect(clusterConfigMap.Data).To(gomega.HaveKey("rekorExternalUrl"))
		g.Expect(clusterConfigMap.Data["rekorExternalUrl"]).To(gomega.Equal("https://rekor-external.example.com"))
		// Verify empty fields are not present
		g.Expect(clusterConfigMap.Data).NotTo(gomega.HaveKey("enableKeylessSigning"))
		g.Expect(clusterConfigMap.Data).NotTo(gomega.HaveKey("fulcioInternalUrl"))
		g.Expect(clusterConfigMap.Data).NotTo(gomega.HaveKey("fulcioExternalUrl"))
		g.Expect(clusterConfigMap.Data).NotTo(gomega.HaveKey("rekorInternalUrl"))
		g.Expect(clusterConfigMap.Data).NotTo(gomega.HaveKey("tufInternalUrl"))
		g.Expect(clusterConfigMap.Data).NotTo(gomega.HaveKey("tufExternalUrl"))
		g.Expect(clusterConfigMap.Data).NotTo(gomega.HaveKey("trustifyServerInternalUrl"))
		g.Expect(clusterConfigMap.Data).NotTo(gomega.HaveKey("trustifyServerExternalUrl"))
	})

	t.Run("should update cluster-config ConfigMap when ClusterConfig values change", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo with initial ClusterConfig
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				ClusterConfig: &konfluxv1alpha1.ClusterConfig{
					Data: &konfluxv1alpha1.ClusterConfigData{
						DefaultOIDCIssuer: "https://oidc.example.com",
						FulcioInternalUrl: "https://fulcio-internal.example.com",
					},
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile to create initial resources
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Update KonfluxInfo with new ClusterConfig values
		info := &konfluxv1alpha1.KonfluxInfo{}
		err = k8sClient.Get(ctx, typeNamespacedName, info)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		info.Spec.ClusterConfig.Data = &konfluxv1alpha1.ClusterConfigData{
			DefaultOIDCIssuer: "https://oidc-updated.example.com",
			FulcioInternalUrl: "https://fulcio-internal-updated.example.com",
			RekorExternalUrl:  "https://rekor-external.example.com",
		}
		err = k8sClient.Update(ctx, info)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Reconcile the updated resource
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify ConfigMap was updated
		clusterConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      clusterConfigMapName,
			Namespace: infoNamespace,
		}, clusterConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(clusterConfigMap.Data["defaultOIDCIssuer"]).To(gomega.Equal("https://oidc-updated.example.com"))
		g.Expect(clusterConfigMap.Data["fulcioInternalUrl"]).To(gomega.Equal("https://fulcio-internal-updated.example.com"))
		g.Expect(clusterConfigMap.Data["rekorExternalUrl"]).To(gomega.Equal("https://rekor-external.example.com"))
		// Verify that removed fields are not present
		g.Expect(clusterConfigMap.Data).NotTo(gomega.HaveKey("fulcioExternalUrl"))
	})

	t.Run("should include cluster-config in RBAC Role resourceNames", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo resource
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Reconcile
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify Role includes cluster-config in resourceNames
		role := &rbacv1.Role{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      "konflux-public-info-view-role",
			Namespace: infoNamespace,
		}, role)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(role.Rules).ToNot(gomega.BeEmpty())
		g.Expect(role.Rules[0].ResourceNames).To(gomega.ContainElement(clusterConfigMapName))
	})

	t.Run("should merge discovered values with user-provided values", func(t *testing.T) {
		g := gomega.NewWithT(t)

		// Create KonfluxInfo with some user-provided values
		resource := &konfluxv1alpha1.KonfluxInfo{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
			},
			Spec: konfluxv1alpha1.KonfluxInfoSpec{
				ClusterConfig: &konfluxv1alpha1.ClusterConfig{
					Data: &konfluxv1alpha1.ClusterConfigData{
						DefaultOIDCIssuer: "https://user-oidc.example.com", // User overrides discovered
						RekorExternalUrl:  "https://user-rekor-external.example.com",
						// User doesn't provide FulcioInternalUrl, so discovered value should be used
					},
				},
			},
		}
		err := k8sClient.Create(ctx, resource)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		defer func() {
			_ = k8sClient.Delete(ctx, resource)
		}()

		// Create a reconciler with injected discovery implementation
		controllerReconciler := &KonfluxInfoReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
			DiscoverClusterConfig: &testClusterConfigDiscoverer{
				discovered: konfluxv1alpha1.ClusterConfigData{
					DefaultOIDCIssuer: "https://discovered-oidc.example.com",
					FulcioInternalUrl: "https://discovered-fulcio-internal.example.com",
					TufExternalUrl:    "https://discovered-tuf-external.example.com",
				},
			},
		}

		// Reconcile
		_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify ConfigMap contains merged values
		clusterConfigMap := &corev1.ConfigMap{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Name:      clusterConfigMapName,
			Namespace: infoNamespace,
		}, clusterConfigMap)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// User-provided values should override discovered values
		g.Expect(clusterConfigMap.Data["defaultOIDCIssuer"]).To(gomega.Equal("https://user-oidc.example.com"))
		g.Expect(clusterConfigMap.Data["rekorExternalUrl"]).To(gomega.Equal("https://user-rekor-external.example.com"))

		// Discovered values should be present when user doesn't provide them
		g.Expect(clusterConfigMap.Data["fulcioInternalUrl"]).To(gomega.Equal("https://discovered-fulcio-internal.example.com"))
		g.Expect(clusterConfigMap.Data["tufExternalUrl"]).To(gomega.Equal("https://discovered-tuf-external.example.com"))
	})
}

// testClusterConfigDiscoverer is a test implementation of ClusterConfigDiscoverer
type testClusterConfigDiscoverer struct {
	discovered konfluxv1alpha1.ClusterConfigData
}

// Discover returns the pre-configured discovered values
func (d *testClusterConfigDiscoverer) Discover(ctx context.Context) konfluxv1alpha1.ClusterConfigData {
	return d.discovered
}

// Helper function to find a condition by type
func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
