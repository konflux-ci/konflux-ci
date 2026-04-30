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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

const (
	ecPolicyNamespace = "enterprise-contract-service"
	ecPolicyName      = "default"
)

// ecPolicyGVK is defined in the controller file.

func newECPolicyObject() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(ecPolicyGVK)
	obj.SetName(ecPolicyName)
	obj.SetNamespace(ecPolicyNamespace)
	return obj
}

func newECPolicyList() *unstructured.UnstructuredList {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(ecPolicyGVK)
	return list
}

func reconcileEC(ctx context.Context) {
	reconciler := &KonfluxEnterpriseContractReconciler{
		Client:      k8sClient,
		Scheme:      k8sClient.Scheme(),
		ObjectStore: objectStore,
	}
	Eventually(func() error {
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: CRName},
		})
		return err
	}).WithTimeout(15 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
}

var _ = Describe("KonfluxEnterpriseContract Controller", func() {
	Context("When reconciling a resource", func() {
		var cr *konfluxv1alpha1.KonfluxEnterpriseContract

		BeforeEach(func() {
			By("creating the KonfluxEnterpriseContract CR")
			cr = &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			By("running initial reconciliation")
			reconcileEC(ctx)
		})

		AfterEach(func() {
			By("cleaning up the KonfluxEnterpriseContract CR")
			Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		})

		It("should create EnterpriseContractPolicy resources", func() {
			policyList := newECPolicyList()
			Expect(k8sClient.List(ctx, policyList, client.InNamespace(ecPolicyNamespace))).To(Succeed())
			Expect(policyList.Items).NotTo(BeEmpty())
		})

		It("should restore an EnterpriseContractPolicy after it is modified", func() {
			By("fetching the existing EnterpriseContractPolicy")
			policy := newECPolicyObject()
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      ecPolicyName,
				Namespace: ecPolicyNamespace,
			}, policy)).To(Succeed())

			spec, _, _ := unstructured.NestedMap(policy.Object, "spec")
			originalDescription, _, _ := unstructured.NestedString(spec, "description")

			By("modifying the EnterpriseContractPolicy spec")
			Expect(unstructured.SetNestedField(policy.Object, "modified-by-test", "spec", "description")).To(Succeed())
			Expect(k8sClient.Update(ctx, policy)).To(Succeed())

			By("reconciling and verifying the controller restores the original spec")
			reconcileEC(ctx)

			updated := newECPolicyObject()
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      ecPolicyName,
				Namespace: ecPolicyNamespace,
			}, updated)).To(Succeed())
			desc, _, _ := unstructured.NestedString(updated.Object, "spec", "description")
			Expect(desc).To(Equal(originalDescription))
		})

		It("should re-create an EnterpriseContractPolicy after it is deleted", func() {
			By("deleting the EnterpriseContractPolicy")
			policy := newECPolicyObject()
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      ecPolicyName,
				Namespace: ecPolicyNamespace,
			}, policy)).To(Succeed())
			Expect(k8sClient.Delete(ctx, policy)).To(Succeed())

			By("reconciling and verifying the controller re-creates it")
			reconcileEC(ctx)

			recreated := newECPolicyObject()
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      ecPolicyName,
				Namespace: ecPolicyNamespace,
			}, recreated)).To(Succeed())
		})
	})
})
