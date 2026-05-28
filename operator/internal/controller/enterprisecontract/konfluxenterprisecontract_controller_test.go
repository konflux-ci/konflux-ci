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

package enterprisecontract

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

// newDefaultECPolicy returns an unstructured object for the default EnterpriseContractPolicy.
func newDefaultECPolicy() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(ecPolicyGVK)
	obj.SetName("default")
	obj.SetNamespace("enterprise-contract-service")
	return obj
}

var _ = Describe("KonfluxEnterpriseContract Controller", Ordered, func() {
	// waitForReady returns an Eventually-compatible poll function that uses the per-test ctx.
	waitForReady := func(ctx context.Context) func(Gomega) {
		return func(g Gomega) {
			updated := &konfluxv1alpha1.KonfluxEnterpriseContract{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
			readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
			g.Expect(readyCond).NotTo(BeNil())
			g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
		}
	}

	AfterEach(func(ctx context.Context) {
		testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxEnterpriseContract{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
		testutil.DeleteAndWait(ctx, k8sClient, newDefaultECPolicy())
	})

	Context("with skipPolicies unset (defaults to deploying policies)", func() {
		It("should successfully reconcile and create policies", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())

			Eventually(waitForReady(ctx)).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the enterprise-contract namespace was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "enterprise-contract-service"}, &corev1.Namespace{})).To(Succeed())

			By("verifying a representative ClusterRole was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "enterprisecontractpolicy-viewer-role"}, &rbacv1.ClusterRole{})).To(Succeed())

			By("verifying the configmap-viewer ClusterRole was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "enterprisecontract-configmap-viewer-role"}, &rbacv1.ClusterRole{})).To(Succeed())

			By("verifying the ec-defaults ConfigMap was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "ec-defaults", Namespace: "enterprise-contract-service"}, &corev1.ConfigMap{})).To(Succeed())

			By("verifying the default EnterpriseContractPolicy was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default", Namespace: "enterprise-contract-service"}, newDefaultECPolicy())).To(Succeed())
		})
	})

	Context("with skipPolicies set to true", func() {
		It("should reconcile without creating EnterpriseContractPolicy resources", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxEnterpriseContractSpec{
					SkipPolicies: true,
				},
			})).To(Succeed())

			Eventually(waitForReady(ctx)).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the enterprise-contract namespace was still created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "enterprise-contract-service"}, &corev1.Namespace{})).To(Succeed())

			By("verifying RBAC resources were still created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "enterprisecontractpolicy-viewer-role"}, &rbacv1.ClusterRole{})).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "enterprisecontract-configmap-viewer-role"}, &rbacv1.ClusterRole{})).To(Succeed())

			By("verifying the ec-defaults ConfigMap was still created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "ec-defaults", Namespace: "enterprise-contract-service"}, &corev1.ConfigMap{})).To(Succeed())

			By("verifying the EnterpriseContractPolicy was NOT created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default", Namespace: "enterprise-contract-service"}, newDefaultECPolicy())).
				To(MatchError(ContainSubstring("not found")))
		})
	})

	Context("transitioning skipPolicies from false to true", func() {
		It("should delete previously deployed EnterpriseContractPolicy resources", func(ctx context.Context) {
			By("creating CR with skipPolicies=false (default) so policies are deployed")
			cr := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			Eventually(waitForReady(ctx)).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the EnterpriseContractPolicy was created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default", Namespace: "enterprise-contract-service"}, newDefaultECPolicy())).To(Succeed())

			By("updating CR to set skipPolicies=true")
			updated := &konfluxv1alpha1.KonfluxEnterpriseContract{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
			updated.Spec.SkipPolicies = true
			Expect(k8sClient.Update(ctx, updated)).To(Succeed())

			By("verifying the EnterpriseContractPolicy is removed via orphan cleanup")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default", Namespace: "enterprise-contract-service"}, newDefaultECPolicy())).
					To(MatchError(ContainSubstring("not found")))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying non-policy resources are still present")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "enterprise-contract-service"}, &corev1.Namespace{})).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "ec-defaults", Namespace: "enterprise-contract-service"}, &corev1.ConfigMap{})).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "enterprisecontractpolicy-viewer-role"}, &rbacv1.ClusterRole{})).To(Succeed())
		})
	})

	Context("transitioning skipPolicies from true to false", func() {
		It("should re-create EnterpriseContractPolicy resources", func(ctx context.Context) {
			By("creating CR with skipPolicies=true so policies are not deployed")
			cr := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxEnterpriseContractSpec{
					SkipPolicies: true,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			Eventually(waitForReady(ctx)).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the EnterpriseContractPolicy was NOT created")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default", Namespace: "enterprise-contract-service"}, newDefaultECPolicy())).
				To(MatchError(ContainSubstring("not found")))

			By("updating CR to set skipPolicies=false")
			updated := &konfluxv1alpha1.KonfluxEnterpriseContract{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
			updated.Spec.SkipPolicies = false
			Expect(k8sClient.Update(ctx, updated)).To(Succeed())

			By("verifying the EnterpriseContractPolicy is now created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default", Namespace: "enterprise-contract-service"}, newDefaultECPolicy())).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})
})
