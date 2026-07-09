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

package applicationapi

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

type crdInfo struct {
	name string
	kind string
}

var managedCRDs = []crdInfo{
	{"applications.appstudio.redhat.com", "Application"},
	{"componentdetectionqueries.appstudio.redhat.com", "ComponentDetectionQuery"},
	{"components.appstudio.redhat.com", "Component"},
	{"deploymenttargetclaims.appstudio.redhat.com", "DeploymentTargetClaim"},
	{"deploymenttargetclasses.appstudio.redhat.com", "DeploymentTargetClass"},
	{"deploymenttargets.appstudio.redhat.com", "DeploymentTarget"},
	{"environments.appstudio.redhat.com", "Environment"},
	{"promotionruns.appstudio.redhat.com", "PromotionRun"},
	{"snapshotenvironmentbindings.appstudio.redhat.com", "SnapshotEnvironmentBinding"},
	{"snapshots.appstudio.redhat.com", "Snapshot"},
}

func crdEntries() []TableEntry {
	entries := make([]TableEntry, len(managedCRDs))
	for i, c := range managedCRDs {
		entries[i] = Entry(c.kind, c.name, c.kind)
	}
	return entries
}

func crdEntriesNameOnly() []TableEntry {
	entries := make([]TableEntry, len(managedCRDs))
	for i, c := range managedCRDs {
		entries[i] = Entry(c.kind, c.name)
	}
	return entries
}

var _ = Describe("KonfluxApplicationAPI Controller", func() {
	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			appRes := &konfluxv1alpha1.KonfluxApplicationAPI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			}
			Expect(k8sClient.Create(ctx, appRes)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, appRes)

			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.KonfluxApplicationAPI{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updated)).To(Succeed())
				readyCond := meta.FindStatusCondition(updated.Status.Conditions, condition.TypeReady)
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Self-healing", func() {
		DescribeTable("recreates CRD when deleted",
			func(ctx context.Context, crdName, expectedKind string) {
				cr := &konfluxv1alpha1.KonfluxApplicationAPI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

				crdNN := types.NamespacedName{Name: crdName}

				By("waiting for CRD with owner labels")
				var originalUID types.UID
				Eventually(func(g Gomega) {
					crd := &apiextensionsv1.CustomResourceDefinition{}
					g.Expect(k8sClient.Get(ctx, crdNN, crd)).To(Succeed())
					g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
					g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.ApplicationAPI)))
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
					g.Expect(crd.Spec.Names.Kind).To(Equal(expectedKind))
					g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
					g.Expect(crd.Labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.ApplicationAPI)))
				}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
			},
			crdEntries(),
		)
	})

	Context("Drift correction", func() {
		DescribeTable("restores CRD spec when version is disabled",
			func(ctx context.Context, crdName string) {
				cr := &konfluxv1alpha1.KonfluxApplicationAPI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				}
				Expect(k8sClient.Create(ctx, cr)).To(Succeed())
				DeferCleanup(testutil.DeleteAndWait, k8sClient, cr)

				crdNN := types.NamespacedName{Name: crdName}

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
			},
			crdEntriesNameOnly(),
		)
	})
})
