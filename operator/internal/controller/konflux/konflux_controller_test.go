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

package konflux

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/defaulttenant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/internalregistry"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

var _ = Describe("Konflux Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "konflux"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		konflux := &konfluxv1alpha1.Konflux{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Konflux")
			err := k8sClient.Get(ctx, typeNamespacedName, konflux)
			if err != nil && errors.IsNotFound(err) {
				resource := &konfluxv1alpha1.Konflux{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &konfluxv1alpha1.Konflux{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance Konflux")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			clusterInfo := createTestClusterInfo()
			controllerReconciler := &KonfluxReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ClusterInfo: clusterInfo,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})

	Context("Konflux Name Validation (CEL)", func() {
		const requiredKonfluxName = "konflux"

		AfterEach(func(ctx context.Context) {
			// Clean up any Konflux instances created during tests
			konfluxList := &konfluxv1alpha1.KonfluxList{}
			if err := k8sClient.List(ctx, konfluxList); err == nil {
				for _, item := range konfluxList.Items {
					if err := k8sClient.Delete(ctx, &item); err != nil && !errors.IsNotFound(err) {
						_, _ = fmt.Fprintf(
							GinkgoWriter,
							"Failed to delete Konflux %q: %v\n",
							item.GetName(),
							err,
						)
					}
				}
			}
		})

		It("Should allow creation with the required name 'konflux'", func(ctx context.Context) {
			By("creating a Konflux instance with the required name")
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: requiredKonfluxName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{},
			}
			err := k8sClient.Create(ctx, konflux)
			Expect(err).NotTo(HaveOccurred(), "Creation with required name should be allowed")

			By("verifying the instance was created")
			created := &konfluxv1alpha1.Konflux{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: requiredKonfluxName}, created)
			Expect(err).NotTo(HaveOccurred())
			Expect(created.GetName()).To(Equal(requiredKonfluxName))
		})

		It("Should deny creation with a different name", func(ctx context.Context) {
			By("attempting to create a Konflux instance with a different name")
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-konflux",
				},
				Spec: konfluxv1alpha1.KonfluxSpec{},
			}
			err := k8sClient.Create(ctx, konflux)
			Expect(err).To(HaveOccurred(), "Creation with different name should be rejected")
			Expect(err.Error()).To(ContainSubstring("konflux"), "Error message should mention 'konflux'")
		})

		It("Should allow updates to the instance with the required name", func(ctx context.Context) {
			By("creating a Konflux instance with the required name")
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: requiredKonfluxName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{},
			}
			err := k8sClient.Create(ctx, konflux)
			Expect(err).NotTo(HaveOccurred(), "Failed to create Konflux instance")

			By("updating the instance")
			// Get the latest version
			updated := &konfluxv1alpha1.Konflux{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: requiredKonfluxName}, updated)
			Expect(err).NotTo(HaveOccurred())

			// Add a label
			if updated.Labels == nil {
				updated.Labels = make(map[string]string)
			}
			updated.Labels["test"] = "value"
			err = k8sClient.Update(ctx, updated)
			Expect(err).NotTo(HaveOccurred(), "Updates should be allowed")
		})
	})

	Context("InternalRegistry conditional enablement", func() {
		const resourceName = "konflux"
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		registryTypeNamespacedName := types.NamespacedName{
			Name: internalregistry.CRName,
		}

		AfterEach(func() {
			// Cleanup Konflux CR
			resource := &konfluxv1alpha1.Konflux{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			// Cleanup InternalRegistry CR if it exists
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
			if err := k8sClient.Get(ctx, registryTypeNamespacedName, registry); err == nil {
				Expect(k8sClient.Delete(ctx, registry)).To(Succeed())
			}
		})

		It("should not create InternalRegistry CR when internalRegistry is omitted", func() {
			By("creating Konflux CR without internalRegistry config")
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{
					// internalRegistry is omitted (nil)
				},
			}
			Expect(k8sClient.Create(ctx, konflux)).To(Succeed())

			By("reconciling the Konflux CR")
			clusterInfo := createTestClusterInfo()
			reconciler := &KonfluxReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ClusterInfo: clusterInfo,
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying InternalRegistry CR was not created")
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
			err = k8sClient.Get(ctx, registryTypeNamespacedName, registry)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "InternalRegistry CR should not exist when omitted")
		})

		It("should not create InternalRegistry CR when enabled is false", func() {
			By("creating Konflux CR with internalRegistry.enabled=false")
			disabled := false
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InternalRegistry: &konfluxv1alpha1.InternalRegistryConfig{
						Enabled: &disabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, konflux)).To(Succeed())

			By("reconciling the Konflux CR")
			clusterInfo := createTestClusterInfo()
			reconciler := &KonfluxReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ClusterInfo: clusterInfo,
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying InternalRegistry CR was not created")
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
			err = k8sClient.Get(ctx, registryTypeNamespacedName, registry)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "InternalRegistry CR should not exist when enabled=false")
		})

		It("should create InternalRegistry CR when enabled is true", func() {
			By("creating Konflux CR with internalRegistry.enabled=true")
			enabled := true
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InternalRegistry: &konfluxv1alpha1.InternalRegistryConfig{
						Enabled: &enabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, konflux)).To(Succeed())

			By("reconciling the Konflux CR")
			clusterInfo := createTestClusterInfo()
			reconciler := &KonfluxReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ClusterInfo: clusterInfo,
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying InternalRegistry CR was created")
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
			err = k8sClient.Get(ctx, registryTypeNamespacedName, registry)
			Expect(err).NotTo(HaveOccurred(), "InternalRegistry CR should exist when enabled=true")
			Expect(registry.Name).To(Equal(internalregistry.CRName))
		})

		It("should delete InternalRegistry CR when enabled changes from true to false", func() {
			By("creating Konflux CR with internalRegistry.enabled=true")
			enabled := true
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{
					InternalRegistry: &konfluxv1alpha1.InternalRegistryConfig{
						Enabled: &enabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, konflux)).To(Succeed())

			By("reconciling to create the InternalRegistry CR")
			clusterInfo := createTestClusterInfo()
			reconciler := &KonfluxReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ClusterInfo: clusterInfo,
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying InternalRegistry CR was created")
			registry := &konfluxv1alpha1.KonfluxInternalRegistry{}
			err = k8sClient.Get(ctx, registryTypeNamespacedName, registry)
			Expect(err).NotTo(HaveOccurred())

			By("updating Konflux CR to set enabled=false")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedKonflux)).To(Succeed())
			disabled := false
			updatedKonflux.Spec.InternalRegistry = &konfluxv1alpha1.InternalRegistryConfig{
				Enabled: &disabled,
			}
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("reconciling again after disabling")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying InternalRegistry CR was deleted")
			err = k8sClient.Get(ctx, registryTypeNamespacedName, registry)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "InternalRegistry CR should be deleted when enabled changes to false")
		})
	})

	Context("DefaultTenant conditional enablement", func() {
		const resourceName = "konflux"
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		defaultTenantTypeNamespacedName := types.NamespacedName{
			Name: defaulttenant.CRName,
		}

		AfterEach(func() {
			// Cleanup Konflux CR
			resource := &konfluxv1alpha1.Konflux{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			// Cleanup DefaultTenant CR if it exists
			tenant := &konfluxv1alpha1.KonfluxDefaultTenant{}
			if err := k8sClient.Get(ctx, defaultTenantTypeNamespacedName, tenant); err == nil {
				Expect(k8sClient.Delete(ctx, tenant)).To(Succeed())
			}
		})

		It("should create DefaultTenant CR when defaultTenant is omitted (enabled by default)", func() {
			By("creating Konflux CR without defaultTenant config")
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{
					// defaultTenant is omitted (nil) - should default to enabled
				},
			}
			Expect(k8sClient.Create(ctx, konflux)).To(Succeed())

			By("reconciling the Konflux CR")
			clusterInfo := createTestClusterInfo()
			reconciler := &KonfluxReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ClusterInfo: clusterInfo,
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying DefaultTenant CR was created (enabled by default)")
			tenant := &konfluxv1alpha1.KonfluxDefaultTenant{}
			err = k8sClient.Get(ctx, defaultTenantTypeNamespacedName, tenant)
			Expect(err).NotTo(HaveOccurred(), "DefaultTenant CR should exist when omitted (enabled by default)")
			Expect(tenant.Name).To(Equal(defaulttenant.CRName))
		})

		It("should create DefaultTenant CR when enabled is explicitly true", func() {
			By("creating Konflux CR with defaultTenant.enabled=true")
			enabled := true
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{
					DefaultTenant: &konfluxv1alpha1.DefaultTenantConfig{
						Enabled: &enabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, konflux)).To(Succeed())

			By("reconciling the Konflux CR")
			clusterInfo := createTestClusterInfo()
			reconciler := &KonfluxReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ClusterInfo: clusterInfo,
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying DefaultTenant CR was created")
			tenant := &konfluxv1alpha1.KonfluxDefaultTenant{}
			err = k8sClient.Get(ctx, defaultTenantTypeNamespacedName, tenant)
			Expect(err).NotTo(HaveOccurred(), "DefaultTenant CR should exist when enabled=true")
		})

		It("should not create DefaultTenant CR when enabled is false", func() {
			By("creating Konflux CR with defaultTenant.enabled=false")
			disabled := false
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{
					DefaultTenant: &konfluxv1alpha1.DefaultTenantConfig{
						Enabled: &disabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, konflux)).To(Succeed())

			By("reconciling the Konflux CR")
			clusterInfo := createTestClusterInfo()
			reconciler := &KonfluxReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ClusterInfo: clusterInfo,
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying DefaultTenant CR was not created")
			tenant := &konfluxv1alpha1.KonfluxDefaultTenant{}
			err = k8sClient.Get(ctx, defaultTenantTypeNamespacedName, tenant)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "DefaultTenant CR should not exist when enabled=false")
		})

		It("should delete DefaultTenant CR when enabled changes from true to false", func() {
			By("creating Konflux CR with defaultTenant.enabled=true")
			enabled := true
			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
				},
				Spec: konfluxv1alpha1.KonfluxSpec{
					DefaultTenant: &konfluxv1alpha1.DefaultTenantConfig{
						Enabled: &enabled,
					},
				},
			}
			Expect(k8sClient.Create(ctx, konflux)).To(Succeed())

			By("reconciling to create the DefaultTenant CR")
			clusterInfo := createTestClusterInfo()
			reconciler := &KonfluxReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ClusterInfo: clusterInfo,
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying DefaultTenant CR was created")
			tenant := &konfluxv1alpha1.KonfluxDefaultTenant{}
			err = k8sClient.Get(ctx, defaultTenantTypeNamespacedName, tenant)
			Expect(err).NotTo(HaveOccurred())

			By("updating Konflux CR to set enabled=false")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedKonflux)).To(Succeed())
			disabled := false
			updatedKonflux.Spec.DefaultTenant = &konfluxv1alpha1.DefaultTenantConfig{
				Enabled: &disabled,
			}
			Expect(k8sClient.Update(ctx, updatedKonflux)).To(Succeed())

			By("reconciling again after disabling")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying DefaultTenant CR was deleted")
			err = k8sClient.Get(ctx, defaultTenantTypeNamespacedName, tenant)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "DefaultTenant CR should be deleted when enabled changes to false")
		})
	})
})

// createTestClusterInfo creates a minimal ClusterInfo for testing
func createTestClusterInfo() *clusterinfo.Info {
	mockClient := &testMockDiscoveryClient{
		resources:     map[string]*metav1.APIResourceList{},
		serverVersion: &version.Info{GitVersion: "v1.30.0"},
	}
	info, _ := clusterinfo.DetectWithClient(mockClient)
	return info
}

// testMockDiscoveryClient implements clusterinfo.DiscoveryClient for general testing
type testMockDiscoveryClient struct {
	resources     map[string]*metav1.APIResourceList
	serverVersion *version.Info
}

func (m *testMockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if r, ok := m.resources[groupVersion]; ok {
		return r, nil
	}
	return nil, errors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *testMockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return m.serverVersion, nil
}
