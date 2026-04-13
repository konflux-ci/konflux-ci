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

package integrationservice

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

const integrationServiceNamespace = "integration-service"

var _ = Describe("KonfluxIntegrationService Controller", func() {
	Context("When reconciling a resource", Ordered, func() {
		BeforeAll(func() {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxIntegrationService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())

			// Wait for the Deployment rather than Ready=True: UpdateComponentStatuses
			// gates Ready=True on ReadyReplicas == Replicas, which never happens in
			// envtest (no kubelet → pods never start). Deployment existence is
			// sufficient proof that the full manifest-apply codepath completed.
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "integration-service-controller-manager",
					Namespace: integrationServiceNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})

		AfterAll(func() {
			testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxIntegrationService{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
		})

		It("should create the integration-service namespace", func() {
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: integrationServiceNamespace}, ns)).To(Succeed())
		})

		It("should create the CRDs", func() {
			for _, name := range []string{
				"componentgroups.appstudio.redhat.com",
				"integrationtestscenarios.appstudio.redhat.com",
			} {
				crd := &apiextensionsv1.CustomResourceDefinition{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, crd)).To(Succeed())
			}
		})

		It("should create the leader-election Role", func() {
			role := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "integration-service-leader-election-role",
				Namespace: integrationServiceNamespace,
			}, role)).To(Succeed())
		})

		It("should create the ClusterRoles", func() {
			for _, name := range []string{
				"integration-service-componentgroup-admin-role",
				"integration-service-componentgroup-editor-role",
				"integration-service-componentgroup-viewer-role",
				"integration-service-integrationtestscenario-admin-role",
				"integration-service-integrationtestscenario-editor-role",
				"integration-service-integrationtestscenario-viewer-role",
				"integration-service-manager-role",
				"integration-service-metrics-auth-role",
				"integration-service-snapshot-garbage-collector",
				"integration-service-tekton-editor-role",
				"konflux-integration-runner",
			} {
				cr := &rbacv1.ClusterRole{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, cr)).To(Succeed())
			}
		})

		It("should create the leader-election RoleBinding", func() {
			rb := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "integration-service-leader-election-rolebinding",
				Namespace: integrationServiceNamespace,
			}, rb)).To(Succeed())
		})

		It("should create the ClusterRoleBindings", func() {
			for _, name := range []string{
				"integration-service-manager-rolebinding",
				"integration-service-metrics-auth-rolebinding",
				"integration-service-snapshot-garbage-collector",
				"integration-service-tekton-role-binding",
				"kyverno-background-controller-konflux-integration-runner",
			} {
				crb := &rbacv1.ClusterRoleBinding{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, crb)).To(Succeed())
			}
		})

		It("should create the manager ConfigMap", func() {
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "integration-service-manager-config",
				Namespace: integrationServiceNamespace,
			}, cm)).To(Succeed())
		})

		It("should create the Services", func() {
			for _, name := range []string{
				"integration-service-controller-manager-metrics-service",
				"integration-service-webhook-service",
			} {
				svc := &corev1.Service{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: integrationServiceNamespace}, svc)).To(Succeed())
			}
		})

		It("should create the controller manager Deployment", func() {
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "integration-service-controller-manager",
				Namespace: integrationServiceNamespace,
			}, dep)).To(Succeed())
		})

	})
})
