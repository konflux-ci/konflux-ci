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

package rbac

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const (
	adminBatchClusterRole       = "konflux-admin-user-actions-batch"
	adminCoreClusterRole        = "konflux-admin-user-actions-core"
	adminExtraClusterRole       = "konflux-admin-user-actions-extra"
	builderBotClusterRole       = "konflux-builder-bot-actions"
	contributorCoreClusterRole  = "konflux-contributor-user-actions-core"
	contributorExtraClusterRole = "konflux-contributor-user-actions-extra"
	maintainerCoreClusterRole   = "konflux-maintainer-user-actions-core"
	maintainerExtraClusterRole  = "konflux-maintainer-user-actions-extra"
	releaserBotClusterRole      = "konflux-releaser-bot-actions"
	selfAccessClusterRole       = "konflux-self-access-reviewer"
	viewerCoreClusterRole       = "konflux-viewer-user-actions-core"
	viewerExtraClusterRole      = "konflux-viewer-user-actions-extra"
)

// rbacClusterScopedChildren returns all cluster-scoped resources that the reconciler creates.
// envtest has no garbage collector, so these must be explicitly cleaned up after each test.
func rbacClusterScopedChildren() []client.Object {
	return []client.Object{
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: adminBatchClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: adminCoreClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: adminExtraClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: builderBotClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: contributorCoreClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: contributorExtraClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: maintainerCoreClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: maintainerExtraClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: releaserBotClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: selfAccessClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: viewerCoreClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: viewerExtraClusterRole}},
	}
}

var _ = Describe("KonfluxRBAC Controller", func() {
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			rbacRes := &konfluxv1alpha1.KonfluxRBAC{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, rbacRes)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, rbacRes, rbacClusterScopedChildren()...)

			// The rbac manifests contain only ClusterRoles — no Deployments — so
			// Ready=True is a reliable sentinel.
			Eventually(func(g Gomega) {
				cr := &konfluxv1alpha1.KonfluxRBAC{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, cr)).To(Succeed())
				g.Expect(condition.IsConditionTrue(cr, condition.TypeReady)).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying representative ClusterRoles were created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: adminBatchClusterRole}, &rbacv1.ClusterRole{})).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: builderBotClusterRole}, &rbacv1.ClusterRole{})).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: releaserBotClusterRole}, &rbacv1.ClusterRole{})).To(Succeed())
		})
	})

	Context("Self-healing", func() {
		DescribeTable("recreates ClusterRole when deleted",
			func(ctx context.Context, roleName string) {
				rbacRes := &konfluxv1alpha1.KonfluxRBAC{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, rbacRes)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, rbacRes, rbacClusterScopedChildren()...)

				crNN := types.NamespacedName{Name: roleName}

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
					g.Expect(cr.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
					g.Expect(cr.Labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.RBAC)))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			Entry("admin-user-actions-core", adminCoreClusterRole),
			Entry("releaser-bot-actions", releaserBotClusterRole),
			Entry("self-access-reviewer", selfAccessClusterRole),
		)
	})

	Context("Drift correction", func() {
		DescribeTable("restores ClusterRole rules when modified",
			func(ctx context.Context, roleName string) {
				rbacRes := &konfluxv1alpha1.KonfluxRBAC{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, rbacRes)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, rbacRes, rbacClusterScopedChildren()...)

				crNN := types.NamespacedName{Name: roleName}

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
			},
			Entry("admin-user-actions-core", adminCoreClusterRole),
			Entry("releaser-bot-actions", releaserBotClusterRole),
			Entry("self-access-reviewer", selfAccessClusterRole),
		)
	})
})
