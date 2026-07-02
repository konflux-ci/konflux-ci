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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const (
	ecNamespace                = "enterprise-contract-service"
	ecConfigMapName            = "ec-defaults"
	configmapViewerClusterRole = "enterprisecontract-configmap-viewer-role"
	policyEditorClusterRole    = "enterprisecontractpolicy-editor-role"
	policyViewerClusterRole    = "enterprisecontractpolicy-viewer-role"
	publicEcCmRoleBinding      = "public-ec-cm"
	publicEcpRoleBinding       = "public-ecp"
	ecCRDName                  = "enterprisecontractpolicies.appstudio.redhat.com"
)

// newDefaultECPolicy returns an unstructured object for the default EnterpriseContractPolicy.
func newDefaultECPolicy() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(ecPolicyGVK)
	obj.SetName("default")
	obj.SetNamespace("enterprise-contract-service")
	return obj
}

// ecClusterScopedChildren returns all cluster-scoped resources that the reconciler creates.
// envtest has no garbage collector, so these must be explicitly cleaned up after each test.
func ecClusterScopedChildren() []client.Object {
	return []client.Object{
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: configmapViewerClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: policyEditorClusterRole}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: policyViewerClusterRole}},
		newDefaultECPolicy(),
	}
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

	Context("with skipPolicies unset (defaults to deploying policies)", func() {
		It("should successfully reconcile and create policies", func(ctx context.Context) {
			ec := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, ec)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ec, ecClusterScopedChildren()...)

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
			ec := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxEnterpriseContractSpec{
					SkipPolicies: true,
				},
			}
			Expect(k8sClient.Create(ctx, ec)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ec, ecClusterScopedChildren()...)

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
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, ecClusterScopedChildren()...)

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
			testutil.DeferCleanupParentAndChildren(k8sClient, cr, ecClusterScopedChildren()...)

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

	Context("Self-healing", func() {
		It("recreates ConfigMap when deleted", func(ctx context.Context) {
			ec := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, ec)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ec, ecClusterScopedChildren()...)

			cmNN := types.NamespacedName{Name: ecConfigMapName, Namespace: ecNamespace}

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
		})

		It("recreates ClusterRole when deleted", func(ctx context.Context) {
			ec := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, ec)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ec, ecClusterScopedChildren()...)

			crNN := types.NamespacedName{Name: configmapViewerClusterRole}

			By("waiting for initial ClusterRole creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crNN, &rbacv1.ClusterRole{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ClusterRole")
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: crNN.Name},
			})).To(Succeed())

			By("verifying the ClusterRole is recreated with ownership labels")
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				g.Expect(cr.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates RoleBinding when deleted", func(ctx context.Context) {
			ec := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, ec)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ec, ecClusterScopedChildren()...)

			rbNN := types.NamespacedName{Name: publicEcCmRoleBinding, Namespace: ecNamespace}

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

		It("recreates CRD when deleted", func(ctx context.Context) {
			ec := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, ec)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ec, ecClusterScopedChildren()...)

			crdNN := types.NamespacedName{Name: ecCRDName}

			By("waiting for CRD with owner labels")
			var originalUID types.UID
			Eventually(func(g Gomega) {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
				g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
				g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.EnterpriseContract)))
				originalUID = crd.UID
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the CRD and waiting for it to be gone")
			Expect(k8sClient.Delete(ctx, &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{Name: crdNN.Name},
			})).To(Succeed())
			Eventually(func(g Gomega) {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				err := k8sClient.Get(ctx, crdNN, crd)
				if err == nil {
					if crd.DeletionTimestamp != nil && len(crd.Finalizers) > 0 {
						crd.Finalizers = nil
						g.Expect(k8sClient.Update(ctx, crd)).To(Succeed())
					}
					g.Expect(crd.UID).NotTo(Equal(originalUID), "old CRD still exists")
					return
				}
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the CRD is recreated with correct spec and labels")
			Eventually(func(g Gomega) {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
				g.Expect(crd.UID).NotTo(Equal(originalUID))
				g.Expect(crd.Spec.Names.Kind).To(Equal("EnterpriseContractPolicy"))
				g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
				g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.EnterpriseContract)))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Drift correction", func() {
		It("restores Namespace labels when stripped", func(ctx context.Context) {
			ec := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, ec)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ec, ecClusterScopedChildren()...)

			nsNN := types.NamespacedName{Name: ecNamespace}

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

		It("restores ConfigMap data when modified", func(ctx context.Context) {
			ec := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, ec)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ec, ecClusterScopedChildren()...)

			cmNN := types.NamespacedName{Name: ecConfigMapName, Namespace: ecNamespace}

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
		})

		DescribeTable("restores ClusterRole rules when modified",
			func(ctx context.Context, roleName string) {
				ec := &konfluxv1alpha1.KonfluxEnterpriseContract{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, ec)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, ec, ecClusterScopedChildren()...)

				crNN := types.NamespacedName{Name: roleName}

				By("waiting for initial ClusterRole creation")
				var originalRules []rbacv1.PolicyRule
				Eventually(func(g Gomega) {
					cr := &rbacv1.ClusterRole{}
					g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
					g.Expect(cr.Rules).NotTo(BeEmpty())
					originalRules = cr.Rules
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("modifying the ClusterRole rules")
				Eventually(func(g Gomega) {
					cr := &rbacv1.ClusterRole{}
					g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
					cr.Rules = []rbacv1.PolicyRule{{
						APIGroups: []string{""},
						Resources: []string{"pods"},
						Verbs:     []string{"delete"},
					}}
					g.Expect(k8sClient.Update(ctx, cr)).To(Succeed())
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

				By("verifying the ClusterRole rules are restored")
				Eventually(func(g Gomega) {
					cr := &rbacv1.ClusterRole{}
					g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
					g.Expect(cr.Rules).To(Equal(originalRules))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			Entry("configmap-viewer", configmapViewerClusterRole),
			Entry("policy-editor", policyEditorClusterRole),
			Entry("policy-viewer", policyViewerClusterRole),
		)

		DescribeTable("restores RoleBinding subjects when modified",
			func(ctx context.Context, bindingName string) {
				ec := &konfluxv1alpha1.KonfluxEnterpriseContract{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, ec)).To(Succeed())
				testutil.DeferCleanupParentAndChildren(k8sClient, ec, ecClusterScopedChildren()...)

				rbNN := types.NamespacedName{Name: bindingName, Namespace: ecNamespace}

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
			Entry("public-ec-cm", publicEcCmRoleBinding),
			Entry("public-ecp", publicEcpRoleBinding),
		)

		It("restores CRD spec when version is disabled", func(ctx context.Context) {
			ec := &konfluxv1alpha1.KonfluxEnterpriseContract{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, ec)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ec, ecClusterScopedChildren()...)

			crdNN := types.NamespacedName{Name: ecCRDName}

			By("waiting for CRD creation with served=true")
			Eventually(func(g Gomega) {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
				g.Expect(crd.Spec.Versions).NotTo(BeEmpty())
				g.Expect(crd.Spec.Versions[0].Served).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("disabling the served version")
			var afterTamperRV string
			Eventually(func(g Gomega) {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
				crd.Spec.Versions[0].Served = false
				g.Expect(k8sClient.Update(ctx, crd)).To(Succeed())
				afterTamperRV = crd.ResourceVersion
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying SSA restores served=true")
			Eventually(func(g Gomega) {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
				g.Expect(crd.ResourceVersion).NotTo(Equal(afterTamperRV), "controller has not reconciled yet")
				g.Expect(crd.Spec.Versions[0].Served).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})
})
