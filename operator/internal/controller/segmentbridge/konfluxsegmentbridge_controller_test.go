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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const childResourceName = "segment-bridge"

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
	// A per-test manager is required because each test wires the reconciler with a different
	// GetDefaultSegmentKey or ClusterInfo, and a shared suite-level manager cannot be
	// re-configured between tests.
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
		waitForStop := testutil.StartManagerWithContext(mgrCtx, mgr)
		DeferCleanup(func() {
			cancel()
			waitForStop()
		})
	}

	// createCR creates the KonfluxSegmentBridge CR and registers cleanup for both
	// the CR and the Secret the controller will create.
	createCR := func(ctx context.Context) {
		segmentRes := &konfluxv1alpha1.KonfluxSegmentBridge{
			ObjectMeta: metav1.ObjectMeta{Name: CRName},
		}
		Expect(k8sClient.Create(ctx, segmentRes)).To(Succeed())
		testutil.DeferCleanupParentAndChildren(k8sClient, segmentRes, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      segmentBridgeSecretName,
			Namespace: segmentBridgeNamespace,
		}})
	}

	// segmentBridgeChildren lists cluster-scoped children that envtest's missing GC
	// won't cascade-delete when the parent CR is removed.
	segmentBridgeChildren := func() []client.Object {
		return []client.Object{
			&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: childResourceName}},
			&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: childResourceName}},
		}
	}

	// waitForSecret polls until the Secret exists and the caller's check passes.
	waitForSecret := func(ctx context.Context, check func(g Gomega, data map[string][]byte)) {
		Eventually(func(g Gomega) {
			secret := &corev1.Secret{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: segmentBridgeSecretName, Namespace: segmentBridgeNamespace,
			}, secret)).To(Succeed())
			check(g, secret.Data)
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
	}

	Context("When reconciling a resource", func() {
		Context("with no-op default segment key", func() {
			BeforeEach(func() { startManager(noDefaultKey, nil) })

			It("should successfully reconcile the resource", func(ctx context.Context) {
				createCR(ctx)
				Eventually(func(g Gomega) {
					cr := &konfluxv1alpha1.KonfluxSegmentBridge{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, cr)).To(Succeed())
					g.Expect(condition.IsConditionTrue(cr, condition.TypeReady)).To(BeTrue())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
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

	Context("Self-healing", func() {
		BeforeEach(func() { startManager(noDefaultKey, nil) })

		It("recreates ServiceAccount when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, segmentBridgeChildren()...)

			saNN := types.NamespacedName{
				Name:      childResourceName,
				Namespace: segmentBridgeNamespace,
			}

			By("waiting for initial ServiceAccount creation")
			var originalUID types.UID
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				originalUID = sa.UID
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ServiceAccount")
			Expect(k8sClient.Delete(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: saNN.Name, Namespace: saNN.Namespace},
			})).To(Succeed())

			By("verifying the ServiceAccount is recreated with a new UID and ownership labels")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.UID).NotTo(Equal(originalUID))
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates CronJob when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, segmentBridgeChildren()...)

			cjNN := types.NamespacedName{
				Name:      childResourceName,
				Namespace: segmentBridgeNamespace,
			}

			By("waiting for initial CronJob creation")
			var originalUID types.UID
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				originalUID = cj.UID
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the CronJob")
			Expect(k8sClient.Delete(ctx, &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{Name: cjNN.Name, Namespace: cjNN.Namespace},
			})).To(Succeed())

			By("verifying the CronJob is recreated with a new UID and ownership labels")
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				g.Expect(cj.UID).NotTo(Equal(originalUID))
				g.Expect(cj.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ClusterRole when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, segmentBridgeChildren()...)

			crNN := types.NamespacedName{Name: childResourceName}

			By("waiting for initial ClusterRole creation")
			var originalUID types.UID
			Eventually(func(g Gomega) {
				role := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, role)).To(Succeed())
				originalUID = role.UID
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ClusterRole")
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: crNN.Name},
			})).To(Succeed())

			By("verifying the ClusterRole is recreated with a new UID and ownership labels")
			Eventually(func(g Gomega) {
				role := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, role)).To(Succeed())
				g.Expect(role.UID).NotTo(Equal(originalUID))
				g.Expect(role.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ClusterRoleBinding when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, segmentBridgeChildren()...)

			crbNN := types.NamespacedName{Name: childResourceName}

			By("waiting for initial ClusterRoleBinding creation")
			var originalUID types.UID
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				originalUID = crb.UID
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ClusterRoleBinding")
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: crbNN.Name},
			})).To(Succeed())

			By("verifying the ClusterRoleBinding is recreated with a new UID and ownership labels")
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				g.Expect(crb.UID).NotTo(Equal(originalUID))
				g.Expect(crb.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Drift correction", func() {
		BeforeEach(func() { startManager(noDefaultKey, nil) })

		It("restores Namespace labels when stripped", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, segmentBridgeChildren()...)

			nsNN := types.NamespacedName{Name: segmentBridgeNamespace}

			By("waiting for initial Namespace creation with ownership labels")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Namespace")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				delete(ns.Labels, constant.KonfluxOwnerLabel)
				delete(ns.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, ns)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Namespace labels are restored")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ServiceAccount labels when stripped", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, segmentBridgeChildren()...)

			saNN := types.NamespacedName{
				Name:      childResourceName,
				Namespace: segmentBridgeNamespace,
			}

			By("waiting for initial ServiceAccount creation with ownership labels")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the ServiceAccount")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				delete(sa.Labels, constant.KonfluxOwnerLabel)
				delete(sa.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, sa)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ServiceAccount labels are restored")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores CronJob image when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, segmentBridgeChildren()...)

			cjNN := types.NamespacedName{
				Name:      childResourceName,
				Namespace: segmentBridgeNamespace,
			}

			By("waiting for initial CronJob creation")
			var originalImage string
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, "segment-bridge")
				g.Expect(container).NotTo(BeNil())
				originalImage = container.Image
				g.Expect(originalImage).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the CronJob image")
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, "segment-bridge")
				g.Expect(container).NotTo(BeNil())
				container.Image = "tampered-image:latest"
				g.Expect(k8sClient.Update(ctx, cj)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the CronJob image is restored")
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, "segment-bridge")
				g.Expect(container).NotTo(BeNil())
				g.Expect(container.Image).To(Equal(originalImage))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores CronJob schedule when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, segmentBridgeChildren()...)

			cjNN := types.NamespacedName{
				Name:      childResourceName,
				Namespace: segmentBridgeNamespace,
			}

			By("waiting for initial CronJob creation")
			var originalSchedule string
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				originalSchedule = cj.Spec.Schedule
				g.Expect(originalSchedule).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the CronJob schedule")
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				cj.Spec.Schedule = "*/5 * * * *"
				g.Expect(k8sClient.Update(ctx, cj)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the CronJob schedule is restored")
			Eventually(func(g Gomega) {
				cj := &batchv1.CronJob{}
				g.Expect(k8sClient.Get(ctx, cjNN, cj)).To(Succeed())
				g.Expect(cj.Spec.Schedule).To(Equal(originalSchedule))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ClusterRole rules when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, segmentBridgeChildren()...)

			crNN := types.NamespacedName{Name: childResourceName}

			By("waiting for initial ClusterRole creation")
			var originalRules []rbacv1.PolicyRule
			Eventually(func(g Gomega) {
				role := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, role)).To(Succeed())
				g.Expect(role.Rules).NotTo(BeEmpty())
				originalRules = role.Rules
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ClusterRole rules")
			Eventually(func(g Gomega) {
				role := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, role)).To(Succeed())
				role.Rules = []rbacv1.PolicyRule{{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"delete"},
				}}
				g.Expect(k8sClient.Update(ctx, role)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ClusterRole rules are restored")
			Eventually(func(g Gomega) {
				role := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, role)).To(Succeed())
				g.Expect(role.Rules).To(Equal(originalRules))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ClusterRoleBinding subjects when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, segmentBridgeChildren()...)

			crbNN := types.NamespacedName{Name: childResourceName}

			By("waiting for initial ClusterRoleBinding creation")
			var originalSubjects []rbacv1.Subject
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				g.Expect(crb.Subjects).NotTo(BeEmpty())
				originalSubjects = crb.Subjects
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ClusterRoleBinding subjects")
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				crb.Subjects = []rbacv1.Subject{{
					Kind:      "ServiceAccount",
					Name:      "tampered-sa",
					Namespace: "default",
				}}
				g.Expect(k8sClient.Update(ctx, crb)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ClusterRoleBinding subjects are restored")
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				g.Expect(crb.Subjects).To(Equal(originalSubjects))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})
})
