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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

var _ = Describe("KonfluxRBAC Controller", func() {
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxRBAC{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxRBAC{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
			})

			// The rbac manifests contain only ClusterRoles — no Deployments — so
			// Ready=True is a reliable sentinel.
			Eventually(func(g Gomega) {
				cr := &konfluxv1alpha1.KonfluxRBAC{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, cr)).To(Succeed())
				g.Expect(condition.IsConditionTrue(cr, condition.TypeReady)).To(BeTrue())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})
	})
})
