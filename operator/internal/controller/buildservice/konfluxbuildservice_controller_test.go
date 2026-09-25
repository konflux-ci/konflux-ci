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
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	securityv1 "github.com/openshift/api/security/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/common"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/contenthash"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	sigyaml "sigs.k8s.io/yaml"
)

const (
	buildServiceNamespace                 = "build-service"
	sccName                               = "appstudio-pipelines-scc"
	metricsServiceName                    = "build-service-controller-manager-metrics-service"
	pipelineConfigMapName                 = "build-pipeline-config"
	pipelineReadOnlyRoleName              = "build-service-build-pipeline-config-read-only"
	pipelineReadOnlyRoleBindingName       = "build-pipeline-config-read-only-binding"
	pipelinesRunnerClusterRoleName        = "appstudio-pipelines-runner"
	pipelinesRunnerClusterRoleBindingName = "build-pipeline-runner-rolebinding"
)

var _ = Describe("KonfluxBuildService Controller", func() {
	// startManagerWithClusterInfo starts a per-test manager with the given ClusterInfo
	// and registers a DeferCleanup to stop it when the It block finishes.
	// A per-test manager is used for every test (rather than a shared suite-level manager)
	// because each test may wire the reconciler with a different ClusterInfo
	// (e.g. OpenShift vs vanilla Kubernetes). Running a single manager for the whole
	// suite while some tests also start their own would cause two managers to reconcile
	// the same objects concurrently, leading to race conditions on status updates.
	newBuildServiceCR := func() *konfluxv1alpha1.KonfluxBuildService {
		return &konfluxv1alpha1.KonfluxBuildService{
			ObjectMeta: metav1.ObjectMeta{Name: CRName},
			Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(
				konfluxv1alpha1.KonfluxBuildServiceConfigSpec{},
				testutil.DefaultComponentMetricsConfig(),
			),
		}
	}

	startManagerWithClusterInfo := func(clusterInfo *clusterinfo.Info) {
		mgrCtx, mgrCancel := context.WithCancel(testEnv.Ctx)
		mgr := testutil.NewTestManager(testEnv)
		Expect((&KonfluxBuildServiceReconciler{
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
			startManagerWithClusterInfo(nil)

			Expect(k8sClient.Create(ctx, newBuildServiceCR())).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, newBuildServiceCR())
			})

			// Wait for the Deployment rather than Ready=True: UpdateComponentStatuses
			// gates Ready=True on ReadyReplicas == Replicas, which never happens in
			// envtest (no kubelet → pods never start). Deployment existence is
			// sufficient proof that the full manifest-apply codepath completed.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      buildControllerManagerDeploymentName,
					Namespace: buildServiceNamespace,
				}, &appsv1.Deployment{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("OpenShift SecurityContextConstraints", func() {
		var buildService *konfluxv1alpha1.KonfluxBuildService

		sccExists := func() bool {
			scc := &securityv1.SecurityContextConstraints{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: sccName}, scc) == nil
		}

		BeforeEach(func() {
			buildService = newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
		})

		AfterEach(func() {
			testutil.DeleteAndWait(ctx, k8sClient, buildService)
			testutil.DeleteAndWait(ctx, k8sClient, &securityv1.SecurityContextConstraints{
				ObjectMeta: metav1.ObjectMeta{Name: sccName},
			})
		})

		It("Should create SCC when running on OpenShift", func() {
			openShiftClusterInfo, err := clusterinfo.DetectWithClient(&buildServiceMockDiscoveryClient{
				resources: map[string]*metav1.APIResourceList{
					"config.openshift.io/v1": {
						APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
					},
				},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())

			startManagerWithClusterInfo(openShiftClusterInfo)

			By("verifying the SCC was created")
			Eventually(sccExists).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(BeTrue())
		})

		It("Should NOT create SCC when NOT running on OpenShift", func() {
			defaultClusterInfo, err := clusterinfo.DetectWithClient(&buildServiceMockDiscoveryClient{
				resources:     map[string]*metav1.APIResourceList{},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())

			startManagerWithClusterInfo(defaultClusterInfo)

			By("waiting for the controller to apply manifests and create the build-service Deployment, then verifying no SCC was created")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      buildControllerManagerDeploymentName,
					Namespace: buildServiceNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			Expect(sccExists()).To(BeFalse())
		})

		It("Should NOT create SCC when ClusterInfo is nil", func() {
			startManagerWithClusterInfo(nil)

			By("waiting for the controller to apply manifests and create the build-service Deployment, then verifying no SCC was created")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      buildControllerManagerDeploymentName,
					Namespace: buildServiceNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			Expect(sccExists()).To(BeFalse())
		})
	})

	Context("OpenShift trusted-ca ConfigMap", func() {
		var buildService *konfluxv1alpha1.KonfluxBuildService

		trustedCAExists := func() bool {
			cm := &corev1.ConfigMap{}
			return k8sClient.Get(ctx, types.NamespacedName{
				Name:      common.TrustedCAConfigMapName,
				Namespace: buildServiceNamespace,
			}, cm) == nil
		}

		trustedCAHasInjectionLabel := func(g Gomega) {
			cm := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      common.TrustedCAConfigMapName,
				Namespace: buildServiceNamespace,
			}, cm)).To(Succeed())
			g.Expect(cm.Labels).To(HaveKeyWithValue(
				common.OpenShiftInjectTrustedCABundleLabel, "true"))
		}

		trustedCAIsGone := func(g Gomega) {
			cm := &corev1.ConfigMap{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      common.TrustedCAConfigMapName,
				Namespace: buildServiceNamespace,
			}, cm)
			g.Expect(errors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
		}

		detectOpenShiftClusterInfo := func() *clusterinfo.Info {
			info, err := clusterinfo.DetectWithClient(&buildServiceMockDiscoveryClient{
				resources: map[string]*metav1.APIResourceList{
					"config.openshift.io/v1": {
						APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
					},
				},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())
			return info
		}

		expectVolumeConfigMapName := func(name string) {
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      buildControllerManagerDeploymentName,
					Namespace: buildServiceNamespace,
				}, dep)).To(Succeed())
				vol := kubernetes.FindVolume(dep.Spec.Template.Spec.Volumes, trustedCAVolumeName)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal(name))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		}

		expectPipelineConfigExists := func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      pipelineConfigMapName,
				Namespace: buildServiceNamespace,
			}, &corev1.ConfigMap{})).To(Succeed())
		}

		BeforeEach(func() {
			buildService = newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
		})

		AfterEach(func() {
			testutil.DeleteAndWait(ctx, k8sClient, buildService)
			testutil.DeleteAndWait(ctx, k8sClient, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: common.TrustedCAConfigMapName, Namespace: buildServiceNamespace},
			})
		})

		It("Should create trusted-ca ConfigMap with injection label when running on OpenShift", func() {
			startManagerWithClusterInfo(detectOpenShiftClusterInfo())

			By("verifying the trusted-ca ConfigMap was created with the injection label")
			Eventually(trustedCAHasInjectionLabel).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should NOT create trusted-ca ConfigMap when NOT running on OpenShift", func() {
			defaultClusterInfo, err := clusterinfo.DetectWithClient(&buildServiceMockDiscoveryClient{
				resources:     map[string]*metav1.APIResourceList{},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())

			startManagerWithClusterInfo(defaultClusterInfo)

			By("waiting for the controller to apply manifests and create the build-service Deployment")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      buildControllerManagerDeploymentName,
					Namespace: buildServiceNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying no trusted-ca ConfigMap was created")
			Expect(trustedCAExists()).To(BeFalse())
		})

		It("Should NOT create trusted-ca ConfigMap when ClusterInfo is nil", func() {
			startManagerWithClusterInfo(nil)

			By("waiting for the controller to apply manifests and create the build-service Deployment")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      buildControllerManagerDeploymentName,
					Namespace: buildServiceNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying no trusted-ca ConfigMap was created")
			Expect(trustedCAExists()).To(BeFalse())
		})

		It("Should NOT create trusted-ca ConfigMap when spec.trustedCA is set", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: buildServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			startManagerWithClusterInfo(detectOpenShiftClusterInfo())

			By("waiting for the controller to retarget the volume to the supplied ConfigMap")
			expectVolumeConfigMapName("custom-ca-bundle")

			By("verifying the operator did not create the platform-injected trusted-ca ConfigMap")
			Consistently(trustedCAIsGone).WithTimeout(3 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("Should mount spec.trustedCA when its name is trusted-ca without creating the platform-injected ConfigMap", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: common.TrustedCAConfigMapName, Namespace: buildServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\nuser\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: common.TrustedCAConfigMapName,
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			startManagerWithClusterInfo(detectOpenShiftClusterInfo())

			By("waiting for the controller to mount the user-supplied ConfigMap named trusted-ca")
			expectVolumeConfigMapName(common.TrustedCAConfigMapName)

			By("verifying the operator did not replace the user-supplied trusted-ca with the platform-injected object")
			Consistently(func(g Gomega) {
				got := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      common.TrustedCAConfigMapName,
					Namespace: buildServiceNamespace,
				}, got)).To(Succeed())
				g.Expect(got.Data).To(HaveKeyWithValue("tls.pem", "-----BEGIN CERTIFICATE-----\nuser\n-----END CERTIFICATE-----\n"))
				g.Expect(got.Labels).NotTo(HaveKey(common.OpenShiftInjectTrustedCABundleLabel))
			}).WithTimeout(3 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("Should delete leftover operator-owned trusted-ca when spec.trustedCA points at another ConfigMap", func() {
			startManagerWithClusterInfo(detectOpenShiftClusterInfo())
			Eventually(trustedCAHasInjectionLabel).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: buildServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			By("waiting for the leftover platform-injected ConfigMap to be removed")
			Eventually(trustedCAIsGone).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			expectPipelineConfigExists()
			expectVolumeConfigMapName("custom-ca-bundle")
		})

		It("Should delete leftover operator-owned trusted-ca when spec.trustedCA.name is trusted-ca", func() {
			startManagerWithClusterInfo(detectOpenShiftClusterInfo())
			Eventually(trustedCAHasInjectionLabel).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: common.TrustedCAConfigMapName,
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			By("waiting for the leftover operator-owned ConfigMap to be removed so the name can be reused")
			Eventually(trustedCAIsGone).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			expectPipelineConfigExists()

			userCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: common.TrustedCAConfigMapName, Namespace: buildServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\nuser\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, userCM)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, userCM)

			expectVolumeConfigMapName(common.TrustedCAConfigMapName)
			Eventually(func(g Gomega) {
				got := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      common.TrustedCAConfigMapName,
					Namespace: buildServiceNamespace,
				}, got)).To(Succeed())
				g.Expect(got.Data).To(HaveKeyWithValue("tls.pem", "-----BEGIN CERTIFICATE-----\nuser\n-----END CERTIFICATE-----\n"))
				g.Expect(got.Labels).NotTo(HaveKey(common.OpenShiftInjectTrustedCABundleLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should create trusted-ca ConfigMap after spec.trustedCA is cleared", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: buildServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			startManagerWithClusterInfo(detectOpenShiftClusterInfo())

			expectVolumeConfigMapName("custom-ca-bundle")
			Expect(trustedCAExists()).To(BeFalse())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = nil
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			By("verifying the platform-injected trusted-ca ConfigMap is created and mounted after trustedCA is cleared")
			Eventually(trustedCAHasInjectionLabel).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			expectVolumeConfigMapName(common.TrustedCAConfigMapName)
		})
	})

	Context("trustedCA volume mount", func() {
		var buildService *konfluxv1alpha1.KonfluxBuildService

		getDeployment := func(g Gomega) *appsv1.Deployment {
			dep := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      buildControllerManagerDeploymentName,
				Namespace: buildServiceNamespace,
			}, dep)).To(Succeed())
			return dep
		}

		findTrustedCAMount := func(dep *appsv1.Deployment) *corev1.VolumeMount {
			manager := kubernetes.FindContainer(dep.Spec.Template.Spec.Containers, buildManagerContainerName)
			if manager == nil {
				return nil
			}
			return kubernetes.FindVolumeMount(manager.VolumeMounts, trustedCAVolumeName)
		}

		findTrustedCAVolume := func(dep *appsv1.Deployment) *corev1.Volume {
			return kubernetes.FindVolume(dep.Spec.Template.Spec.Volumes, trustedCAVolumeName)
		}

		BeforeEach(func() {
			startManagerWithClusterInfo(nil)
			buildService = newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, buildService,
				&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: pipelinesRunnerClusterRoleName}},
				&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: pipelinesRunnerClusterRoleBindingName}},
			)
			// envtest does not GC the leaked controller-manager Deployment.
			// Wait until this CR's reconcile has restored the default extra-file
			// mount so a previous spec's hash is not copied onto a never-stamped apply.
			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal(common.TrustedCAConfigMapName))
				g.Expect(dep.Spec.Template.Annotations).NotTo(HaveKey(trustedCAHashAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should keep the extra-file mount when trustedCA is omitted", func() {
			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				mount := findTrustedCAMount(dep)
				g.Expect(mount).NotTo(BeNil())
				g.Expect(mount.MountPath).To(Equal(trustedCADefaultFileMountPath))
				g.Expect(mount.SubPath).To(Equal(trustedCADefaultFileVolumePath))

				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal(common.TrustedCAConfigMapName))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeTrue())
				g.Expect(vol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
					Key:  trustedCADefaultFileVolumePath,
					Path: trustedCADefaultFileVolumePath,
				}))
				g.Expect(dep.Spec.Template.Annotations).NotTo(HaveKey(trustedCAHashAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should retarget the trusted-ca volume to the specified ConfigMap when trustedCA is set", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: buildServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				mount := findTrustedCAMount(dep)
				g.Expect(mount).NotTo(BeNil())
				g.Expect(mount.MountPath).To(Equal(trustedCADefaultFileMountPath))
				g.Expect(mount.SubPath).To(Equal(trustedCADefaultFileVolumePath))

				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal("custom-ca-bundle"))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeFalse())
				g.Expect(vol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
					Key:  "tls.pem",
					Path: trustedCADefaultFileVolumePath,
				}))
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(
					trustedCAHashAnnotation,
					contenthash.String("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"),
				))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should restore the default ConfigMap reference when trustedCA is cleared", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: buildServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal("custom-ca-bundle"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = nil
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				mount := findTrustedCAMount(dep)
				g.Expect(mount).NotTo(BeNil())
				g.Expect(mount.MountPath).To(Equal(trustedCADefaultFileMountPath))
				g.Expect(mount.SubPath).To(Equal(trustedCADefaultFileVolumePath))

				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal(common.TrustedCAConfigMapName))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeTrue())
				g.Expect(vol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
					Key:  trustedCADefaultFileVolumePath,
					Path: trustedCADefaultFileVolumePath,
				}))
				g.Expect(dep.Spec.Template.Annotations).NotTo(HaveKey(trustedCAHashAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should update the pod-template hash when the trustedCA ConfigMap data changes", func() {
			const originalPEM = "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"
			const rotatedPEM = "-----BEGIN CERTIFICATE-----\nrotated\n-----END CERTIFICATE-----\n"

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: buildServiceNamespace},
				Data:       map[string]string{"tls.pem": originalPEM},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(
					trustedCAHashAnnotation, contenthash.String(originalPEM)))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, cm)).To(Succeed())
			cm.Data["tls.pem"] = rotatedPEM
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(
					trustedCAHashAnnotation, contenthash.String(rotatedPEM)))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should still reconcile when the trustedCA ConfigMap is missing", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal("custom-ca-bundle"))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeFalse())
				g.Expect(dep.Spec.Template.Annotations).NotTo(HaveKey(trustedCAHashAnnotation))

				current := &konfluxv1alpha1.KonfluxBuildService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, current)).To(Succeed())
				g.Expect(current.Status.Conditions).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("should keep the pod-template hash when the trustedCA ConfigMap is deleted after a successful mount", func() {
			const pem = "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle", Namespace: buildServiceNamespace},
				Data:       map[string]string{"tls.pem": pem},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cm)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: "custom-ca-bundle",
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			wantHash := contenthash.String(pem)
			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(trustedCAHashAnnotation, wantHash))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, &corev1.ConfigMap{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Consistently(func(g Gomega) {
				dep := getDeployment(g)
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(trustedCAHashAnnotation, wantHash))
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal("custom-ca-bundle"))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeFalse())
			}, 5*time.Second, time.Second).Should(Succeed())
		})

		It("should retarget from one ConfigMap to another when trustedCA name changes", func() {
			cmA := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle-a", Namespace: buildServiceNamespace},
				Data:       map[string]string{"tls.pem": "-----BEGIN CERTIFICATE-----\na\n-----END CERTIFICATE-----\n"},
			}
			cmB := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-ca-bundle-b", Namespace: buildServiceNamespace},
				Data:       map[string]string{"ca.pem": "-----BEGIN CERTIFICATE-----\nb\n-----END CERTIFICATE-----\n"},
			}
			Expect(k8sClient.Create(ctx, cmA)).To(Succeed())
			Expect(k8sClient.Create(ctx, cmB)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cmA)
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cmB)

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: cmA.Name,
				Key:  "tls.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal(cmA.Name))
				g.Expect(vol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
					Key:  "tls.pem",
					Path: trustedCADefaultFileVolumePath,
				}))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.TrustedCA = &konfluxv1alpha1.TrustedCAConfigMap{
				Name: cmB.Name,
				Key:  "ca.pem",
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			Eventually(func(g Gomega) {
				dep := getDeployment(g)
				vol := findTrustedCAVolume(dep)
				g.Expect(vol).NotTo(BeNil())
				g.Expect(vol.ConfigMap).NotTo(BeNil())
				g.Expect(vol.ConfigMap.Name).To(Equal(cmB.Name))
				g.Expect(vol.ConfigMap.Optional).NotTo(BeNil())
				g.Expect(*vol.ConfigMap.Optional).To(BeFalse())
				g.Expect(vol.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
					Key:  "ca.pem",
					Path: trustedCADefaultFileVolumePath,
				}))
				g.Expect(dep.Spec.Template.Annotations).To(HaveKeyWithValue(
					trustedCAHashAnnotation,
					contenthash.String(cmB.Data["ca.pem"]),
				))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("trustedCA CRD validation", func() {
		newCR := func(name, key string) *konfluxv1alpha1.KonfluxBuildService {
			return &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(
					konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
						TrustedCA: &konfluxv1alpha1.TrustedCAConfigMap{
							Name: name,
							Key:  key,
						},
					},
					testutil.DefaultComponentMetricsConfig(),
				),
			}
		}

		expectInvalidField := func(err error, field string) {
			GinkgoHelper()
			Expect(errors.IsInvalid(err)).To(BeTrue(), "unexpected error: %v", err)
			statusErr, ok := err.(*errors.StatusError)
			Expect(ok).To(BeTrue())
			Expect(statusErr.Status().Details).NotTo(BeNil())
			Expect(statusErr.Status().Details.Causes).NotTo(BeEmpty())
			Expect(statusErr.Status().Details.Causes[0].Field).To(Equal(field))
		}

		It("should accept a trustedCA key of 253 characters", func() {
			cr := newCR("custom-ca-bundle", strings.Repeat("a", 253))
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)
		})

		It("should reject a trustedCA key of 254 characters", func() {
			err := k8sClient.Create(ctx, newCR("custom-ca-bundle", strings.Repeat("a", 254)))
			expectInvalidField(err, "spec.trustedCA.key")
		})

		It("should reject trustedCA keys that are not valid ConfigMap keys", func() {
			for _, key := range []string{"../etc/passwd", "/etc/passwd"} {
				err := k8sClient.Create(ctx, newCR("custom-ca-bundle", key))
				expectInvalidField(err, "spec.trustedCA.key")
			}
		})

		It("should reject trustedCA keys that are '.' or '..'", func() {
			for _, key := range []string{".", ".."} {
				err := k8sClient.Create(ctx, newCR("custom-ca-bundle", key))
				expectInvalidField(err, "spec.trustedCA.key")
			}
		})

		It("should accept a trustedCA name of 253 characters", func() {
			cr := newCR(strings.Repeat("a", 253), "tls.pem")
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)
		})

		It("should reject a trustedCA name of 254 characters", func() {
			err := k8sClient.Create(ctx, newCR(strings.Repeat("a", 254), "tls.pem"))
			expectInvalidField(err, "spec.trustedCA.name")
		})

		It("should reject an empty trustedCA name", func() {
			err := k8sClient.Create(ctx, newCR("", "tls.pem"))
			expectInvalidField(err, "spec.trustedCA.name")
		})

		It("should reject trustedCA names that are not DNS-1123 subdomains", func() {
			for _, name := range []string{"NotValid", "-leading-dash", "trailing-dash-", "has_underscore"} {
				err := k8sClient.Create(ctx, newCR(name, "tls.pem"))
				expectInvalidField(err, "spec.trustedCA.name")
			}
		})
	})

	Context("PipelineConfig", func() {
		var configMapNN types.NamespacedName

		getConfigMapData := func(g Gomega, ctx context.Context) map[string]interface{} {
			cm := &corev1.ConfigMap{}
			g.ExpectWithOffset(1, k8sClient.Get(ctx, configMapNN, cm)).To(Succeed())

			var data map[string]interface{}
			g.ExpectWithOffset(1, sigyaml.Unmarshal([]byte(cm.Data["config.yaml"]), &data)).To(Succeed())
			return data
		}

		getPipelines := func(g Gomega, ctx context.Context) []interface{} {
			data := getConfigMapData(g, ctx)
			pipelines, ok := data["pipelines"].([]interface{})
			g.ExpectWithOffset(1, ok).To(BeTrue(), "pipelines should be an array")
			return pipelines
		}

		findPipelineByName := func(pipelines []interface{}, name string) map[string]interface{} {
			for _, p := range pipelines {
				pipeline, ok := p.(map[string]interface{})
				ExpectWithOffset(1, ok).To(BeTrue(), "pipeline entry should be a map")
				if pipeline["name"] == name {
					return pipeline
				}
			}
			return nil
		}

		BeforeEach(func() {
			configMapNN = types.NamespacedName{
				Name:      pipelineConfigMapName,
				Namespace: buildServiceNamespace,
			}
		})

		It("Should create ConfigMap with defaults when pipelineConfig is not set", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			By("creating a KonfluxBuildService with no pipelineConfig")
			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			By("waiting for the ConfigMap to be created with default pipelines")
			Eventually(func(g Gomega) {
				pipelines := getPipelines(g, ctx)
				g.Expect(pipelines).ToNot(BeEmpty(), "should have default pipelines")

				dockerBuild := findPipelineByName(pipelines, "docker-build-oci-ta-min")
				g.Expect(dockerBuild).NotTo(BeNil(), "docker-build-oci-ta-min should exist")
				g.Expect(dockerBuild["bundle"]).To(ContainSubstring("quay.io/konflux-ci/tekton-catalog"))

				data := getConfigMapData(g, ctx)
				g.Expect(data["default-pipeline-name"]).To(Equal("docker-build-oci-ta-min"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should override a default pipeline bundle", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			By("waiting for initial reconciliation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMapNN.Name, Namespace: configMapNN.Namespace}, &corev1.ConfigMap{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating the CR with a pipeline override")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.PipelineConfig = &konfluxv1alpha1.PipelineConfigSpec{
				Pipelines: []konfluxv1alpha1.PipelineSpec{
					{Name: "docker-build-oci-ta-min", Bundle: "quay.io/custom/pipeline@sha256:abcd1234"},
				},
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			By("verifying the bundle was overridden and other defaults remain")
			Eventually(func(g Gomega) {
				pipelines := getPipelines(g, ctx)
				dockerBuild := findPipelineByName(pipelines, "docker-build-oci-ta-min")
				g.Expect(dockerBuild).NotTo(BeNil())
				g.Expect(dockerBuild["bundle"]).To(Equal("quay.io/custom/pipeline@sha256:abcd1234"))

				fbcBuilder := findPipelineByName(pipelines, "fbc-builder")
				g.Expect(fbcBuilder).NotTo(BeNil(), "other defaults should remain")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should remove a pipeline with removed: true", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			By("waiting for initial reconciliation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMapNN.Name, Namespace: configMapNN.Namespace}, &corev1.ConfigMap{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating the CR to remove fbc-builder")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.PipelineConfig = &konfluxv1alpha1.PipelineConfigSpec{
				Pipelines: []konfluxv1alpha1.PipelineSpec{
					{Name: "fbc-builder", Removed: true},
				},
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			By("verifying fbc-builder was removed and other defaults remain")
			Eventually(func(g Gomega) {
				pipelines := getPipelines(g, ctx)
				g.Expect(findPipelineByName(pipelines, "fbc-builder")).To(BeNil(), "fbc-builder should be removed")
				g.Expect(findPipelineByName(pipelines, "docker-build-oci-ta-min")).NotTo(BeNil(), "other defaults should remain")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should add a custom pipeline alongside defaults", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			By("waiting for initial reconciliation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMapNN.Name, Namespace: configMapNN.Namespace}, &corev1.ConfigMap{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating the CR with a custom pipeline")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.PipelineConfig = &konfluxv1alpha1.PipelineConfigSpec{
				Pipelines: []konfluxv1alpha1.PipelineSpec{
					{Name: "custom-pipeline", Bundle: "quay.io/custom/my-pipeline@sha256:xyz789"},
				},
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			By("verifying the custom pipeline exists alongside defaults")
			Eventually(func(g Gomega) {
				pipelines := getPipelines(g, ctx)
				customPipeline := findPipelineByName(pipelines, "custom-pipeline")
				g.Expect(customPipeline).NotTo(BeNil())
				g.Expect(customPipeline["bundle"]).To(Equal("quay.io/custom/my-pipeline@sha256:xyz789"))
				g.Expect(findPipelineByName(pipelines, "docker-build-oci-ta-min")).NotTo(BeNil(), "defaults should remain")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should preserve description when overriding a default pipeline bundle", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			By("waiting for defaults and capturing the description")
			var defaultDescription interface{}
			Eventually(func(g Gomega) {
				pipelines := getPipelines(g, ctx)
				dockerBuild := findPipelineByName(pipelines, "docker-build-oci-ta-min")
				g.Expect(dockerBuild).NotTo(BeNil())
				g.Expect(dockerBuild["description"]).NotTo(BeEmpty(), "default should have a description")
				defaultDescription = dockerBuild["description"]
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating the CR with a bundle override")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.PipelineConfig = &konfluxv1alpha1.PipelineConfigSpec{
				Pipelines: []konfluxv1alpha1.PipelineSpec{
					{Name: "docker-build-oci-ta-min", Bundle: "quay.io/custom/pipeline@sha256:override123"},
				},
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			By("verifying the bundle was overridden but description is preserved")
			Eventually(func(g Gomega) {
				pipelines := getPipelines(g, ctx)
				dockerBuild := findPipelineByName(pipelines, "docker-build-oci-ta-min")
				g.Expect(dockerBuild).NotTo(BeNil())
				g.Expect(dockerBuild["bundle"]).To(Equal("quay.io/custom/pipeline@sha256:override123"))
				g.Expect(dockerBuild["description"]).To(Equal(defaultDescription), "description should be preserved from defaults")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should use only user-specified pipelines when removeDefaults is true", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						RemoveDefaults:      true,
						DefaultPipelineName: "my-only-pipeline",
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "my-only-pipeline", Bundle: "quay.io/custom/only@sha256:abc123"},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			By("verifying only user-specified pipelines exist")
			Eventually(func(g Gomega) {
				pipelines := getPipelines(g, ctx)
				g.Expect(pipelines).To(HaveLen(1), "should have exactly one pipeline")

				myPipeline := findPipelineByName(pipelines, "my-only-pipeline")
				g.Expect(myPipeline).NotTo(BeNil())
				g.Expect(myPipeline["bundle"]).To(Equal("quay.io/custom/only@sha256:abc123"))

				data := getConfigMapData(g, ctx)
				g.Expect(data["default-pipeline-name"]).To(Equal("my-only-pipeline"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should use user-provided description for override", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "docker-build-oci-ta-min", Bundle: "quay.io/custom/pipeline@sha256:override123", Description: "user custom description"},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			Eventually(func(g Gomega) {
				pipelines := getPipelines(g, ctx)
				dockerBuild := findPipelineByName(pipelines, "docker-build-oci-ta-min")
				g.Expect(dockerBuild).NotTo(BeNil())
				g.Expect(dockerBuild["bundle"]).To(Equal("quay.io/custom/pipeline@sha256:override123"))
				g.Expect(dockerBuild["description"]).To(Equal("user custom description"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should include description for a new custom pipeline", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "my-custom-pipeline", Bundle: "quay.io/custom/my-pipeline@sha256:new123", Description: "brand new pipeline"},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			Eventually(func(g Gomega) {
				pipelines := getPipelines(g, ctx)
				custom := findPipelineByName(pipelines, "my-custom-pipeline")
				g.Expect(custom).NotTo(BeNil())
				g.Expect(custom["bundle"]).To(Equal("quay.io/custom/my-pipeline@sha256:new123"))
				g.Expect(custom["description"]).To(Equal("brand new pipeline"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should auto-select first pipeline when current default is removed", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			By("waiting for initial reconciliation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configMapNN.Name, Namespace: configMapNN.Namespace}, &corev1.ConfigMap{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("removing the current default pipeline without specifying a replacement")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, buildService)).To(Succeed())
			buildService.Spec.PipelineConfig = &konfluxv1alpha1.PipelineConfigSpec{
				Pipelines: []konfluxv1alpha1.PipelineSpec{
					{Name: "docker-build-oci-ta-min", Removed: true},
				},
			}
			Expect(k8sClient.Update(ctx, buildService)).To(Succeed())

			By("verifying the controller auto-selected a new default pipeline")
			Eventually(func(g Gomega) {
				pipelines := getPipelines(g, ctx)
				g.Expect(findPipelineByName(pipelines, "docker-build-oci-ta-min")).To(BeNil(), "removed pipeline should be gone")
				g.Expect(pipelines).NotTo(BeEmpty(), "other pipelines should remain")

				data := getConfigMapData(g, ctx)
				defaultName, ok := data["default-pipeline-name"].(string)
				g.Expect(ok).To(BeTrue())
				g.Expect(defaultName).NotTo(Equal("docker-build-oci-ta-min"), "should not point to removed pipeline")
				g.Expect(findPipelineByName(pipelines, defaultName)).NotTo(BeNil(), "default should reference an existing pipeline")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Self-healing", func() {
		It("recreates Deployment when deleted", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			deploymentNN := types.NamespacedName{
				Name:      buildControllerManagerDeploymentName,
				Namespace: buildServiceNamespace,
			}

			By("waiting for initial Deployment creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, deploymentNN, &appsv1.Deployment{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Deployment")
			Expect(k8sClient.Delete(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: deploymentNN.Name, Namespace: deploymentNN.Namespace},
			})).To(Succeed())

			By("verifying the Deployment is recreated with correct spec")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				g.Expect(dep.Labels).To(HaveKeyWithValue("control-plane", "controller-manager"))
				manager := kubernetes.FindContainer(dep.Spec.Template.Spec.Containers, buildManagerContainerName)
				g.Expect(manager).NotTo(BeNil(), "manager container should exist")
				g.Expect(manager.Image).NotTo(BeEmpty(), "manager container image should be set")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ServiceAccount when deleted", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			saNN := types.NamespacedName{
				Name:      buildControllerManagerDeploymentName,
				Namespace: buildServiceNamespace,
			}

			By("waiting for initial ServiceAccount creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, saNN, &corev1.ServiceAccount{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ServiceAccount")
			Expect(k8sClient.Delete(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: saNN.Name, Namespace: saNN.Namespace},
			})).To(Succeed())

			By("verifying the ServiceAccount is recreated with ownership labels")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates Service when deleted", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: buildServiceNamespace,
			}

			By("waiting for initial Service creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, svcNN, &corev1.Service{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Service")
			Expect(k8sClient.Delete(ctx, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: svcNN.Name, Namespace: svcNN.Namespace},
			})).To(Succeed())

			By("verifying the Service is recreated with ownership labels")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ConfigMap when deleted", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			cmNN := types.NamespacedName{
				Name:      pipelineConfigMapName,
				Namespace: buildServiceNamespace,
			}

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
		})

		It("recreates Role when deleted", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			roleNN := types.NamespacedName{
				Name:      pipelineReadOnlyRoleName,
				Namespace: buildServiceNamespace,
			}

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
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			rbNN := types.NamespacedName{
				Name:      pipelineReadOnlyRoleBindingName,
				Namespace: buildServiceNamespace,
			}

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

		It("recreates ClusterRole when deleted", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, buildService, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: pipelinesRunnerClusterRoleName},
			})

			crNN := types.NamespacedName{Name: pipelinesRunnerClusterRoleName}

			By("waiting for initial ClusterRole creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crNN, &rbacv1.ClusterRole{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ClusterRole")
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: crNN.Name},
			})).To(Succeed())

			By("verifying the ClusterRole is recreated with ownership labels")
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				g.Expect(cr.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ClusterRoleBinding when deleted", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, buildService, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: pipelinesRunnerClusterRoleBindingName},
			})

			crbNN := types.NamespacedName{Name: pipelinesRunnerClusterRoleBindingName}

			By("waiting for initial ClusterRoleBinding creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crbNN, &rbacv1.ClusterRoleBinding{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ClusterRoleBinding")
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: crbNN.Name},
			})).To(Succeed())

			By("verifying the ClusterRoleBinding is recreated with ownership labels")
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				g.Expect(crb.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates SecurityContextConstraints when deleted on OpenShift", func(ctx context.Context) {
			openShiftClusterInfo, err := clusterinfo.DetectWithClient(&buildServiceMockDiscoveryClient{
				resources: map[string]*metav1.APIResourceList{
					"config.openshift.io/v1": {
						APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
					},
				},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())

			startManagerWithClusterInfo(openShiftClusterInfo)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, buildService,
				&securityv1.SecurityContextConstraints{
					ObjectMeta: metav1.ObjectMeta{Name: sccName},
				},
			)

			By("waiting for initial SCC creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: sccName}, &securityv1.SecurityContextConstraints{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the SCC")
			Expect(k8sClient.Delete(ctx, &securityv1.SecurityContextConstraints{
				ObjectMeta: metav1.ObjectMeta{Name: sccName},
			})).To(Succeed())

			By("verifying the SCC is recreated with correct spec")
			Eventually(func(g Gomega) {
				scc := &securityv1.SecurityContextConstraints{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: sccName}, scc)).To(Succeed())
				g.Expect(scc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(scc.Priority).NotTo(BeNil())
				g.Expect(*scc.Priority).To(Equal(int32(10)))
				g.Expect(scc.RunAsUser.Type).To(Equal(securityv1.RunAsUserStrategyRunAsAny))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Self-healing SCC not watched on non-OpenShift", func() {
		It("does not recreate SCC when deleted on non-OpenShift", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			By("waiting for the controller to finish reconciliation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      buildControllerManagerDeploymentName,
					Namespace: buildServiceNamespace,
				}, &appsv1.Deployment{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("manually creating an SCC (simulating out-of-band creation)")
			scc := &securityv1.SecurityContextConstraints{
				ObjectMeta: metav1.ObjectMeta{Name: sccName},
			}
			Expect(k8sClient.Create(ctx, scc)).To(Succeed())

			By("deleting the SCC")
			Expect(k8sClient.Delete(ctx, scc)).To(Succeed())

			By("verifying the SCC stays deleted (no Owns watch to trigger re-reconcile)")
			Consistently(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: sccName}, &securityv1.SecurityContextConstraints{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, 5*time.Second, time.Second).Should(Succeed())
		})
	})

	Context("Drift correction", func() {
		It("restores Deployment image when modified", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			deploymentNN := types.NamespacedName{
				Name:      buildControllerManagerDeploymentName,
				Namespace: buildServiceNamespace,
			}

			By("waiting for initial Deployment creation")
			var originalImage string
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				manager := kubernetes.FindContainer(dep.Spec.Template.Spec.Containers, buildManagerContainerName)
				g.Expect(manager).NotTo(BeNil())
				originalImage = manager.Image
				g.Expect(originalImage).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Deployment image")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				manager := kubernetes.FindContainer(dep.Spec.Template.Spec.Containers, buildManagerContainerName)
				g.Expect(manager).NotTo(BeNil())
				manager.Image = "tampered-image:latest"
				g.Expect(k8sClient.Update(ctx, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Deployment image is restored")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				m := kubernetes.FindContainer(dep.Spec.Template.Spec.Containers, buildManagerContainerName)
				g.Expect(m).NotTo(BeNil())
				g.Expect(m.Image).To(Equal(originalImage))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ServiceAccount labels when stripped", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			saNN := types.NamespacedName{
				Name:      buildControllerManagerDeploymentName,
				Namespace: buildServiceNamespace,
			}

			By("waiting for initial ServiceAccount creation")
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

		It("restores Service labels when stripped", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: buildServiceNamespace,
			}

			By("waiting for initial Service creation with ownership labels")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Service")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				delete(svc.Labels, constant.KonfluxOwnerLabel)
				delete(svc.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, svc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Service labels are restored")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Service spec when modified", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			svcNN := types.NamespacedName{
				Name:      metricsServiceName,
				Namespace: buildServiceNamespace,
			}

			By("waiting for initial Service creation")
			var originalTargetPort int32
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Spec.Ports).NotTo(BeEmpty())
				originalTargetPort = svc.Spec.Ports[0].TargetPort.IntVal
				g.Expect(originalTargetPort).NotTo(BeZero())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Service target port")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				svc.Spec.Ports[0].TargetPort.IntVal = 9999
				g.Expect(k8sClient.Update(ctx, svc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Service target port is restored")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Spec.Ports).NotTo(BeEmpty())
				g.Expect(svc.Spec.Ports[0].TargetPort.IntVal).To(Equal(originalTargetPort))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ConfigMap data when modified", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			cmNN := types.NamespacedName{
				Name:      pipelineConfigMapName,
				Namespace: buildServiceNamespace,
			}

			By("waiting for initial ConfigMap creation")
			var originalData map[string]string
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				g.Expect(cm.Data).NotTo(BeEmpty())
				originalData = cm.Data
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ConfigMap data")
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				cm.Data = map[string]string{"config.yaml": "tampered"}
				g.Expect(k8sClient.Update(ctx, cm)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ConfigMap data is restored")
			Eventually(func(g Gomega) {
				cm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
				g.Expect(cm.Data).To(Equal(originalData))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Namespace labels when stripped", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			nsNN := types.NamespacedName{Name: buildServiceNamespace}

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

		It("restores Role rules when modified", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			roleNN := types.NamespacedName{
				Name:      pipelineReadOnlyRoleName,
				Namespace: buildServiceNamespace,
			}

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
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, buildService)

			rbNN := types.NamespacedName{
				Name:      pipelineReadOnlyRoleBindingName,
				Namespace: buildServiceNamespace,
			}

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

		It("restores ClusterRole rules when modified", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, buildService, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: pipelinesRunnerClusterRoleName},
			})

			crNN := types.NamespacedName{Name: pipelinesRunnerClusterRoleName}

			By("waiting for initial ClusterRole creation")
			var originalRules []rbacv1.PolicyRule
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				g.Expect(cr.Rules).NotTo(BeEmpty())
				originalRules = cr.Rules
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ClusterRole rules")
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				cr.Rules = []rbacv1.PolicyRule{{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"delete"},
				}}
				g.Expect(k8sClient.Update(ctx, cr)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ClusterRole rules are restored")
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				g.Expect(cr.Rules).To(Equal(originalRules))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ClusterRoleBinding subjects when modified", func(ctx context.Context) {
			startManagerWithClusterInfo(nil)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, buildService, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: pipelinesRunnerClusterRoleBindingName},
			})

			crbNN := types.NamespacedName{Name: pipelinesRunnerClusterRoleBindingName}

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

		It("restores SecurityContextConstraints spec when modified", func(ctx context.Context) {
			openShiftClusterInfo, err := clusterinfo.DetectWithClient(&buildServiceMockDiscoveryClient{
				resources: map[string]*metav1.APIResourceList{
					"config.openshift.io/v1": {
						APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
					},
				},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())

			startManagerWithClusterInfo(openShiftClusterInfo)

			buildService := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, buildService, &securityv1.SecurityContextConstraints{
				ObjectMeta: metav1.ObjectMeta{Name: sccName},
			})

			sccNN := types.NamespacedName{Name: sccName}

			By("waiting for initial SCC creation")
			Eventually(func(g Gomega) {
				scc := &securityv1.SecurityContextConstraints{}
				g.Expect(k8sClient.Get(ctx, sccNN, scc)).To(Succeed())
				g.Expect(scc.RunAsUser.Type).To(Equal(securityv1.RunAsUserStrategyRunAsAny))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the SCC RunAsUser strategy")
			Eventually(func(g Gomega) {
				scc := &securityv1.SecurityContextConstraints{}
				g.Expect(k8sClient.Get(ctx, sccNN, scc)).To(Succeed())
				scc.RunAsUser.Type = securityv1.RunAsUserStrategyMustRunAsRange
				g.Expect(k8sClient.Update(ctx, scc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the SCC RunAsUser strategy is restored")
			Eventually(func(g Gomega) {
				scc := &securityv1.SecurityContextConstraints{}
				g.Expect(k8sClient.Get(ctx, sccNN, scc)).To(Succeed())
				g.Expect(scc.RunAsUser.Type).To(Equal(securityv1.RunAsUserStrategyRunAsAny))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("CEL Validation", func() {
		It("Should reject creation with a name other than 'konflux-build-service'", func(ctx context.Context) {
			bs := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: "wrong-name"},
			}
			err := k8sClient.Create(ctx, bs)
			Expect(err).To(HaveOccurred(), "creation with wrong name should be rejected")
			Expect(err.Error()).To(ContainSubstring("konflux-build-service"))
		})

		It("Should allow creation with the required name", func(ctx context.Context) {
			bs := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, bs)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, bs)
		})

		It("Should reject a pipeline with both bundle and removed: true", func(ctx context.Context) {
			bs := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "test-pipeline", Bundle: "quay.io/test/bundle:latest", Removed: true},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			err := k8sClient.Create(ctx, bs)
			Expect(err).To(HaveOccurred(), "bundle + removed should be rejected")
			Expect(err.Error()).To(ContainSubstring("bundle must not be set when removed is true"))
		})

		It("Should reject a pipeline without bundle and without removed", func(ctx context.Context) {
			bs := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "test-pipeline"},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			err := k8sClient.Create(ctx, bs)
			Expect(err).To(HaveOccurred(), "missing bundle without removed should be rejected")
			Expect(err.Error()).To(ContainSubstring("bundle is required when removed is not true"))
		})

		It("Should accept a pipeline with bundle set", func(ctx context.Context) {
			bs := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "test-pipeline", Bundle: "quay.io/test/bundle:latest"},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			Expect(k8sClient.Create(ctx, bs)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, bs)
		})

		It("Should accept a pipeline with removed: true and no bundle", func(ctx context.Context) {
			bs := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "fbc-builder", Removed: true},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			Expect(k8sClient.Create(ctx, bs)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, bs)
		})

		It("Should reject update that adds invalid pipeline spec", func(ctx context.Context) {
			bs := newBuildServiceCR()
			Expect(k8sClient.Create(ctx, bs)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, bs)

			By("updating to add a pipeline with bundle + removed")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, bs)).To(Succeed())
			bs.Spec.PipelineConfig = &konfluxv1alpha1.PipelineConfigSpec{
				Pipelines: []konfluxv1alpha1.PipelineSpec{
					{Name: "test-pipeline", Bundle: "quay.io/test/bundle:latest", Removed: true},
				},
			}
			err := k8sClient.Update(ctx, bs)
			Expect(err).To(HaveOccurred(), "update with invalid pipeline should be rejected")
			Expect(err.Error()).To(ContainSubstring("bundle must not be set when removed is true"))
		})

		It("Should reject removeDefaults without defaultPipelineName", func(ctx context.Context) {
			bs := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						RemoveDefaults: true,
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "my-pipeline", Bundle: "quay.io/test/bundle:latest"},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			err := k8sClient.Create(ctx, bs)
			Expect(err).To(HaveOccurred(), "removeDefaults without defaultPipelineName should be rejected")
			Expect(err.Error()).To(ContainSubstring("defaultPipelineName is required when removeDefaults is true"))
		})

		It("Should reject removeDefaults without any non-removed pipelines", func(ctx context.Context) {
			bs := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						RemoveDefaults:      true,
						DefaultPipelineName: "some-pipeline",
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "some-pipeline", Removed: true},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			err := k8sClient.Create(ctx, bs)
			Expect(err).To(HaveOccurred(), "removeDefaults with only removed pipelines should be rejected")
		})

		It("Should reject removeDefaults when defaultPipelineName does not match a pipeline", func(ctx context.Context) {
			bs := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						RemoveDefaults:      true,
						DefaultPipelineName: "nonexistent",
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "my-pipeline", Bundle: "quay.io/test/bundle:latest"},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			err := k8sClient.Create(ctx, bs)
			Expect(err).To(HaveOccurred(), "defaultPipelineName not in pipelines list should be rejected")
			Expect(err.Error()).To(ContainSubstring("defaultPipelineName must reference a pipeline"))
		})

		It("Should reject defaultPipelineName referencing a removed pipeline", func(ctx context.Context) {
			bs := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						DefaultPipelineName: "fbc-builder",
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "fbc-builder", Removed: true},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			err := k8sClient.Create(ctx, bs)
			Expect(err).To(HaveOccurred(), "defaultPipelineName referencing removed pipeline should be rejected")
			Expect(err.Error()).To(ContainSubstring("defaultPipelineName must not reference a pipeline that is being removed"))
		})

		It("Should accept removeDefaults with valid defaultPipelineName and pipelines", func(ctx context.Context) {
			bs := &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.NewKonfluxBuildServiceSpec(konfluxv1alpha1.KonfluxBuildServiceConfigSpec{
					PipelineConfig: &konfluxv1alpha1.PipelineConfigSpec{
						RemoveDefaults:      true,
						DefaultPipelineName: "my-pipeline",
						Pipelines: []konfluxv1alpha1.PipelineSpec{
							{Name: "my-pipeline", Bundle: "quay.io/test/bundle:latest"},
						},
					},
				}, testutil.DefaultComponentMetricsConfig()),
			}
			Expect(k8sClient.Create(ctx, bs)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, bs)
		})
	})
})

// buildServiceMockDiscoveryClient implements clusterinfo.DiscoveryClient for testing.
type buildServiceMockDiscoveryClient struct {
	resources     map[string]*metav1.APIResourceList
	serverVersion *version.Info
}

func (m *buildServiceMockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if r, ok := m.resources[groupVersion]; ok {
		return r, nil
	}
	return nil, errors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *buildServiceMockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return m.serverVersion, nil
}
