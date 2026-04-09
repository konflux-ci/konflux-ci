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

package releaseservice

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

const releaseServiceNamespace = "release-service"

var _ = Describe("KonfluxReleaseService Controller", func() {
	Context("When reconciling a resource", Ordered, func() {
		BeforeAll(func() {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxReleaseService{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())

			// Wait for the Deployment rather than Ready=True: UpdateComponentStatuses
			// gates Ready=True on ReadyReplicas == Replicas, which never happens in
			// envtest (no kubelet → pods never start). Deployment existence is
			// sufficient proof that the full manifest-apply codepath completed.
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      releaseControllerManagerDeploymentName,
					Namespace: releaseServiceNamespace,
				}, dep)).To(Succeed())
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
		})

		AfterAll(func() {
			testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxReleaseService{ObjectMeta: metav1.ObjectMeta{Name: CRName}})
		})

		It("should create the release-service Namespace", func() {
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: releaseServiceNamespace}, ns)).To(Succeed())
		})

		It("should create the internalrequests CRD", func() {
			crd := &unstructured.Unstructured{}
			crd.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "internalrequests.appstudio.redhat.com"}, crd)).To(Succeed())
		})

		It("should create the releases CRD", func() {
			crd := &unstructured.Unstructured{}
			crd.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "releases.appstudio.redhat.com"}, crd)).To(Succeed())
		})

		It("should create the release-service-manager-role ClusterRole", func() {
			cr := &rbacv1.ClusterRole{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "release-service-manager-role"}, cr)).To(Succeed())
		})

		It("should create the release-service-leader-election-role Role", func() {
			role := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "release-service-leader-election-role",
				Namespace: releaseServiceNamespace,
			}, role)).To(Succeed())
		})

		It("should create the release-service-manager-rolebinding ClusterRoleBinding", func() {
			crb := &rbacv1.ClusterRoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "release-service-manager-rolebinding"}, crb)).To(Succeed())
		})

		It("should create the release-service-leader-election-rolebinding RoleBinding", func() {
			rb := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "release-service-leader-election-rolebinding",
				Namespace: releaseServiceNamespace,
			}, rb)).To(Succeed())
		})

		It("should create the ConfigMaps", func() {
			for _, name := range []string{
				"release-service-manager-config",
				"release-service-manager-properties",
			} {
				cm := &corev1.ConfigMap{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: releaseServiceNamespace}, cm)).To(Succeed())
			}
		})

		It("should create the metrics Service", func() {
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "release-service-controller-manager-metrics-service",
				Namespace: releaseServiceNamespace,
			}, svc)).To(Succeed())
		})

		It("should create the webhook Service", func() {
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "release-service-webhook-service",
				Namespace: releaseServiceNamespace,
			}, svc)).To(Succeed())
		})

		It("should create the controller manager Deployment", func() {
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      releaseControllerManagerDeploymentName,
				Namespace: releaseServiceNamespace,
			}, dep)).To(Succeed())
		})

	})
})
