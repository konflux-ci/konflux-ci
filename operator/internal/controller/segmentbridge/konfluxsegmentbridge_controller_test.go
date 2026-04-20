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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
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
	// startManager creates a per-test manager with the given reconciler configuration
	// and registers a DeferCleanup to cancel it after the test.
	startManager := func(getDefaultSegmentKey func() string, clusterInfo *clusterinfo.Info) {
		mgr := testutil.NewTestManager(testEnv)
		Expect((&KonfluxSegmentBridgeReconciler{
			Client:               mgr.GetClient(),
			Scheme:               mgr.GetScheme(),
			ObjectStore:          objectStore,
			GetDefaultSegmentKey: getDefaultSegmentKey,
			ClusterInfo:          clusterInfo,
		}).SetupWithManager(mgr)).To(Succeed())
		mgrCtx, cancel := context.WithCancel(testEnv.Ctx)
		DeferCleanup(cancel)
		testutil.StartManagerWithContext(mgrCtx, mgr)
	}

	// createCR creates the KonfluxSegmentBridge CR and registers cleanup for both
	// the CR and the Secret the controller will create.
	createCR := func(ctx context.Context) {
		Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxSegmentBridge{
			ObjectMeta: metav1.ObjectMeta{Name: CRName},
		})).To(Succeed())
		DeferCleanup(func(ctx context.Context) {
			testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})
			_ = k8sClient.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name:      segmentBridgeSecretName,
				Namespace: segmentBridgeNamespace,
			}})
		})
	}

	// waitForSecret polls until the Secret exists and the caller's check passes.
	waitForSecret := func(ctx context.Context, check func(g Gomega, data map[string][]byte)) {
		Eventually(func(g Gomega) {
			secret := &corev1.Secret{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed())
			check(g, secret.Data)
		}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
	}

	Context("When reconciling a resource", func() {
		Context("with no-op default segment key", func() {
			BeforeEach(func() { startManager(noDefaultKey, nil) })

			It("should successfully reconcile the resource", func(ctx context.Context) {
				createCR(ctx)
				Eventually(func(g Gomega) {
					cr := &konfluxv1alpha1.KonfluxSegmentBridge{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, cr)).To(Succeed())
					g.Expect(cr.Status.Conditions).To(ContainElement(
						HaveField("Type", condition.TypeReady),
					))
				}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
			})

			It("should create Secret with empty segment fields when no write key is configured", func(ctx context.Context) {
				createCR(ctx)
				waitForSecret(ctx, func(g Gomega, data map[string][]byte) {
					g.Expect(string(data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
					g.Expect(string(data["SEGMENT_WRITE_KEY"])).To(BeEmpty(), "write key should be empty")
					g.Expect(string(data["SEGMENT_BATCH_API"])).To(Equal(
						konfluxv1alpha1.DefaultSegmentAPIURL + "/batch"))
				})
			})

			It("should retain Secret with only TEKTON_RESULTS_API_ADDR when key becomes empty", func(ctx context.Context) {
				createCR(ctx)

				By("setting a temporary key on the CR")
				resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, resource)).To(Succeed())
				resource.Spec.SegmentKey = "temporary-key"
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())

				waitForSecret(ctx, func(g Gomega, data map[string][]byte) {
					g.Expect(string(data["SEGMENT_WRITE_KEY"])).To(Equal("temporary-key"))
				})

				By("removing the key from the CR")
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, resource)).To(Succeed())
				resource.Spec.SegmentKey = ""
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())

				waitForSecret(ctx, func(g Gomega, data map[string][]byte) {
					g.Expect(string(data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
					g.Expect(string(data["SEGMENT_WRITE_KEY"])).To(BeEmpty(), "write key should be empty")
					g.Expect(string(data["SEGMENT_BATCH_API"])).To(Equal(
						konfluxv1alpha1.DefaultSegmentAPIURL + "/batch"))
				})
			})

			It("should create Secret with both SEGMENT_WRITE_KEY and SEGMENT_BATCH_API from inline CR fields", func(ctx context.Context) {
				createCR(ctx)

				resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, resource)).To(Succeed())
				resource.Spec.SegmentKey = "test-write-key"
				resource.Spec.SegmentAPIURL = "https://console.redhat.com/connections/api/v1"
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())

				waitForSecret(ctx, func(g Gomega, data map[string][]byte) {
					g.Expect(string(data["SEGMENT_WRITE_KEY"])).To(Equal("test-write-key"))
					g.Expect(string(data["SEGMENT_BATCH_API"])).To(Equal("https://console.redhat.com/connections/api/v1/batch"))
					g.Expect(string(data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
				})
			})

			It("should use default URL when only segmentKey is set", func(ctx context.Context) {
				createCR(ctx)

				resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, resource)).To(Succeed())
				resource.Spec.SegmentKey = "default-key"
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())

				waitForSecret(ctx, func(g Gomega, data map[string][]byte) {
					g.Expect(string(data["SEGMENT_WRITE_KEY"])).To(Equal("default-key"))
					g.Expect(string(data["SEGMENT_BATCH_API"])).To(Equal(
						konfluxv1alpha1.DefaultSegmentAPIURL + "/batch"))
					g.Expect(string(data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
				})
			})
		})

		Context("with build-time default key", func() {
			BeforeEach(func() { startManager(staticKey("build-time-key"), nil) })

			It("should use build-time default key when CR key is empty", func(ctx context.Context) {
				createCR(ctx)
				waitForSecret(ctx, func(g Gomega, data map[string][]byte) {
					g.Expect(string(data["SEGMENT_WRITE_KEY"])).To(Equal("build-time-key"))
					g.Expect(string(data["SEGMENT_BATCH_API"])).To(Equal(
						konfluxv1alpha1.DefaultSegmentAPIURL + "/batch"))
					g.Expect(string(data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
				})
			})

			It("should prefer CR inline key over build-time default", func(ctx context.Context) {
				createCR(ctx)

				resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, resource)).To(Succeed())
				resource.Spec.SegmentKey = "cr-override-key"
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())

				waitForSecret(ctx, func(g Gomega, data map[string][]byte) {
					g.Expect(string(data["SEGMENT_WRITE_KEY"])).To(Equal("cr-override-key"))
					g.Expect(string(data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrK8s))
				})
			})
		})

		Context("on OpenShift", func() {
			BeforeEach(func() {
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
				startManager(noDefaultKey, openShiftClusterInfo)
			})

			It("should use OpenShift Tekton Results API address when running on OpenShift", func(ctx context.Context) {
				createCR(ctx)

				resource := &konfluxv1alpha1.KonfluxSegmentBridge{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, resource)).To(Succeed())
				resource.Spec.SegmentKey = "openshift-key"
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())

				waitForSecret(ctx, func(g Gomega, data map[string][]byte) {
					g.Expect(string(data["TEKTON_RESULTS_API_ADDR"])).To(Equal(tektonResultsAPIAddrOpenShift))
				})
			})
		})
	})
})
