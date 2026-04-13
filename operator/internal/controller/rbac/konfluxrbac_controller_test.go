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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

var _ = Describe("KonfluxRBAC Controller", func() {
	Context("When reconciling a resource", Ordered, func() {
		BeforeAll(func() {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxRBAC{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())

			// The rbac manifests contain only ClusterRoles — no Deployments — so
			// Ready=True is a reliable barrier: it fires as soon as all owned objects
			// are applied without waiting for pod readiness.
			cr := &konfluxv1alpha1.KonfluxRBAC{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, cr)).To(Succeed())
				g.Expect(condition.IsConditionTrue(cr, condition.TypeReady)).To(BeTrue())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})

		AfterAll(func() {
			testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxRBAC{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
		})

		clusterRole := func(name string) func() {
			return func() {
				cr := &rbacv1.ClusterRole{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, cr)).To(Succeed())
			}
		}

		It("should create the konflux-admin-user-actions-batch ClusterRole", clusterRole("konflux-admin-user-actions-batch"))
		It("should create the konflux-admin-user-actions-core ClusterRole", clusterRole("konflux-admin-user-actions-core"))
		It("should create the konflux-admin-user-actions-extra ClusterRole", clusterRole("konflux-admin-user-actions-extra"))
		It("should create the konflux-contributor-user-actions-core ClusterRole", clusterRole("konflux-contributor-user-actions-core"))
		It("should create the konflux-contributor-user-actions-extra ClusterRole", clusterRole("konflux-contributor-user-actions-extra"))
		It("should create the konflux-maintainer-user-actions-core ClusterRole", clusterRole("konflux-maintainer-user-actions-core"))
		It("should create the konflux-maintainer-user-actions-extra ClusterRole", clusterRole("konflux-maintainer-user-actions-extra"))
		It("should create the konflux-self-access-reviewer ClusterRole", clusterRole("konflux-self-access-reviewer"))
		It("should create the konflux-viewer-user-actions-core ClusterRole", clusterRole("konflux-viewer-user-actions-core"))
		It("should create the konflux-viewer-user-actions-extra ClusterRole", clusterRole("konflux-viewer-user-actions-extra"))
	})
})
