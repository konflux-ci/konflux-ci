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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	securityv1 "github.com/openshift/api/security/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

var _ = Describe("KonfluxBuildService Controller", func() {
	Context("When reconciling a resource", func() {

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      CRName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		konfluxbuildservice := &konfluxv1alpha1.KonfluxBuildService{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KonfluxBuildService")
			err := k8sClient.Get(ctx, typeNamespacedName, konfluxbuildservice)
			if err != nil && errors.IsNotFound(err) {
				resource := &konfluxv1alpha1.KonfluxBuildService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      CRName,
						Namespace: "default",
					},
					Spec: konfluxv1alpha1.KonfluxBuildServiceSpec{},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &konfluxv1alpha1.KonfluxBuildService{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KonfluxBuildService")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KonfluxBuildServiceReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})

	Context("OpenShift SecurityContextConstraints", func() {
		const sccName = "appstudio-pipelines-scc"

		var (
			ctx                  context.Context
			buildService         *konfluxv1alpha1.KonfluxBuildService
			reconciler           *KonfluxBuildServiceReconciler
			openShiftClusterInfo *clusterinfo.Info
			defaultClusterInfo   *clusterinfo.Info
			typeNamespacedName   types.NamespacedName
		)

		sccExists := func(ctx context.Context) bool {
			scc := &securityv1.SecurityContextConstraints{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: sccName}, scc)
			return err == nil
		}

		reconcileBuildService := func(ctx context.Context) {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		BeforeEach(func() {
			ctx = context.Background()
			typeNamespacedName = types.NamespacedName{
				Name:      CRName,
				Namespace: "default",
			}

			By("cleaning up any existing SCC from previous tests")
			existingSCC := &securityv1.SecurityContextConstraints{
				ObjectMeta: metav1.ObjectMeta{
					Name: sccName,
				},
			}
			_ = k8sClient.Delete(ctx, existingSCC)

			By("creating mock cluster info for OpenShift and non-OpenShift platforms")
			var err error
			openShiftClusterInfo, err = clusterinfo.DetectWithClient(&buildServiceMockDiscoveryClient{
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

			defaultClusterInfo, err = clusterinfo.DetectWithClient(&buildServiceMockDiscoveryClient{
				resources:     map[string]*metav1.APIResourceList{},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())

			By("creating the KonfluxBuildService resource")
			buildService = &konfluxv1alpha1.KonfluxBuildService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      CRName,
					Namespace: "default",
				},
				Spec: konfluxv1alpha1.KonfluxBuildServiceSpec{},
			}
			err = k8sClient.Get(ctx, typeNamespacedName, &konfluxv1alpha1.KonfluxBuildService{})
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, buildService)).To(Succeed())
			}

			reconciler = &KonfluxBuildServiceReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
				ClusterInfo: nil, // Will be set in individual tests
			}
		})

		AfterEach(func() {
			By("cleaning up the SCC")
			existingSCC := &securityv1.SecurityContextConstraints{
				ObjectMeta: metav1.ObjectMeta{
					Name: sccName,
				},
			}
			_ = k8sClient.Delete(ctx, existingSCC)

			By("cleaning up KonfluxBuildService resource")
			_ = k8sClient.Delete(ctx, buildService)
		})

		It("Should create SCC when running on OpenShift", func() {
			By("setting ClusterInfo to OpenShift")
			reconciler.ClusterInfo = openShiftClusterInfo

			By("reconciling the resource")
			reconcileBuildService(ctx)

			By("verifying the SCC was created")
			Expect(sccExists(ctx)).To(BeTrue())
		})

		It("Should NOT create SCC when NOT running on OpenShift", func() {
			By("setting ClusterInfo to non-OpenShift")
			reconciler.ClusterInfo = defaultClusterInfo

			By("reconciling the resource")
			reconcileBuildService(ctx)

			By("verifying the SCC was NOT created")
			Expect(sccExists(ctx)).To(BeFalse())
		})

		It("Should NOT create SCC when ClusterInfo is nil", func() {
			By("keeping ClusterInfo as nil")
			reconciler.ClusterInfo = nil

			By("reconciling the resource")
			reconcileBuildService(ctx)

			By("verifying the SCC was NOT created")
			Expect(sccExists(ctx)).To(BeFalse())
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
