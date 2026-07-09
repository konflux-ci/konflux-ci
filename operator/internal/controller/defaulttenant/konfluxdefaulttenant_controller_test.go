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
	"context"

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
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/internalregistry"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

const (
	integrationRunnerSA          = "konflux-integration-runner"
	authMaintainerRoleBinding    = "authenticated-konflux-maintainer"
	integrationRunnerRoleBinding = "konflux-integration-runner"
)

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
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: DefaultTenantNamespace}, ns)).To(Succeed())
			Expect(ns.Labels).To(HaveKeyWithValue("konflux-ci.dev/type", "tenant"))
		})

		It("should create the konflux-integration-runner ServiceAccount", func() {
			sa := &corev1.ServiceAccount{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "konflux-integration-runner",
				Namespace: DefaultTenantNamespace,
			}, sa)).To(Succeed())
		})

		It("should create the RoleBindings", func() {
			rb := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "konflux-integration-runner",
				Namespace: DefaultTenantNamespace,
			}, rb)).To(Succeed())
			Expect(rb.RoleRef.Name).To(Equal("konflux-integration-runner"))

			rbAuth := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "authenticated-konflux-maintainer",
				Namespace: DefaultTenantNamespace,
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
				Namespace: DefaultTenantNamespace,
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
					Namespace: DefaultTenantNamespace,
				}, got)).To(Succeed())
				g.Expect(got.Data).To(HaveKey(corev1.DockerConfigJsonKey))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Self-healing", func() {
		It("recreates ServiceAccount when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxDefaultTenant{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			saNN := types.NamespacedName{Name: integrationRunnerSA, Namespace: DefaultTenantNamespace}

			By("waiting for initial ServiceAccount creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, saNN, &corev1.ServiceAccount{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ServiceAccount")
			Expect(k8sClient.Delete(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: saNN.Name, Namespace: saNN.Namespace},
			})).To(Succeed())

			By("verifying the ServiceAccount is recreated with ownership labels")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		DescribeTable("recreates RoleBinding when deleted",
			func(ctx context.Context, rbName string) {
				cr := &konfluxv1alpha1.KonfluxDefaultTenant{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

				rbNN := types.NamespacedName{Name: rbName, Namespace: DefaultTenantNamespace}

				By("waiting for initial RoleBinding creation")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, rbNN, &rbacv1.RoleBinding{})).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("deleting the RoleBinding")
				Expect(k8sClient.Delete(ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: rbNN.Name, Namespace: rbNN.Namespace},
				})).To(Succeed())

				By("verifying the RoleBinding is recreated with ownership labels")
				Eventually(func(g Gomega) {
					rb := &rbacv1.RoleBinding{}
					g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
					g.Expect(rb.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			Entry("authenticated-konflux-maintainer", authMaintainerRoleBinding),
			Entry("konflux-integration-runner", integrationRunnerRoleBinding),
		)
	})

	Context("Drift correction", func() {
		It("restores Namespace labels when stripped", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxDefaultTenant{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			nsNN := types.NamespacedName{Name: DefaultTenantNamespace}

			By("waiting for initial Namespace creation with ownership labels")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Namespace")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				delete(ns.Labels, constant.KonfluxOwnerLabel)
				delete(ns.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, ns)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Namespace labels are restored")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ServiceAccount labels when stripped", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxDefaultTenant{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			saNN := types.NamespacedName{Name: integrationRunnerSA, Namespace: DefaultTenantNamespace}

			By("waiting for initial ServiceAccount creation with ownership labels")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the ServiceAccount")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				delete(sa.Labels, constant.KonfluxOwnerLabel)
				delete(sa.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, sa)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ServiceAccount labels are restored")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		DescribeTable("restores RoleBinding subjects when modified",
			func(ctx context.Context, rbName string) {
				cr := &konfluxv1alpha1.KonfluxDefaultTenant{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

				rbNN := types.NamespacedName{Name: rbName, Namespace: DefaultTenantNamespace}

				By("waiting for initial RoleBinding creation")
				var originalSubjects []rbacv1.Subject
				Eventually(func(g Gomega) {
					rb := &rbacv1.RoleBinding{}
					g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
					g.Expect(rb.Subjects).NotTo(BeEmpty())
					originalSubjects = rb.Subjects
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("modifying the RoleBinding subjects")
				Eventually(func(g Gomega) {
					rb := &rbacv1.RoleBinding{}
					g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
					rb.Subjects = []rbacv1.Subject{{
						Kind:     "User",
						Name:     "tampered-user",
						APIGroup: "rbac.authorization.k8s.io",
					}}
					g.Expect(k8sClient.Update(ctx, rb)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the RoleBinding subjects are restored")
				Eventually(func(g Gomega) {
					rb := &rbacv1.RoleBinding{}
					g.Expect(k8sClient.Get(ctx, rbNN, rb)).To(Succeed())
					g.Expect(rb.Subjects).To(Equal(originalSubjects))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			Entry("authenticated-konflux-maintainer", authMaintainerRoleBinding),
			Entry("konflux-integration-runner", integrationRunnerRoleBinding),
		)

		It("restores Secret data when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxDefaultTenant{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: internalregistry.CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, registry)

			err := k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: RegistrySourceSecretNamespace},
			})
			if !apierrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			sourceData := []byte(`{"auths":{"registry-service.kind-registry":{"auth":"original-creds"}}}`)
			source := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      RegistrySourceSecretName,
					Namespace: RegistrySourceSecretNamespace,
				},
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: sourceData,
				},
			}
			Expect(k8sClient.Create(ctx, source)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, source)

			targetNN := types.NamespacedName{Name: RegistryCredentialsSecretName, Namespace: DefaultTenantNamespace}

			By("waiting for target Secret creation")
			Eventually(func(g Gomega) {
				got := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, targetNN, got)).To(Succeed())
				g.Expect(got.Data).To(HaveKey(corev1.DockerConfigJsonKey))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the target Secret data")
			Eventually(func(g Gomega) {
				got := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, targetNN, got)).To(Succeed())
				got.Data[corev1.DockerConfigJsonKey] = []byte(`{"auths":{"tampered.example.com":{"auth":"dGFtcGVyZWQ="}}}`)

				g.Expect(k8sClient.Update(ctx, got)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Secret data is restored from source")
			Eventually(func(g Gomega) {
				got := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, targetNN, got)).To(Succeed())
				g.Expect(got.Data[corev1.DockerConfigJsonKey]).To(Equal(sourceData))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("propagates source Secret update to target (credential rotation)", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxDefaultTenant{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			registry := &konfluxv1alpha1.KonfluxInternalRegistry{
				ObjectMeta: metav1.ObjectMeta{Name: internalregistry.CRName},
			}
			Expect(k8sClient.Create(ctx, registry)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, registry)

			err := k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: RegistrySourceSecretNamespace},
			})
			if !apierrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			initialData := []byte(`{"auths":{"registry-service.kind-registry":{"auth":"aW5pdGlhbA=="}}}`)
			source := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      RegistrySourceSecretName,
					Namespace: RegistrySourceSecretNamespace,
				},
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: initialData,
				},
			}
			Expect(k8sClient.Create(ctx, source)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, source)

			targetNN := types.NamespacedName{Name: RegistryCredentialsSecretName, Namespace: DefaultTenantNamespace}

			By("waiting for target Secret to be created with initial data")
			Eventually(func(g Gomega) {
				got := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, targetNN, got)).To(Succeed())
				g.Expect(got.Data[corev1.DockerConfigJsonKey]).To(Equal(initialData))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("rotating the source Secret credentials")
			rotatedData := []byte(`{"auths":{"registry-service.kind-registry":{"auth":"cm90YXRlZA=="}}}`)
			Eventually(func(g Gomega) {
				src := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      RegistrySourceSecretName,
					Namespace: RegistrySourceSecretNamespace,
				}, src)).To(Succeed())
				src.Data[corev1.DockerConfigJsonKey] = rotatedData
				g.Expect(k8sClient.Update(ctx, src)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the target Secret is updated with rotated credentials")
			Eventually(func(g Gomega) {
				got := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, targetNN, got)).To(Succeed())
				g.Expect(got.Data[corev1.DockerConfigJsonKey]).To(Equal(rotatedData))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})
})
