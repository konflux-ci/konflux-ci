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

package cli

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

var _ = Describe("KonfluxCLI Controller", func() {
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxCLI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxCLI{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
			})

			// The CLI manifests contain no Deployments — only a Namespace, ConfigMaps, and
			// RBAC resources — so Ready=True is a reliable sentinel that the full
			// reconcile codepath ran successfully.
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.KonfluxCLI{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
				readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(readyCond.Message).To(ContainSubstring("Component ready"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the konflux-cli namespace was created")
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "konflux-cli"}, ns)).To(Succeed())

			By("verifying the owner label is set on the namespace")
			Expect(ns.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))

			By("verifying the create-tenant ConfigMap was created")
			createTenantCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "create-tenant",
				Namespace: "konflux-cli",
			}, createTenantCM)).To(Succeed())
			Expect(createTenantCM.Data).To(HaveKey("create-tenant.sh"))

			By("verifying the setup-release ConfigMap was created")
			setupReleaseCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "setup-release",
				Namespace: "konflux-cli",
			}, setupReleaseCM)).To(Succeed())
			Expect(setupReleaseCM.Data).To(HaveKey("setup-release.sh"))

			By("Verifying the setup-component ConfigMap was created")
			setupComponentCM := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "setup-component",
				Namespace: "konflux-cli",
			}, setupComponentCM)).To(Succeed())
			Expect(setupComponentCM.Data).To(HaveKey("setup-component.sh"))
		})
		It("should return no error when the CR does not exist", func(ctx context.Context) {
			// This is a unit-level guard: the controller must silently ignore
			// reconcile requests for resources that no longer exist (e.g. deleted
			// between the watch event and the actual reconcile). The manager flow
			// cannot exercise this path directly, so we call Reconcile once with a
			// name that was never created.
			reconciler := &KonfluxCLIReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "nonexistent"},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
