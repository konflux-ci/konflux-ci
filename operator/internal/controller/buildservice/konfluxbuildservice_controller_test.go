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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	securityv1 "github.com/openshift/api/security/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

const buildServiceNamespace = "build-service"

var _ = Describe("KonfluxBuildService Controller", func() {
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxBuildService{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
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
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})
	})

	Context("OpenShift SecurityContextConstraints", func() {
		const sccName = "appstudio-pipelines-scc"

		var buildService *konfluxv1alpha1.KonfluxBuildService

		sccExists := func() bool {
			scc := &securityv1.SecurityContextConstraints{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: sccName}, scc) == nil
		}

		// startManagerWithClusterInfo starts a per-test manager with the given ClusterInfo
		// and registers a DeferCleanup to stop it when the It block finishes.
		startManagerWithClusterInfo := func(clusterInfo *clusterinfo.Info) {
			mgrCtx, mgrCancel := context.WithCancel(testEnv.Ctx)
			DeferCleanup(mgrCancel)
			mgr := testutil.NewTestManager(testEnv)
			Expect((&KonfluxBuildServiceReconciler{
				Client:      mgr.GetClient(),
				Scheme:      mgr.GetScheme(),
				ObjectStore: objectStore,
				ClusterInfo: clusterInfo,
			}).SetupWithManager(mgr)).To(Succeed())
			testutil.StartManagerWithContext(mgrCtx, mgr)
		}

		BeforeEach(func() {
			buildService = &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
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
			Eventually(sccExists).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(BeTrue())
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
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
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
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
			Expect(sccExists()).To(BeFalse())
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
