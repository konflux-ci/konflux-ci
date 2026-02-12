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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

var _ = Describe("KonfluxEnterpriseContract Controller", func() {
	Context("When reconciling a resource", func() {

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      CRName,
			Namespace: "default",
		}
		konfluxenterprisecontract := &konfluxv1alpha1.KonfluxEnterpriseContract{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KonfluxEnterpriseContract")
			err := k8sClient.Get(ctx, typeNamespacedName, konfluxenterprisecontract)
			if err != nil && errors.IsNotFound(err) {
				resource := &konfluxv1alpha1.KonfluxEnterpriseContract{
					ObjectMeta: metav1.ObjectMeta{
						Name:      CRName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &konfluxv1alpha1.KonfluxEnterpriseContract{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KonfluxEnterpriseContract")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KonfluxEnterpriseContractReconciler{
				Client:      k8sClient,
				Scheme:      k8sClient.Scheme(),
				ObjectStore: objectStore,
			}

			By("Waiting for reconciliation to succeed (CRD may need to establish first)")
			Eventually(func() error {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				return err
			}).WithTimeout(15 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})
