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

package segmentbridge

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

func noDefaultKey() string             { return "" }
func staticKey(k string) func() string { return func() string { return k } }

type mockDiscoveryClient struct {
	resources     map[string]*metav1.APIResourceList
	serverVersion *version.Info
}

func (m *mockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if rl, ok := m.resources[groupVersion]; ok {
		return rl, nil
	}
	return nil, errors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *mockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return m.serverVersion, nil
}

var _ clusterinfo.DiscoveryClient = (*mockDiscoveryClient)(nil)

var _ = Describe("KonfluxSegmentBridge Controller", func() {
	Context("When reconciling a resource", func() {

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: CRName,
		}
		konfluxsegmentbridge := &konfluxv1alpha1.KonfluxSegmentBridge{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KonfluxSegmentBridge")
			err := k8sClient.Get(ctx, typeNamespacedName, konfluxsegmentbridge)
			if err != nil && errors.IsNotFound(err) {
				resource := &konfluxv1alpha1.KonfluxSegmentBridge{
					ObjectMeta: metav1.ObjectMeta{
						Name: CRName,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KonfluxSegmentBridge")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Cleanup the segment-bridge-config secret if it exists")
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)
			if err == nil {
				_ = k8sClient.Delete(ctx, secret)
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: noDefaultKey,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create Secret with empty segment fields when no write key is configured", func() {
			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: noDefaultKey,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed(), "Secret should always be created")

			Expect(string(secret.Data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
			Expect(string(secret.Data["SEGMENT_WRITE_KEY"])).To(BeEmpty(), "write key should be empty")
			Expect(string(secret.Data["SEGMENT_BATCH_API"])).To(Equal(
				konfluxv1alpha1.DefaultSegmentAPIURL + "/batch"))
		})

		It("should retain Secret with only TEKTON_RESULTS_API_ADDR when key becomes empty", func() {
			By("First reconciling with a key to create the Secret")
			resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.SegmentKey = "temporary-key"
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: noDefaultKey,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed(), "Secret should exist after reconciling with a key")
			Expect(secret.Data).To(HaveKey("SEGMENT_WRITE_KEY"))

			By("Removing the key and reconciling again")
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.SegmentKey = ""
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed(), "Secret should still exist after key removal")

			Expect(string(secret.Data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
			Expect(string(secret.Data["SEGMENT_WRITE_KEY"])).To(BeEmpty(), "write key should be empty")
			Expect(string(secret.Data["SEGMENT_BATCH_API"])).To(Equal(
				konfluxv1alpha1.DefaultSegmentAPIURL + "/batch"))
		})

		It("should create Secret with both SEGMENT_WRITE_KEY and SEGMENT_BATCH_API from inline CR fields", func() {
			By("Updating the CR with inline segment config")
			resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.SegmentKey = "test-write-key"
			resource.Spec.SegmentAPIURL = "https://console.redhat.com/connections/api/v1"
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: noDefaultKey,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed())

			Expect(string(secret.Data["SEGMENT_WRITE_KEY"])).To(Equal("test-write-key"))
			Expect(string(secret.Data["SEGMENT_BATCH_API"])).To(Equal("https://console.redhat.com/connections/api/v1/batch"))
			Expect(string(secret.Data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
		})

		It("should use default URL when only segmentKey is set", func() {
			By("Updating the CR with only a segment key")
			resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.SegmentKey = "default-key"
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: noDefaultKey,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed())

			Expect(string(secret.Data["SEGMENT_WRITE_KEY"])).To(Equal("default-key"))
			Expect(string(secret.Data["SEGMENT_BATCH_API"])).To(Equal(
				konfluxv1alpha1.DefaultSegmentAPIURL + "/batch"))
			Expect(string(secret.Data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
		})

		It("should use build-time default key when CR key is empty", func() {
			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: staticKey("build-time-key"),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed())

			Expect(string(secret.Data["SEGMENT_WRITE_KEY"])).To(Equal("build-time-key"))
			Expect(string(secret.Data["SEGMENT_BATCH_API"])).To(Equal(
				konfluxv1alpha1.DefaultSegmentAPIURL + "/batch"))
			Expect(string(secret.Data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
		})

		It("should prefer CR inline key over build-time default", func() {
			By("Updating the CR with an inline segment key")
			resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.SegmentKey = "cr-override-key"
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: staticKey("build-time-key"),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed())

			Expect(string(secret.Data["SEGMENT_WRITE_KEY"])).To(Equal("cr-override-key"))
			Expect(string(secret.Data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
		})

		It("should use OpenShift Tekton Results API address when running on OpenShift", func() {
			By("Updating the CR with a segment key")
			resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.SegmentKey = "openshift-key"
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			openShiftClusterInfo, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{
				resources: map[string]*metav1.APIResourceList{
					"config.openshift.io/v1": {
						APIResources: []metav1.APIResource{
							{Kind: "ClusterVersion"},
						},
					},
				},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())

			controllerReconciler := &KonfluxSegmentBridgeReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				ClusterInfo:          openShiftClusterInfo,
				GetDefaultSegmentKey: noDefaultKey,
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed())

			Expect(string(secret.Data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrOpenShift))
		})

	})
})
