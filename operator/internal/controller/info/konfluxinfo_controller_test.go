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

package info

import (
	"context"
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

func newReconciler() *KonfluxInfoReconciler {
	return &KonfluxInfoReconciler{
		Client:      k8sClient,
		Scheme:      k8sClient.Scheme(),
		ObjectStore: objectStore,
	}
}

func reconcileSuccessfully(ctx context.Context, r *KonfluxInfoReconciler) {
	_, err := r.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: CRName},
	})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
}

func getConfigMap(ctx context.Context, name string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{}
	ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: infoNamespace,
	}, cm)).To(Succeed())
	return cm
}

var _ = Describe("KonfluxInfo Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: CRName,
		}
		konfluxinfo := &konfluxv1alpha1.KonfluxInfo{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KonfluxInfo")
			err := k8sClient.Get(ctx, typeNamespacedName, konfluxinfo)
			if err != nil && apierrors.IsNotFound(err) {
				resource := &konfluxv1alpha1.KonfluxInfo{
					ObjectMeta: metav1.ObjectMeta{
						Name: CRName,
					},
					Spec: konfluxv1alpha1.KonfluxInfoSpec{},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &konfluxv1alpha1.KonfluxInfo{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				By("Cleanup the specific resource instance KonfluxInfo")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			r := newReconciler()
			reconcileSuccessfully(ctx, r)
		})
	})

	Context("ConfigMap drift detection", func() {
		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{Name: CRName}

		BeforeEach(func() {
			By("creating the KonfluxInfo CR")
			err := k8sClient.Get(ctx, typeNamespacedName, &konfluxv1alpha1.KonfluxInfo{})
			if err != nil && apierrors.IsNotFound(err) {
				resource := &konfluxv1alpha1.KonfluxInfo{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
					Spec: konfluxv1alpha1.KonfluxInfoSpec{
						PublicInfo: &konfluxv1alpha1.PublicInfo{
							Environment: "staging",
							Visibility:  "public",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}

			By("running initial reconciliation to create ConfigMaps")
			reconcileSuccessfully(ctx, newReconciler())
		})

		AfterEach(func() {
			resource := &konfluxv1alpha1.KonfluxInfo{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should create all expected ConfigMaps", func() {
			for _, name := range []string{infoConfigMapName, bannerConfigMapName, clusterConfigMapName} {
				cm := getConfigMap(ctx, name)
				Expect(cm.Name).To(Equal(name))
			}
		})

		It("should revert out-of-band data changes to the info ConfigMap", func() {
			cm := getConfigMap(ctx, infoConfigMapName)
			originalData := cm.Data["info.json"]
			Expect(originalData).NotTo(BeEmpty())

			var parsed map[string]interface{}
			Expect(json.Unmarshal([]byte(originalData), &parsed)).To(Succeed())
			Expect(parsed["environment"]).To(Equal("staging"))

			By("modifying the ConfigMap data out of band")
			cm.Data["info.json"] = `{"environment":"hacked"}`
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			fetched := getConfigMap(ctx, infoConfigMapName)
			Expect(fetched.Data["info.json"]).To(Equal(`{"environment":"hacked"}`))

			By("reconciling again to revert the change")
			reconcileSuccessfully(ctx, newReconciler())

			reverted := getConfigMap(ctx, infoConfigMapName)
			Expect(reverted.Data["info.json"]).To(Equal(originalData))
		})

		It("should revert out-of-band data changes to the banner ConfigMap", func() {
			cm := getConfigMap(ctx, bannerConfigMapName)
			originalData := cm.Data["banner-content.yaml"]

			By("modifying the ConfigMap data out of band")
			cm.Data["banner-content.yaml"] = "tampered"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			By("reconciling again to revert the change")
			reconcileSuccessfully(ctx, newReconciler())

			reverted := getConfigMap(ctx, bannerConfigMapName)
			Expect(reverted.Data["banner-content.yaml"]).To(Equal(originalData))
		})

		It("should revert out-of-band data changes to the cluster-config ConfigMap", func() {
			By("updating the CR with cluster config data")
			cr := &konfluxv1alpha1.KonfluxInfo{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, cr)).To(Succeed())
			cr.Spec.ClusterConfig = &konfluxv1alpha1.ClusterConfig{
				Data: &konfluxv1alpha1.ClusterConfigData{
					DefaultOIDCIssuer: "https://oidc.example.com",
				},
			}
			Expect(k8sClient.Update(ctx, cr)).To(Succeed())

			By("reconciling to apply the cluster config")
			reconcileSuccessfully(ctx, newReconciler())

			cm := getConfigMap(ctx, clusterConfigMapName)
			Expect(cm.Data).To(HaveKeyWithValue("defaultOIDCIssuer", "https://oidc.example.com"))

			By("modifying the ConfigMap data out of band")
			cm.Data["defaultOIDCIssuer"] = "https://evil.example.com"
			Expect(k8sClient.Update(ctx, cm)).To(Succeed())

			fetched := getConfigMap(ctx, clusterConfigMapName)
			Expect(fetched.Data["defaultOIDCIssuer"]).To(Equal("https://evil.example.com"))

			By("reconciling again to revert the change")
			reconcileSuccessfully(ctx, newReconciler())

			reverted := getConfigMap(ctx, clusterConfigMapName)
			Expect(reverted.Data).To(HaveKeyWithValue("defaultOIDCIssuer", "https://oidc.example.com"))
		})

		It("should recreate a deleted info ConfigMap", func() {
			cm := getConfigMap(ctx, infoConfigMapName)

			By("deleting the ConfigMap out of band")
			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{})
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())

			By("reconciling again to recreate it")
			reconcileSuccessfully(ctx, newReconciler())

			recreated := getConfigMap(ctx, infoConfigMapName)
			Expect(recreated.Data).To(HaveKey("info.json"))
		})

		It("should recreate a deleted banner ConfigMap", func() {
			cm := getConfigMap(ctx, bannerConfigMapName)

			By("deleting the ConfigMap out of band")
			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{})
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())

			By("reconciling again to recreate it")
			reconcileSuccessfully(ctx, newReconciler())

			recreated := getConfigMap(ctx, bannerConfigMapName)
			Expect(recreated.Data).To(HaveKey("banner-content.yaml"))
		})

		It("should recreate a deleted cluster-config ConfigMap", func() {
			cm := getConfigMap(ctx, clusterConfigMapName)

			By("deleting the ConfigMap out of band")
			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{})
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())

			By("reconciling again to recreate it")
			reconcileSuccessfully(ctx, newReconciler())

			recreated := getConfigMap(ctx, clusterConfigMapName)
			Expect(recreated.Name).To(Equal(clusterConfigMapName))
		})
	})
})
