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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

var _ = Describe("KonfluxInfo Controller", func() {
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			infoRes := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, infoRes)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, infoRes)

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

		It("should allow proxy URLs without credentials", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxInfo{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxInfoSpec{
					ClusterConfig: &konfluxv1alpha1.ClusterConfig{
						Data: &konfluxv1alpha1.ClusterConfigData{
							HTTPProxy:                   "squid.caching.svc.cluster.local:3128",
							PackageRegistryProxyNpmURL:  "https://npm-proxy.internal.svc:8080",
							PackageRegistryProxyYarnURL: "https://yarn-proxy.internal.svc:8080",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, cr)
		})
	})
})
