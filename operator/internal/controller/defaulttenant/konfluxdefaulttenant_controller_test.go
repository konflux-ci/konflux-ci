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

package defaulttenant

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/internalregistry"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

const defaultTenantNamespace = "default-tenant"

var _ = Describe("KonfluxDefaultTenant Controller", func() {
	Context("When reconciling a resource", Ordered, func() {
		BeforeAll(func() {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxDefaultTenant{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())

			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.KonfluxDefaultTenant{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
				readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		AfterAll(func() {
			testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxDefaultTenant{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
		})

		It("should successfully reconcile the resource", func() {
			updated := &konfluxv1alpha1.KonfluxDefaultTenant{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
			readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should create the default-tenant namespace", func() {
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: defaultTenantNamespace}, ns)).To(Succeed())
			Expect(ns.Labels).To(HaveKeyWithValue("konflux-ci.dev/type", "tenant"))
		})

		It("should create the konflux-integration-runner ServiceAccount", func() {
			sa := &corev1.ServiceAccount{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "konflux-integration-runner",
				Namespace: defaultTenantNamespace,
			}, sa)).To(Succeed())
		})

		It("should create the RoleBindings", func() {
			rb := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "konflux-integration-runner",
				Namespace: defaultTenantNamespace,
			}, rb)).To(Succeed())
			Expect(rb.RoleRef.Name).To(Equal("konflux-integration-runner"))

			rbAuth := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "authenticated-konflux-maintainer",
				Namespace: defaultTenantNamespace,
			}, rbAuth)).To(Succeed())
			Expect(rbAuth.RoleRef.Name).To(Equal("konflux-maintainer-user-actions"))
			Expect(rbAuth.Subjects).To(HaveLen(1))
			Expect(rbAuth.Subjects[0].Kind).To(Equal("Group"))
			Expect(rbAuth.Subjects[0].Name).To(Equal("system:authenticated"))
		})

		It("should reconcile when registry source secret appears after default-tenant", func() {
			// Ensure target secret is absent before the source secret event.
			target := &corev1.Secret{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      RegistryCredentialsSecretName,
				Namespace: defaultTenantNamespace,
			}, target)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: internalregistry.CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, registry)

			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: RegistrySourceSecretNamespace},
			})).To(Succeed())

			source := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      RegistrySourceSecretName,
					Namespace: RegistrySourceSecretNamespace,
				},
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: []byte(`{"auths":{"registry-service.kind-registry":{"auth":"abc"}}}`),
				},
			}
			Expect(k8sClient.Create(ctx, source)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, source)

			// Source secret watch should enqueue KonfluxDefaultTenant reconcile.
			Eventually(func(g Gomega) {
				got := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      RegistryCredentialsSecretName,
					Namespace: defaultTenantNamespace,
				}, got)).To(Succeed())
				g.Expect(got.Data).To(HaveKey(corev1.DockerConfigJsonKey))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})
})
