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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

const enterpriseContractNamespace = "enterprise-contract-service"

var _ = Describe("KonfluxEnterpriseContract Controller", func() {
	Context("When reconciling a resource", Ordered, func() {
		BeforeAll(func() {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())

			// Wait once for the controller to finish reconciling.
			// The reconciler may need multiple attempts while the EnterpriseContractPolicy CRD establishes.
			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.KonfluxEnterpriseContract{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
				readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})

		AfterAll(func() {
			testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxEnterpriseContract{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
		})

		It("should create the enterprise-contract-service namespace", func() {
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: enterpriseContractNamespace}, ns)).To(Succeed())
		})

		It("should create the EnterpriseContractPolicy CRD", func() {
			crd := &apiextensionsv1.CustomResourceDefinition{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "enterprisecontractpolicies.appstudio.redhat.com"}, crd)).To(Succeed())
		})

		It("should create ClusterRoles for EnterpriseContractPolicy", func() {
			editorRole := &rbacv1.ClusterRole{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "enterprisecontractpolicy-editor-role"}, editorRole)).To(Succeed())

			viewerRole := &rbacv1.ClusterRole{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "enterprisecontractpolicy-viewer-role"}, viewerRole)).To(Succeed())
		})

		It("should create RoleBindings granting public access", func() {
			cmBinding := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "public-ec-cm", Namespace: enterpriseContractNamespace}, cmBinding)).To(Succeed())

			ecpBinding := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "public-ecp", Namespace: enterpriseContractNamespace}, ecpBinding)).To(Succeed())
		})

		It("should create the ec-defaults ConfigMap", func() {
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "ec-defaults", Namespace: enterpriseContractNamespace}, cm)).To(Succeed())
		})

	})
})
