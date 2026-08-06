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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

const (
	cliNamespace             = "konflux-cli"
	cliRoleName              = "konflux-cli-configmaps-read"
	cliRoleBindingName       = "konflux-cli-configmaps-read-binding"
	cliConfigMapName         = "create-tenant"
	cliSetupReleaseConfigMap = "setup-release"
)

var _ = Describe("KonfluxCLI Controller", func() {
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			cliRes := &konfluxv1alpha1.KonfluxCLI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cliRes)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cliRes)

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

	Context("Self-healing", func() {
		DescribeTable("recreates ConfigMap when deleted",
			func(ctx context.Context, cmName string) {
				cr := &konfluxv1alpha1.KonfluxCLI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

				cmNN := types.NamespacedName{Name: cmName, Namespace: cliNamespace}

				By("waiting for initial ConfigMap creation")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, cmNN, &corev1.ConfigMap{})).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("deleting the ConfigMap")
				Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: cmNN.Name, Namespace: cmNN.Namespace},
				})).To(Succeed())

				By("verifying the ConfigMap is recreated with ownership labels")
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
					g.Expect(cm.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			Entry("create-tenant", cliConfigMapName),
			Entry("setup-release", cliSetupReleaseConfigMap),
		)

		It("recreates Role when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxCLI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			roleNN := types.NamespacedName{Name: cliRoleName, Namespace: cliNamespace}

			By("waiting for initial Role creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, roleNN, &rbacv1.Role{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Role")
			Expect(k8sClient.Delete(ctx, &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: roleNN.Name, Namespace: roleNN.Namespace},
			})).To(Succeed())

			By("verifying the Role is recreated with ownership labels")
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				g.Expect(role.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates RoleBinding when deleted", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxCLI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			rbNN := types.NamespacedName{Name: cliRoleBindingName, Namespace: cliNamespace}

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
		})
	})

	Context("Drift correction", func() {
		It("restores Namespace labels when stripped", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxCLI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			nsNN := types.NamespacedName{Name: cliNamespace}

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

		DescribeTable("restores ConfigMap data when modified",
			func(ctx context.Context, cmName string) {
				cr := &konfluxv1alpha1.KonfluxCLI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

				cmNN := types.NamespacedName{Name: cmName, Namespace: cliNamespace}

				By("waiting for initial ConfigMap creation")
				var originalKey string
				var originalValue string
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
					g.Expect(cm.Data).NotTo(BeEmpty())
					for k, v := range cm.Data {
						originalKey = k
						originalValue = v
						break
					}
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("modifying an existing ConfigMap data key")
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
					cm.Data[originalKey] = "tampered-content"
					g.Expect(k8sClient.Update(ctx, cm)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the ConfigMap data is restored")
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(k8sClient.Get(ctx, cmNN, cm)).To(Succeed())
					g.Expect(cm.Data).To(HaveKeyWithValue(originalKey, originalValue))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			Entry("create-tenant", cliConfigMapName),
			Entry("setup-release", cliSetupReleaseConfigMap),
		)

		It("restores Role rules when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxCLI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			roleNN := types.NamespacedName{Name: cliRoleName, Namespace: cliNamespace}

			By("waiting for initial Role creation")
			var originalRules []rbacv1.PolicyRule
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				g.Expect(role.Rules).NotTo(BeEmpty())
				originalRules = role.Rules
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Role rules")
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				role.Rules = []rbacv1.PolicyRule{{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"delete"},
				}}
				g.Expect(k8sClient.Update(ctx, role)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Role rules are restored")
			Eventually(func(g Gomega) {
				role := &rbacv1.Role{}
				g.Expect(k8sClient.Get(ctx, roleNN, role)).To(Succeed())
				g.Expect(role.Rules).To(Equal(originalRules))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores RoleBinding subjects when modified", func(ctx context.Context) {
			cr := &konfluxv1alpha1.KonfluxCLI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

			rbNN := types.NamespacedName{Name: cliRoleBindingName, Namespace: cliNamespace}

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
		})
	})
})
