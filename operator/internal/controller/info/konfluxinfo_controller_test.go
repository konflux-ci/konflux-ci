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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

const (
	infoRoleName        = "konflux-public-info-view-role"
	infoRoleBindingName = "konflux-public-info-view-rb"
)

var _ = Describe("KonfluxInfo Controller", func() {
	// startManager starts a per-test manager with the given ClusterInfo
	// and registers a DeferCleanup to stop it when the It block finishes.
	// A per-test manager is needed because the OpenShift tests require a different
	// ClusterInfo than the default (nil) tests.
	startManager := func(clusterInfo *clusterinfo.Info) {
		mgrCtx, mgrCancel := context.WithCancel(testEnv.Ctx)
		mgr := testutil.NewTestManager(testEnv)
		Expect((&KonfluxInfoReconciler{
			Client:      mgr.GetClient(),
			Scheme:      mgr.GetScheme(),
			ObjectStore: objectStore,
			ClusterInfo: clusterInfo,
		}).SetupWithManager(mgr)).To(Succeed())
		waitForStop := testutil.StartManagerWithContext(mgrCtx, mgr)
		DeferCleanup(func() {
			mgrCancel()
			waitForStop()
		})
	}

	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			startManager(nil)

			infoRes := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, infoRes)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, infoRes)

			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.KonfluxInfo{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
				readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the konflux-info namespace was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "konflux-info"}, &corev1.Namespace{})).To(Succeed())
		})
	})

	Context("Proxy URL credential validation (CEL)", func() {
		It("should reject httpProxy containing credentials", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxInfoSpec{
					ClusterConfig: &konfluxv1alpha1.ClusterConfig{
						Data: &konfluxv1alpha1.ClusterConfigData{
							HTTPProxy: "http://user:pass@proxy.example.com:3128",
						},
					},
				},
			}
			err := k8sClient.Create(ctx, cr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must not contain credentials"))
		})

		It("should reject packageRegistryProxyNpmUrl containing credentials", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxInfoSpec{
					ClusterConfig: &konfluxv1alpha1.ClusterConfig{
						Data: &konfluxv1alpha1.ClusterConfigData{
							PackageRegistryProxyNpmURL: "https://token@registry.npmjs.org",
						},
					},
				},
			}
			err := k8sClient.Create(ctx, cr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must not contain credentials"))
		})

		It("should reject packageRegistryProxyYarnUrl containing credentials", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxInfoSpec{
					ClusterConfig: &konfluxv1alpha1.ClusterConfig{
						Data: &konfluxv1alpha1.ClusterConfigData{
							PackageRegistryProxyYarnURL: "https://user:secret@yarn.example.com",
						},
					},
				},
			}
			err := k8sClient.Create(ctx, cr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must not contain credentials"))
		})

		It("should reject packageRegistryProxyGomodUrl containing credentials", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxInfoSpec{
					ClusterConfig: &konfluxv1alpha1.ClusterConfig{
						Data: &konfluxv1alpha1.ClusterConfigData{
							PackageRegistryProxyGomodURL: "https://token@go-proxy.example.com",
						},
					},
				},
			}
			err := k8sClient.Create(ctx, cr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must not contain credentials"))
		})

		It("should reject packageRegistryProxyPipUrl containing credentials", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxInfoSpec{
					ClusterConfig: &konfluxv1alpha1.ClusterConfig{
						Data: &konfluxv1alpha1.ClusterConfigData{
							PackageRegistryProxyPipURL: "https://user:pass@pypi.example.com",
						},
					},
				},
			}
			err := k8sClient.Create(ctx, cr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must not contain credentials"))
		})

		It("should reject packageRegistryProxyPnpmUrl containing credentials", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxInfoSpec{
					ClusterConfig: &konfluxv1alpha1.ClusterConfig{
						Data: &konfluxv1alpha1.ClusterConfigData{
							PackageRegistryProxyPnpmURL: "https://admin:secret@pnpm.example.com",
						},
					},
				},
			}
			err := k8sClient.Create(ctx, cr)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must not contain credentials"))
		})

		It("should allow proxy URLs without credentials", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxInfoSpec{
					ClusterConfig: &konfluxv1alpha1.ClusterConfig{
						Data: &konfluxv1alpha1.ClusterConfigData{
							HTTPProxy:                    "squid.caching.svc.cluster.local:3128",
							PackageRegistryProxyNpmURL:   "https://npm-proxy.internal.svc:8080",
							PackageRegistryProxyYarnURL:  "https://yarn-proxy.internal.svc:8080",
							PackageRegistryProxyGomodURL: "https://go-proxy.internal.svc:8080",
							PackageRegistryProxyPipURL:   "https://pypi-proxy.internal.svc:8080",
							PackageRegistryProxyPnpmURL:  "https://pnpm-proxy.internal.svc:8080",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)
		})
	})

	Context("Self-healing", func() {
		DescribeTable("recreates ConfigMap when deleted",
			func(ctx context.Context, cmName string) {
				startManager(nil)

				cr := &konfluxv1alpha1.KonfluxInfo{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

				cmNN := types.NamespacedName{Name: cmName, Namespace: infoNamespace}

				By("waiting for initial ConfigMap creation")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, cmNN, &corev1.ConfigMap{})).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("deleting the ConfigMap")
				Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: cmNN.Name, Namespace: cmNN.Namespace},
				})).To(Succeed())

				By("verifying the ConfigMap is recreated with ownership labels")
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
					g.Expect(cm.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			Entry("info", infoConfigMapName),
			Entry("banner", bannerConfigMapName),
			Entry("cluster-config", clusterConfigMapName),
		)

		It("recreates Role when deleted", func(ctx context.Context) {
			startManager(nil)

			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			roleNN := types.NamespacedName{Name: infoRoleName, Namespace: infoNamespace}

			By("waiting for initial Role creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, roleNN, &rbacv1.Role{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Role")
			Expect(k8sClient.Delete(ctx, &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: roleNN.Name, Namespace: roleNN.Namespace},
			})).To(Succeed())

			By("verifying the Role is recreated with ownership labels")
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				g.Expect(role.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates RoleBinding when deleted", func(ctx context.Context) {
			startManager(nil)

			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			rbNN := types.NamespacedName{Name: infoRoleBindingName, Namespace: infoNamespace}

			By("waiting for initial RoleBinding creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, rbNN, &rbacv1.RoleBinding{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the RoleBinding")
			Expect(k8sClient.Delete(ctx, &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: rbNN.Name, Namespace: rbNN.Namespace},
			})).To(Succeed())

			By("verifying the RoleBinding is recreated with ownership labels")
			Eventually(func(g Gomega) {
				rb := &rbacv1.RoleBinding{}
				g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
				g.Expect(rb.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Drift correction", func() {
		It("restores Namespace labels when stripped", func(ctx context.Context) {
			startManager(nil)

			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			nsNN := types.NamespacedName{Name: infoNamespace}

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

		DescribeTable("restores ConfigMap data when modified",
			func(ctx context.Context, cmName string) {
				startManager(nil)

				cr := &konfluxv1alpha1.KonfluxInfo{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec: konfluxv1alpha1.KonfluxInfoSpec{
						ClusterConfig: &konfluxv1alpha1.ClusterConfig{
							Data: &konfluxv1alpha1.ClusterConfigData{
								DefaultOIDCIssuer: "https://oidc.example.com",
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

				cmNN := types.NamespacedName{Name: cmName, Namespace: infoNamespace}

				By("waiting for initial ConfigMap creation")
				var originalKey string
				var originalValue string
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
					g.Expect(cm.Data).NotTo(BeEmpty())
					for k, v := range cm.Data {
						originalKey = k
						originalValue = v
						break
					}
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("modifying an existing ConfigMap data key")
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
					cm.Data[originalKey] = "tampered-content"
					g.Expect(k8sClient.Update(ctx, cm)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the ConfigMap data is restored")
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
					g.Expect(cm.Data).To(HaveKeyWithValue(originalKey, originalValue))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			Entry("info", infoConfigMapName),
			Entry("banner", bannerConfigMapName),
			Entry("cluster-config", clusterConfigMapName),
		)

		It("restores Role rules when modified", func(ctx context.Context) {
			startManager(nil)

			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			roleNN := types.NamespacedName{Name: infoRoleName, Namespace: infoNamespace}

			By("waiting for initial Role creation")
			var originalRules []rbacv1.PolicyRule
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				g.Expect(role.Rules).NotTo(BeEmpty())
				originalRules = role.Rules
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Role rules")
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				role.Rules = []rbacv1.PolicyRule{{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"delete"},
				}}
				g.Expect(k8sClient.Update(ctx, role)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Role rules are restored")
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				g.Expect(role.Rules).To(Equal(originalRules))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores RoleBinding subjects when modified", func(ctx context.Context) {
			startManager(nil)

			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			rbNN := types.NamespacedName{Name: infoRoleBindingName, Namespace: infoNamespace}

			By("waiting for initial RoleBinding creation")
			var originalSubjects []rbacv1.Subject
			Eventually(func(g Gomega) {
				rb := &rbacv1.RoleBinding{}
				g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
				g.Expect(rb.Subjects).NotTo(BeEmpty())
				originalSubjects = rb.Subjects
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the RoleBinding subjects")
			Eventually(func(g Gomega) {
				rb := &rbacv1.RoleBinding{}
				g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
				rb.Subjects = []rbacv1.Subject{{
					Kind:     "User",
					Name:     "tampered-user",
					APIGroup: "rbac.authorization.k8s.io",
				}}
				g.Expect(k8sClient.Update(ctx, rb)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the RoleBinding subjects are restored")
			Eventually(func(g Gomega) {
				rb := &rbacv1.RoleBinding{}
				g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
				g.Expect(rb.Subjects).To(Equal(originalSubjects))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("OpenShift ClusterVersion watch", func() {
		It("should not include cluster version info in info.json without ClusterInfo", func(ctx context.Context) {
			startManager(nil)

			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			cmNN := types.NamespacedName{Name: infoConfigMapName, Namespace: infoNamespace}
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				g.Expect(cm.Data).To(HaveKey("info.json"))

				var info map[string]interface{}
				g.Expect(json.Unmarshal([]byte(cm.Data["info.json"]), &info)).To(Succeed())
				g.Expect(info).NotTo(HaveKey("openshiftVersion"))
				g.Expect(info).NotTo(HaveKey("kubernetesVersion"))
				g.Expect(info).NotTo(HaveKey("clusterId"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should include cluster version info in info.json on OpenShift", func(ctx context.Context) {
			By("building OpenShift cluster info")
			mockDiscovery := &MockDiscoveryClient{
				resources: map[string]*metav1.APIResourceList{
					"config.openshift.io/v1": {
						APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
					},
				},
			}
			mockDiscovery.SetVersion("v1.29.0")
			openShiftClusterInfo, err := clusterinfo.DetectWithClient(mockDiscovery)
			Expect(err).NotTo(HaveOccurred())

			startManager(openShiftClusterInfo)

			By("creating the ClusterVersion resource")
			clusterVersion := &configv1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{Name: "version"},
				Spec: configv1.ClusterVersionSpec{
					ClusterID: "test-cluster-id-12345",
				},
			}
			Expect(k8sClient.Create(ctx, clusterVersion)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, clusterVersion)

			clusterVersion.Status = configv1.ClusterVersionStatus{
				History: []configv1.UpdateHistory{
					{
						State:       configv1.CompletedUpdate,
						Version:     "4.15.0",
						StartedTime: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, clusterVersion)).To(Succeed())

			By("creating the KonfluxInfo CR")
			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			By("verifying info.json contains OpenShift version info")
			cmNN := types.NamespacedName{Name: infoConfigMapName, Namespace: infoNamespace}
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				g.Expect(cm.Data).To(HaveKey("info.json"))

				var info map[string]interface{}
				g.Expect(json.Unmarshal([]byte(cm.Data["info.json"]), &info)).To(Succeed())
				g.Expect(info["kubernetesVersion"]).To(Equal("v1.29.0"))
				g.Expect(info["openshiftVersion"]).To(Equal("4.15.0"))
				g.Expect(info["clusterId"]).To(Equal("test-cluster-id-12345"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})
})
