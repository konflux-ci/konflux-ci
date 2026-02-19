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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
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
		})
	})
})

var _ = Describe("applyECDefaultsOverrides", func() {
	var (
		cm *corev1.ConfigMap
		cr *konfluxv1alpha1.KonfluxEnterpriseContract
	)

	BeforeEach(func() {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ec-defaults",
				Namespace: "enterprise-contract-service",
			},
			Data: map[string]string{
				"verify_ec_task_bundle":         "quay.io/enterprise-contract/ec-task-bundle@sha256:original",
				"verify_ec_task_git_url":        "https://github.com/conforma/cli.git",
				"verify_ec_task_git_revision":   "abc123",
				"verify_ec_task_git_pathInRepo": "tasks/verify-conforma-konflux-ta/0.1/verify-conforma-konflux-ta.yaml",
			},
		}
		cr = &konfluxv1alpha1.KonfluxEnterpriseContract{
			ObjectMeta: metav1.ObjectMeta{
				Name:      CRName,
				Namespace: "default",
			},
		}
	})

	It("should not modify ConfigMap when ECDefaults is nil", func() {
		cr.Spec.ECDefaults = nil
		originalData := make(map[string]string)
		for k, v := range cm.Data {
			originalData[k] = v
		}

		applyECDefaultsOverrides(cm, cr)

		Expect(cm.Data).To(Equal(originalData))
	})

	It("should override only specified fields when ECDefaults has partial values", func() {
		cr.Spec.ECDefaults = &konfluxv1alpha1.ECDefaultsConfig{
			VerifyECTaskGitURL: "https://github.com/custom/repo.git",
		}

		applyECDefaultsOverrides(cm, cr)

		Expect(cm.Data["verify_ec_task_git_url"]).To(Equal("https://github.com/custom/repo.git"))
		// Other fields remain unchanged
		Expect(cm.Data["verify_ec_task_bundle"]).To(Equal("quay.io/enterprise-contract/ec-task-bundle@sha256:original"))
		Expect(cm.Data["verify_ec_task_git_revision"]).To(Equal("abc123"))
		Expect(cm.Data["verify_ec_task_git_pathInRepo"]).To(Equal("tasks/verify-conforma-konflux-ta/0.1/verify-conforma-konflux-ta.yaml"))
	})

	It("should override all fields when ECDefaults has all values", func() {
		cr.Spec.ECDefaults = &konfluxv1alpha1.ECDefaultsConfig{
			VerifyECTaskBundle:        "quay.io/custom/bundle@sha256:custom",
			VerifyECTaskGitURL:        "https://github.com/custom/repo.git",
			VerifyECTaskGitRevision:   "def456",
			VerifyECTaskGitPathInRepo: "custom/path/task.yaml",
		}

		applyECDefaultsOverrides(cm, cr)

		Expect(cm.Data["verify_ec_task_bundle"]).To(Equal("quay.io/custom/bundle@sha256:custom"))
		Expect(cm.Data["verify_ec_task_git_url"]).To(Equal("https://github.com/custom/repo.git"))
		Expect(cm.Data["verify_ec_task_git_revision"]).To(Equal("def456"))
		Expect(cm.Data["verify_ec_task_git_pathInRepo"]).To(Equal("custom/path/task.yaml"))
	})
})
